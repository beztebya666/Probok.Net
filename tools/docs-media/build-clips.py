"""Assembles the frames from record.mjs into the animated clips the README uses.

Animated WebP keeps the map readable at a fraction of a GIF's size, and GitHub
renders it inline. Run after record.mjs:

    python tools/docs-media/build-clips.py <frames-dir> <output-dir>
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

from PIL import Image

# Wide enough to read the panels, small enough to load on a phone.
TARGET_WIDTH = 900


def build(frames_dir: Path, out_dir: Path) -> None:
    meta = json.loads((frames_dir / "meta.json").read_text(encoding="utf-8"))
    paths = sorted(p for p in frames_dir.glob("*.png"))
    if not paths:
        raise SystemExit(f"no frames in {frames_dir}")

    frames = []
    for path in paths:
        image = Image.open(path).convert("RGB")
        scale = TARGET_WIDTH / image.width
        frames.append(image.resize((TARGET_WIDTH, round(image.height * scale)), Image.LANCZOS))

    out_dir.mkdir(parents=True, exist_ok=True)
    out = out_dir / f"{frames_dir.name}.webp"
    frames[0].save(
        out,
        save_all=True,
        append_images=frames[1:],
        duration=round(1000 / meta["fps"]),
        # The last frame is the answer, so hold it before looping back.
        loop=0,
        quality=72,
        method=6,
    )
    print(f"built {out.name}: {len(frames)} frames, {out.stat().st_size // 1024} KB")

    # A still for anywhere the animation does not play.
    frames[-1].save(out_dir / f"{frames_dir.name}-still.png", optimize=True)


def main() -> None:
    frames_root = Path(sys.argv[1])
    out_dir = Path(sys.argv[2])
    for child in sorted(frames_root.iterdir()):
        if child.is_dir() and (child / "meta.json").exists():
            build(child, out_dir)


if __name__ == "__main__":
    main()
