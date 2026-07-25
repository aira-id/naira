#!/usr/bin/env python3
"""Loopback-only HTTP server wrapping openWakeWord for Naira's wake-word
detector (RFC.md §5 Concerns: wake-word engine decision — openWakeWord,
stock pretrained model, no CGo). Supervised as a subprocess by
internal/adapter/process.Supervisor, called by
internal/adapter/wakeword.HTTPDetector — mirrors the whisper-server/
llama-server pattern (RFC.md#architecture--tech-stack).

Protocol: POST /detect with a raw PCM16 mono 16kHz frame as the request body
(domain.AudioFrameBytes-sized, but any length works since openWakeWord
buffers internally). Response: {"detected": bool, "score": float}.

One persistent openwakeword.Model instance is fed frames in submission
order — its internal melspectrogram/embedding buffers carry state across
calls, so frame order matters and this server must not be scaled across
multiple processes for a single capture stream.

Model files (melspectrogram, embedding, and the wakeword model itself) are
fetched by openwakeword's own utils.download_models() into --cache-dir on
first run — NOT via `naira models download` (see models.yaml comment).
"""
import argparse
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import numpy as np


def build_handler(model, target_key, threshold):
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, fmt, *args):
            pass  # no per-request logging — never log audio-adjacent metadata

        def do_POST(self):
            if self.path != "/detect":
                self.send_response(404)
                self.end_headers()
                return

            length = int(self.headers.get("Content-Length", 0))
            raw = self.rfile.read(length) if length else b""
            frame = np.frombuffer(raw, dtype=np.int16)

            score = 0.0
            if frame.size > 0:
                predictions = model.predict(frame)
                score = float(predictions.get(target_key, 0.0))

            body = json.dumps({"detected": score >= threshold, "score": score}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    return Handler


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--model", default="hey_jarvis_v0.1", help="openwakeword pretrained model name")
    parser.add_argument("--cache-dir", default="./models/openwakeword", help="directory for openwakeword's model cache")
    parser.add_argument("--port", type=int, default=8082)
    parser.add_argument("--threshold", type=float, default=0.5)
    args = parser.parse_args()

    from openwakeword.model import Model
    from openwakeword.utils import download_models

    download_models(target_directory=args.cache_dir)
    model = Model(wakeword_models=[args.model], inference_framework="onnx")

    # Model keys are the loaded model's basename without extension.
    target_key = args.model

    server = ThreadingHTTPServer(("127.0.0.1", args.port), build_handler(model, target_key, args.threshold))
    print(f"openwakeword_server listening on 127.0.0.1:{args.port} (model={args.model})", file=sys.stderr)
    server.serve_forever()


if __name__ == "__main__":
    main()
