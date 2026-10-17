#!/usr/bin/env bash
# Regenerates the README screenshots in docs/images from the made-up demo
# library (TestWriteDocsScreens in internal/alexandria/web and internal/boox).
# No database or server is involved, so no real library can leak into them.
#
# Needs Go, google-chrome (or $CHROME) and network access for the Caveat font
# (SIL OFL 1.1, fetched at a pinned checksum, not committed).
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
chrome=${CHROME:-google-chrome}
out=$root/docs/images
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

font_url='https://github.com/google/fonts/raw/main/ofl/caveat/Caveat%5Bwght%5D.ttf'
font_sha=0bdb6b660482d31531b3945849fba5916b3ef8695da7024a9e6b9ee3c4157988
mkdir -p "$work/page/font" "$work/site"
curl -sSfL -o "$work/page/font/Caveat.ttf" "$font_url"
echo "$font_sha  $work/page/font/Caveat.ttf" | sha256sum -c --quiet

shot() { # shot <html> <png> <width> <height>
	"$chrome" --headless=new --disable-gpu --hide-scrollbars --window-size="$3,$4" \
		--virtual-time-budget=3000 --screenshot="$2" "file://$1" >/dev/null 2>&1
}

# The handwritten page the notebook view shows.
cp "$root/docs/screenshots/demo-page.html" "$work/page/"
shot "$work/page/demo-page.html" "$work/site/demo-page.png" 1404 1872

(cd "$root" && ALEXANDRIA_DOCS_SCREENS_DIR="$work/site" go test -count=1 -run '^TestWriteDocsScreens$' \
	./internal/alexandria/web/ ./internal/boox/ >/dev/null)

mkdir -p "$out"
while read -r name height; do
	shot "$work/site/$name.html" "$out/$name.png" 1280 "$height"
done <<'EOF'
overview 760
boox-notebook 1300
boox-notebooks 900
search 830
tasks 900
calendar 690
boox-minutes 900
settings-recognition 900
jobs 900
EOF
echo "wrote $(ls "$out"/*.png | wc -l) screenshots to $out"
