#!/usr/bin/env python3
"""Export the Server icon into the embedded web assets (requires Pillow)."""
from pathlib import Path
from PIL import Image

root = Path(__file__).resolve().parents[1]
source = root / "assets/aragonite-alexandria-server-crystal-temple-v1.png"
static = root / "internal/alexandria/web/static"
with Image.open(source) as icon:
    icon = icon.convert("RGBA")
    # Header mark is displayed at 32 CSS pixels; 64 keeps it sharp on 2x screens.
    sizes = {
        "alexandria-mark-v1.png": 64,
        "favicon-16.png": 16,
        "favicon-32.png": 32,
        "apple-touch-icon.png": 180,
    }
    for name, size in sizes.items():
        icon.resize((size, size), Image.Resampling.LANCZOS).save(static / name, optimize=True)
    icon.save(static / "favicon.ico", sizes=[(16, 16), (32, 32), (48, 48)])
