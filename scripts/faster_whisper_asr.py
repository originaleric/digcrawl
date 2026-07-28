#!/usr/bin/env python3
"""DigCrawl faster-whisper local ASR runner.

This script intentionally matches the argument shape used by ASR.LocalArgs:

  python3 scripts/faster_whisper_asr.py <audio_path> --model small --output_format json --output_dir /tmp/out
"""

import argparse
import json
import sys
from pathlib import Path


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("audio_path")
    parser.add_argument("--model", default="small")
    parser.add_argument("--language", default="")
    parser.add_argument("--output_format", default="json")
    parser.add_argument("--output_dir", default="")
    parser.add_argument("--device", default="cpu")
    parser.add_argument("--compute_type", default="int8")
    args = parser.parse_args()

    try:
        from faster_whisper import WhisperModel
    except Exception as exc:  # pragma: no cover - exercised by runtime readiness.
        print(f"faster_whisper import failed: {exc}", file=sys.stderr)
        return 2

    model = WhisperModel(args.model, device=args.device, compute_type=args.compute_type)
    kwargs = {"vad_filter": True, "beam_size": 5}
    if args.language:
        kwargs["language"] = args.language
    segments, info = model.transcribe(args.audio_path, **kwargs)

    items = []
    text_parts = []
    for seg in segments:
        text = (seg.text or "").strip()
        if not text:
            continue
        items.append({"start": seg.start, "end": seg.end, "text": text})
        text_parts.append(text)
    result = {
        "text": "".join(text_parts),
        "language": info.language,
        "duration": info.duration,
        "segments": items,
    }
    data = json.dumps(result, ensure_ascii=False)
    if args.output_dir:
        out_dir = Path(args.output_dir)
        out_dir.mkdir(parents=True, exist_ok=True)
        Path(out_dir, Path(args.audio_path).stem + ".json").write_text(data, encoding="utf-8")
    print(data)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
