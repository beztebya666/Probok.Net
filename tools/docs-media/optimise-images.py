"""Converts the README screenshots to WebP.

Retina PNGs of a map are several megabytes each; at the sizes the README shows
them, WebP is visually identical and roughly twenty times smaller.

    python tools/docs-media/optimise-images.py docs/media
"""
from __future__ import annotations

import sys
from pathlib import Path

from PIL import Image

# Retina is wasted on a README: nobody views these above ~1200 CSS px.
MAX_WIDTH = 1800
QUALITY = 82


def main() -> None:
    out_dir = Path(sys.argv[1])
    for path in sorted(out_dir.glob("*.png")):
        if path.stem.endswith("-still"):
            path.unlink()
            continue
        image = Image.open(path).convert("RGB")
        if image.width > MAX_WIDTH:
            image = image.resize((MAX_WIDTH, round(image.height * MAX_WIDTH / image.width)), Image.LANCZOS)
        target = path.with_suffix(".webp")
        image.save(target, quality=QUALITY, method=6)
        path.unlink()
        print(f"{target.name}: {target.stat().st_size // 1024} KB")


if __name__ == "__main__":
    main()
