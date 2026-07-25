#!/usr/bin/env bash
# Builds/installs the standalone inference-server binaries Naira supervises
# as subprocesses (RFC.md#architecture--tech-stack decision note):
#   - whisper-server (from whisper.cpp)  — STT
#   - llama-server   (from llama.cpp)    — LLM
#   - piper          (prebuilt release)  — TTS
#
# whisper.cpp/llama.cpp are built from source with AVX2/FMA/F16C explicitly
# OFF (RFC.md Performance Requirement: target hardware is a Sandy Bridge
# i5-2510M with no AVX2/FMA/F16C) — this holds regardless of what the build
# machine itself supports, since we're producing a binary for the target
# device, not necessarily running it here.
#
# Usage:
#   scripts/install_deps.sh [options]
#
# Options:
#   --prefix DIR       install binaries here (default: /usr/local/bin)
#   --workdir DIR       clone/build sources here (default: ./build/deps)
#   --jobs N            parallel build jobs (default: nproc)
#   --piper-version V   piper release tag to fetch (default: 2023.11.14-2)
#   --skip-whisper       don't build whisper.cpp
#   --skip-llama         don't build llama.cpp
#   --skip-piper         don't fetch piper
#   --force              rebuild/re-fetch even if already installed
#   -h, --help           show this help
set -euo pipefail

PREFIX="/usr/local/bin"
WORKDIR="./build/deps"
JOBS="$(nproc 2>/dev/null || echo 4)"
PIPER_VERSION="2023.11.14-2"
SKIP_WHISPER=0
SKIP_LLAMA=0
SKIP_PIPER=0
FORCE=0

log() { printf '==> %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

usage() { sed -n '2,26p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
  case "$1" in
    --prefix) PREFIX="$2"; shift 2 ;;
    --workdir) WORKDIR="$2"; shift 2 ;;
    --jobs) JOBS="$2"; shift 2 ;;
    --piper-version) PIPER_VERSION="$2"; shift 2 ;;
    --skip-whisper) SKIP_WHISPER=1; shift ;;
    --skip-llama) SKIP_LLAMA=1; shift ;;
    --skip-piper) SKIP_PIPER=1; shift ;;
    --force) FORCE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown argument: $1 (see --help)" ;;
  esac
done

[ "$(uname -s)" = "Linux" ] || die "this script only supports Linux (RFC.md Assumptions: minimal Linux distro target)"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ;;
  *) die "unsupported architecture: $ARCH (target hardware is x86_64, RFC.md Assumptions)" ;;
esac

for bin in git cmake make curl tar; do
  command -v "$bin" >/dev/null 2>&1 || die "required tool not found: $bin"
done

mkdir -p "$WORKDIR" "$PREFIX"
WORKDIR="$(cd "$WORKDIR" && pwd)"

install_bin() {
  # install_bin SRC_PATH DEST_NAME
  local src="$1" dest="$PREFIX/$2"
  if [ -w "$PREFIX" ]; then
    cp "$src" "$dest"
  else
    log "using sudo to write to $PREFIX"
    sudo cp "$src" "$dest"
  fi
  chmod +x "$dest"
  log "installed $dest"
}

already_installed() {
  [ "$FORCE" -eq 0 ] && command -v "$1" >/dev/null 2>&1
}

build_whisper_cpp() {
  if already_installed whisper-server; then
    log "whisper-server already installed, skipping (use --force to rebuild)"
    return
  fi

  log "building whisper.cpp (whisper-server, AVX-only)"
  local src="$WORKDIR/whisper.cpp"
  if [ -d "$src/.git" ]; then
    git -C "$src" pull --ff-only
  else
    git clone --depth 1 https://github.com/ggml-org/whisper.cpp "$src"
  fi

  cmake -B "$src/build" -S "$src" \
    -DCMAKE_BUILD_TYPE=Release \
    -DGGML_AVX2=OFF -DGGML_FMA=OFF -DGGML_F16C=OFF \
    -DWHISPER_SDL2=OFF
  cmake --build "$src/build" --config Release -j "$JOBS" --target whisper-server

  local built
  built="$(find "$src/build" -type f -name whisper-server -perm -u+x | head -n1)"
  [ -n "$built" ] || die "whisper-server binary not found after build — whisper.cpp's server target may have been renamed upstream"
  install_bin "$built" whisper-server
}

build_llama_cpp() {
  if already_installed llama-server; then
    log "llama-server already installed, skipping (use --force to rebuild)"
    return
  fi

  log "building llama.cpp (llama-server, AVX-only)"
  local src="$WORKDIR/llama.cpp"
  if [ -d "$src/.git" ]; then
    git -C "$src" pull --ff-only
  else
    git clone --depth 1 https://github.com/ggml-org/llama.cpp "$src"
  fi

  cmake -B "$src/build" -S "$src" \
    -DCMAKE_BUILD_TYPE=Release \
    -DGGML_AVX=ON -DGGML_AVX2=OFF -DGGML_FMA=OFF -DGGML_F16C=OFF \
    -DLLAMA_CURL=OFF
  cmake --build "$src/build" --config Release -j "$JOBS" --target llama-server

  local built
  built="$(find "$src/build" -type f -name llama-server -perm -u+x | head -n1)"
  [ -n "$built" ] || die "llama-server binary not found after build — llama.cpp's server target may have been renamed upstream"
  install_bin "$built" llama-server
}

fetch_piper() {
  if already_installed piper; then
    log "piper already installed, skipping (use --force to re-fetch)"
    return
  fi

  log "fetching piper $PIPER_VERSION prebuilt release (no official server mode to build from source — RFC.md#architecture--tech-stack decision note)"
  local archive="piper_linux_x86_64.tar.gz"
  local url="https://github.com/rhasspy/piper/releases/download/${PIPER_VERSION}/${archive}"
  local dest="$WORKDIR/piper"
  mkdir -p "$dest"
  curl -fL "$url" -o "$WORKDIR/$archive"
  tar -xzf "$WORKDIR/$archive" -C "$dest" --strip-components=1

  [ -x "$dest/piper" ] || die "piper binary not found in extracted release archive"
  install_bin "$dest/piper" piper

  # piper's binary depends on shared libs (libonnxruntime, libpiper_phonemize,
  # espeak-ng-data) shipped alongside it in the release archive — these must
  # stay next to wherever `piper` actually runs from. Copying just the
  # binary into $PREFIX breaks at runtime, so drop the whole release next to
  # it and point at that via models.yaml's tts.server_bin if needed.
  local libdir="$PREFIX/piper-support"
  mkdir -p "$libdir" 2>/dev/null || sudo mkdir -p "$libdir"
  if [ -w "$PREFIX" ]; then
    cp -r "$dest"/* "$libdir/"
  else
    sudo cp -r "$dest"/* "$libdir/"
  fi
  log "piper's shared libs/espeak-ng-data copied to $libdir — if piper fails to start with a missing .so error, run it as $libdir/piper instead of the copy in $PREFIX"
}

[ "$SKIP_WHISPER" -eq 1 ] || build_whisper_cpp
[ "$SKIP_LLAMA" -eq 1 ] || build_llama_cpp
[ "$SKIP_PIPER" -eq 1 ] || fetch_piper

log "done. Point models.yaml's server_bin fields at: $(command -v whisper-server 2>/dev/null || echo "$PREFIX/whisper-server"), $(command -v llama-server 2>/dev/null || echo "$PREFIX/llama-server"), $(command -v piper 2>/dev/null || echo "$PREFIX/piper")"
