# **Product Requirement Document (PRD)**

## **Project Name: Interactive Desktop AI Companion Robot ("Naira")**

## **1\. Executive Summary & Objective**

**Project Name:** Interactive AI Desktop Companion Robot

**Target Audience:** Children (Educational & Entertainment Focus)

**Primary Goals:** Transform older/recycled laptop hardware (Intel i5 2nd Gen, upgraded to 12 GB RAM) into a low-latency, highly engaging, interactive desktop robot companion. The robot features a dynamic animated facial interface, voice interaction, local offline AI processing, and real-time execution capabilities (e.g., generating and rendering HTML5 games and educational canvases on the fly).

**Connectivity Model:** Core companion (conversation, model knowledge, expressions, offline skills) works fully offline — no internet dependency. Skills that require internet (e.g., agent-based app/game generation, live content fetch) perform a connectivity check first; if unavailable, the robot vocally informs the child and falls back gracefully (e.g., offline skill alternative or "no internet" response) instead of hanging or crashing.

## **2\. Target Hardware & Resource Allocation Constraints**

### **2.1 Hardware Specification**

* **CPU:** Intel Core i5-2510M (Sandy Bridge, 2 Core / 4 Thread, 2.5 GHz base / 3.1 GHz Turbo) — *Supports AVX, No AVX2 / FMA / F16C*.  
* **Memory Target:** 12 GB RAM total.  
* **Operating System Target:** Minimal Linux Distribution (e.g., Ubuntu/Debian Minimal with a lightweight window manager like `i3wm` or `openbox`).

### **2.2 Memory Allocation Strategy (Total 12 GB)**

| Subsystem | Components & Frameworks | Target Allocation | Notes & Constraints |
| ----- | ----- | ----- | ----- |
| **Robot Core Runtime** | STT (`whisper.cpp`), Local LLM (`llama.cpp`), TTS (`Piper ONNX`), Go Orchestrator, GUI Overlay | **\~8.0 GB Dedicated Pool** | Real-time voice processing, expression updates, and core state machines. |
| **External Agent Engine** | Claude CLI / OpenCode Agent CLI | **2.0 GB Permanent Reserve \+ 2.0 GB Burst (allocated only while `EXECUTE_AGENT` active)** | Idle: 2 GB held for fast dispatch. Active: bursts to 4 GB for generation, released back to 2 GB on completion. Total ceiling 12 GB only during active generation; idle steady-state leaves \~2 GB headroom for OS/GUI. |

## **3\. High-Level Architecture & Technical Stack**

The architecture is built around a lightweight **Golang Core Orchestrator** leveraging native CGo bindings and IPC for ultra-low memory overhead and high concurrency.

┌────────────────────────────────────────────────────────────────────────┐  
│                       Golang Core Orchestrator                         │  
│                                                                        │  
│  \[Microphone\] ──\> whisper.cpp (CGo) ──\> Intent Parser                  │  
│                                              │                         │  
│  \[Speakers\]   \<── Piper TTS (ONNX CGo) \<── Local LLM (llama.cpp)       │  
│                                              │                         │  
│  \[UI Layer\]   \<── WebSocket / Native IPC ────┴──\> Display (Pygame/Web) │  
└────────────────────────────────────────────────────────────────────────┘  
                                               │  
                                               ▼  
                              \[Background Execution Engine\]  
                                   Claude CLI / OpenCode

### **Technical Stack Components:**

1. **Orchestration Layer:** Golang (`go build` with specific AVX flags).  
2. **Speech-to-Text (STT):** `whisper.cpp` (Go CGo bindings, model `base` or `small` with `int8` quantization).  
3. **Brain / Inference Engine (LLM):** `llama.cpp` (Go bindings or embedded CGo, compiled strictly with `-DGGML_AVX=ON -DGGML_AVX2=OFF`). Model: `Qwen2.5-1.5B-Instruct` or `Llama-3.2-1B-Instruct` (GGUF `Q4_K_M`).  
4. **Text-to-Speech (TTS):** `Piper TTS` via CGo `ONNX Runtime` (`id_ID-news_tts-medium.onnx` or equivalent `low`/`medium` models).  
5. **UI & Window Manager:** HTML5 Canvas / Neutralinojs / Go `webview` supporting frameless, transparent, always-on-top floating windows.

## **4\. Key Features & Functional Requirements**

### **4.1 Dynamic Expression Display & Window Overlay**

* **Visual States:**  
  * `IDLE` (Default waiting state)  
  * `LISTENING` (Active speech detection — mic stays open, but STT/LLM pipeline only activates after wake word/wake phrase detection; passive audio is not transcribed or sent to the LLM)  
  * `THINKING` (Inference processing)  
  * `SPEAKING` (Synchronized mouth movement matching audio amplitude)  
  * `HAPPY`, `SURPRISED`, `SYMPATHETIC`, `WORKING`, `SLEEPY`  
* **Adaptive Window Modes:**  
  * **Fullscreen Mode:** Default state when conversing directly with the user.  
  * **Floating Mini-Overlay Mode:** Automatically resizes (e.g., 250x250px), becomes frameless/transparent, and pins to `AlwaysOnTop` when opening web pages, educational tools, or generated HTML5 games in the background.

### **4.2 System Prompt Tag Routing (Micro-Agent Pattern)**

The local LLM uses structured prompt outputs to emit emotional metadata and system actions without multi-turn agent latency overhead:

* **System Prompt Guardrails:** Child-friendly language, concise responses, and obligatory tag prefixes.  
* **Output Format:** `[EXPRESSION_TAG] [ACTION_TAG] Spoken response string`  
* **Action Types:**  
  * `NONE`: Standard dialogue.  
  * `EXECUTE_AGENT`: Trigger Claude CLI / OpenCode in the background to build an application or game. Only fires for app/game-generation requests. Before dispatch, orchestrator checks (a) internet connectivity and (b) Claude CLI / OpenCode key or authorization present. If either check fails, skip execution and respond vocally with the specific reason (no internet / not authorized) instead of silently failing. **Sandbox guardrail:** generated code may only fetch dependencies (npm packages, network assets/CDN resources declared by the agent) — no other outbound network calls, no arbitrary shell/system commands, no filesystem access outside its own `/games/<name>/` directory.  
  * `OPEN_BROWSER`: Open a rendered canvas or URL. Requires connectivity check for remote URLs; local generated canvases work offline.

### **4.3 Child-Oriented Companion Skills**

#### **🎮 Skill 1: Instant HTML5 Game Generation**

* **Workflow:**  
  1. Child requests a game via voice (e.g., *"Make me a snake game\!"*).  
  2. Local LLM acknowledges vocally (*"On it\! Building a snake game now..."*) and sets expression to `[WORKING]`.  
  3. UI transitions to **Floating Mini-Overlay Mode**.  
  4. Go Orchestrator triggers `Claude CLI` / `OpenCode` in the background to write HTML5/Canvas JavaScript to disk (`/games/snake/index.html`).  
  5. Go launches the browser underneath the overlay once completed.

#### **📖 Skill 2: Interactive Storyteller (Choose-Your-Own-Adventure)**

* Narrates stories using Piper TTS while displaying SVG/canvas visual cards in the background window.  
* Pauses at story forks to prompt the child for vocal decisions.

#### **🎛️ Skill 3: Interactive Game Master & Quizzer**

* Plays sound effects for guess-the-sound games.  
* Hosts "20 Questions" or "Simon Says" (matching face expressions).

#### **🎨 Skill 4: AI Canvas & Flashcard Visualizer**

* Generates live SVG graphics or flashcards for educational queries (e.g., planetary orbits, alphabet learning).

#### **⏰ Skill 5: Routine & Emotional Companion**

* Friendly daily reminders (study time, hygiene) with cheerful animations.  
* **Screen Time Limit:** Automatically transitions to `[SLEEPY]` state after prolonged continuous usage to encourage breaks.

## **5\. Non-Functional Requirements & Performance Targets**

| Metric / Constraint | Requirement Standard | Mitigation Strategy |
| ----- | ----- | ----- |
| **CPU Architecture Safety** | Zero crashes due to `Illegal Instruction` | Strict CMake flags: `GGML_AVX=ON`, `GGML_AVX2=OFF`, `GGML_FMA=OFF`, `GGML_F16C=OFF`. |
| **Thread Usage** | Max 2 CPU Threads for LLM Inference | i5-2510M is 2 Core / 4 Thread (Hyper-Threading, not 4 physical cores). Pin inference to `-t 2` (one thread per physical core) to avoid HT contention; OS, GUI, STT, and TTS share remaining HT capacity opportunistically. Empirically benchmark in Phase 1 — HT contention behavior under mixed load is hard to predict on paper. |
| **Voice Latency (STT \+ LLM First Token)** | \<2.0 seconds | Sentence-level streaming from LLM directly to Piper TTS. **Must re-verify under `-t 2`** (reduced from `-t 3` per corrected core count) — benchmark explicitly in Phase 1. If target missed: try smaller/faster STT model (`whisper.cpp tiny`), reduce context below 1024 tokens, or accept relaxed target (e.g. \<3.0s) rather than starve OS/GUI threads. |
| **RAM Footprint Safety** | Core system usage \<7.0 GB | Model context capped at 1024 tokens; `--mlock` flag enabled to prevent disk swapping. |
| **Audio Privacy** | No raw audio persisted to disk or sent off-device | Mic buffer processed in-memory by `whisper.cpp` for transcription only, then discarded immediately after text extraction. Fully offline (no cloud STT) means no network exposure of voice data. Applies to core pipeline; `EXECUTE_AGENT` calls send only text (agent prompt), never raw audio. |

## **6\. Implementation & Build Guide**

### **6.1 `llama.cpp` Compilation Flag Setup**

Bash  
cmake \-B build \\  
    \-DGGML\_AVX=ON \\  
    \-DGGML\_AVX2=OFF \\  
    \-DGGML\_FMA=OFF \\  
    \-DGGML\_F16C=OFF

cmake \--build build \--config Release \-j$(nproc)

### **6.2 Golang CGo Compilation Command**

Bash  
CGO\_CFLAGS="-O3 \-mavx \-mno-avx2" go build \-o robot-ai main.go

### **6.3 Runtime Execution Command**

Bash  
./build/bin/llama-cli \\  
    \-m ./models/Qwen2.5-1.5B-Instruct-Q4\_K\_M.gguf \\  
    \-t 2 \\  
    \-c 1024 \\  
    \--mlock \\  
    \-p "You are Oto, a friendly AI robot companion for kids."

## **7\. Roadmap & Phase Execution**

* **Phase 1 (Core Pipeline):** Set up Go orchestrator, compile `whisper.cpp` & `llama.cpp` without AVX2, integrate Piper TTS via ONNX, and benchmark STT+LLM first-token latency under `-t 2` on actual i5-2510M hardware against the \<2.0s target (adjust model/context/thread strategy if missed).  
* **Phase 2 (UI & Overlay):** Build the HTML5/Go `webview` interface, transparent floating window logic, and mouth-sync animation.  
* **Phase 3 (Agent Execution):** Wire background Claude CLI / OpenCode process execution for dynamic HTML5 game generation.  
* **Phase 4 (Child-Friendly Polish):** Implement safety guardrails, screen-time limiters, and interactive storytelling skills. Include first-run parent acknowledgment flow (privacy/audio-handling disclosure, must accept before setup completes) and document it in `README.md` at implementation start.

