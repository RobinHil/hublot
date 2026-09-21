#!/bin/sh
# Renders the social card. The PNG is committed, so a build needs no renderer;
# run this after changing the card and commit the result.
set -eu
cd "$(dirname "$0")/.."
command -v rsvg-convert >/dev/null 2>&1 || {
  echo "rsvg-convert is not installed: it comes with librsvg" >&2
  exit 1
}
rsvg-convert -w 1200 -h 630 scripts/og-card.svg -o public/og.png
echo "wrote public/og.png"
