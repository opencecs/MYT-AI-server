#!/bin/bash
# Build ffdecode.dylib for macOS — FFmpeg + VideoToolbox
# Prerequisites: Xcode Command Line Tools (clang), brew install ffmpeg
# Run from: myt-client-player/ffdecode

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SRC="$SCRIPT_DIR/ffdecode.c"
OUT="$SCRIPT_DIR/../ffdecode.dylib"

BUILT=false

# 优先使用 macos_arm64 静态库
MAC_LIB="$SCRIPT_DIR/third_party/ffmpeg-8.1/macos_arm64"
if [ -d "$MAC_LIB/include" ] && [ -d "$MAC_LIB/lib" ]; then
  echo "=== Building ffdecode.dylib (macOS arm64 FFmpeg static) ==="
  FF_INC="$MAC_LIB/include"
  FF_LINK="$MAC_LIB/lib/libavcodec.a $MAC_LIB/lib/libswscale.a $MAC_LIB/lib/libswresample.a $MAC_LIB/lib/libavutil.a"
  if clang -std=c11 -shared -O2 -o "$OUT" "$SRC" \
    -I"$FF_INC" \
    $FF_LINK \
    -framework VideoToolbox \
    -framework CoreFoundation \
    -framework CoreMedia \
    -framework CoreVideo \
    -framework Security \
    -framework Foundation \
    -framework AudioToolbox \
    -lbz2 -lz -liconv \
    -L/opt/homebrew/lib -ldav1d -lopus -lvpx -lx264 -lx265 -lmp3lame -lSvtAv1Enc 2>&1; then
    BUILT=true
  else
    echo "[warn] macos_arm64 构建失败，尝试 brew FFmpeg ..."
  fi
fi

# Fallback: brew 动态库
if [ "$BUILT" = "false" ]; then
  BREW_PREFIX="$(brew --prefix ffmpeg 2>/dev/null || true)"
  if [ -z "$BREW_PREFIX" ]; then
    echo "ERROR: FFmpeg not found. Install with: brew install ffmpeg"
    exit 1
  fi
  echo "=== Building ffdecode.dylib (brew FFmpeg dynamic) ==="
  FF_INC="$BREW_PREFIX/include"
  FF_LINK="-L$BREW_PREFIX/lib -lavcodec -lswscale -lswresample -lavutil"
  clang -std=c11 -shared -O2 -o "$OUT" "$SRC" \
    -I"$FF_INC" \
    $FF_LINK \
    -framework VideoToolbox \
    -framework CoreFoundation \
    -framework CoreMedia \
    -framework CoreVideo \
    -framework Security \
    -framework Foundation \
    -lbz2 -lz -liconv
  BUILT=true
fi

if [ -f "$OUT" ]; then
  echo ""
  echo "SUCCESS: $OUT"
  file "$OUT"
else
  echo ""
  echo "FAILED"
  exit 1
fi
