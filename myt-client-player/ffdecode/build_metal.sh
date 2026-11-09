#!/bin/bash
# Build metalrender.dylib for macOS — Metal GPU renderer
# Prerequisites: Xcode Command Line Tools (clang)
# Run from: myt-client-player/ffdecode

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SRC="$SCRIPT_DIR/metalrender.m"
OUT="$SCRIPT_DIR/../metalrender.dylib"

echo "=== Building metalrender.dylib (Metal GPU renderer) ==="

clang -std=c11 -ObjC -shared -O2 -o "$OUT" "$SRC" \
  -framework Metal \
  -framework MetalKit \
  -framework AppKit \
  -framework CoreVideo \
  -framework QuartzCore \
  -framework Foundation

if [ -f "$OUT" ]; then
  echo ""
  echo "SUCCESS: $OUT"
  file "$OUT"
else
  echo ""
  echo "FAILED"
  exit 1
fi
