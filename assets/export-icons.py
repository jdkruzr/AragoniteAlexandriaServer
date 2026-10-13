#!/usr/bin/env python3
"""Export the Server icon into the embedded web assets (requires Pillow)."""
from pathlib import Path
from PIL import Image

root = Path(__file__).resolve().parents[1]
source = root / "assets/aragonite-alexandria-server-crystal-temple-v1.png"
static = root / "internal/alexandria/web/static"
with Image.open(source) as icon:
    for name, size in (("alexandria-mark-v1.png", 144), ("alexandria-icon-v1.png", 48)):
        icon.resize((size, size), Image.Resampling.LANCZOS).save(static / name)
