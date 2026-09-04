#!/usr/bin/env python3
"""DigCrawl faster-whisper local ASR runner.

This script intentionally matches the argument shape used by ASR.LocalArgs:

  python3 scripts/faster_whisper_asr.py <audio_path> --model small --output_format json --output_dir /tmp/out
"""

import argparse
import json
import os
import sys
from pathlib import Path


def ensure_model_cache_env() -> None:
    configured_cache = os.environ.get("DIGCRAWL_ASR_MODEL_CACHE_DIR", "").strip()
    legacy_hub_cache = os.environ.get("DIGCRAWL_ASR_LEGACY_HUB_CACHE_DIR", "").strip()
    if configured_cache:
        # Go passes ModelCacheDir through this variable. It must take precedence
        # over inherited Hugging Face settings so doctor and transcription use
        # exactly the same offline model files.
        cache_dir = Path(configured_cache).expanduser()
        cache_dir.mkdir(parents=True, exist_ok=True)
        os.environ["HF_HOME"] = str(cache_dir)
        if legacy_hub_cache:
            os.environ["HUGGINGFACE_HUB_CACHE"] = legacy_hub_cache
        else:
            os.environ.pop("HUGGINGFACE_HUB_CACHE", None)
    elif not os.environ.get("HF_HOME") and not os.environ.get("HUGGINGFACE_HUB_CACHE"):
        cache_root = Path(os.environ.get("XDG_CACHE_HOME", Path.home() / ".cache"))
        cache_dir = cache_root / "digcrawl" / "asr_models"
        cache_dir.mkdir(parents=True, exist_ok=True)
        os.environ["HF_HOME"] = str(cache_dir)
    os.environ.setdefault("HF_HUB_DISABLE_PROGRESS_BARS", "1")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("audio_path", nargs="?")
    parser.add_argument(
        "--check",
        action="store_true",
        help="verify that faster-whisper is importable",
    )
    parser.add_argument(
        "--check-model",
        action="store_true",
        help="with --check, verify the selected model is already available offline",
    )
    parser.add_argument(
        "--download-model",
        action="store_true",
        help="download the selected model into the configured cache without transcribing audio",
    )
    parser.add_argument("--model", default="small")
    parser.add_argument("--language", default="")
    parser.add_argument("--output_format", default="json")
    parser.add_argument("--output_dir", default="")
    parser.add_argument("--device", default="cpu")
    parser.add_argument("--compute_type", default="int8")
    args = parser.parse_args()

    ensure_model_cache_env()

    try:
        from faster_whisper import WhisperModel
    except Exception as exc:  # pragma: no cover - exercised by runtime readiness.
        print(f"faster_whisper import failed: {exc}", file=sys.stderr)
        return 2

    if args.check_model and not args.check:
        parser.error("--check-model requires --check")
    if args.download_model and (args.check or args.check_model or args.audio_path):
        parser.error("--download-model cannot be combined with --check or audio_path")
    if args.check:
        if args.check_model:
            try:
                WhisperModel(
                    args.model,
                    device=args.device,
                    compute_type=args.compute_type,
                    local_files_only=True,
                )
            except Exception as exc:  # pragma: no cover - exercised by doctor.
                print(f"faster_whisper model check failed: {exc}", file=sys.stderr)
                return 3
        print(json.dumps({"status": "ok", "provider": "faster-whisper", "model": args.model, "model_verified": args.check_model}))
        return 0
    if args.download_model:
        try:
            WhisperModel(args.model, device=args.device, compute_type=args.compute_type)
        except Exception as exc:  # pragma: no cover - depends on model hosting.
            print(f"faster_whisper model download failed: {exc}", file=sys.stderr)
            return 3
        print(json.dumps({"status": "ok", "provider": "faster-whisper", "model": args.model, "model_downloaded": True}))
        return 0
    if not args.audio_path:
        parser.error("audio_path is required unless --check or --download-model is used")

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
