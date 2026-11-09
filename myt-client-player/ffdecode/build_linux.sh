#!/bin/bash
# Build libffdecode.so for Linux — FFmpeg (系统库或 Linux 静态库) + 纯软解
# 前置: g++、ffmpeg 头文件与库（apt: build-essential libavcodec-dev libavutil-dev
#       libswscale-dev libswresample-dev）
# 用法: 在 myt-client-player/ffdecode 下执行 bash build_linux.sh
#
# 注意：
#   - 源码 ffdecode.c 是 C++ 兼容代码（extern "C" 块），必须用 g++（gcc C 模式编译失败）
#   - third_party/ffmpeg-8.1/lib 下的 .a 是 MinGW COFF 格式，不能给 Linux 链接；
#     如需静态链接，须提供 Linux ELF 格式的 .a 放到 third_party/ffmpeg-8.1/linux_<arch>/lib

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SRC="$SCRIPT_DIR/ffdecode.c"
OUT="$SCRIPT_DIR/../libffdecode.so"

BUILT=false

# 优先：Linux 静态 FFmpeg（third_party/ffmpeg-8.1/linux_<arch>/lib，可选目录，
# 由使用者自行放置；include 沿用 third_party/ffmpeg-8.1/include）
ARCH="$(uname -m)"
STATIC_DIR="$SCRIPT_DIR/third_party/ffmpeg-8.1/linux_${ARCH}"
FF_INC="$SCRIPT_DIR/third_party/ffmpeg-8.1/include"
if [ -d "$STATIC_DIR/lib" ] && ls "$STATIC_DIR"/lib/libavcodec.a >/dev/null 2>&1; then
  echo "=== Building libffdecode.so (FFmpeg static: $STATIC_DIR) ==="
  g++ -std=c++17 -shared -fPIC -O2 -o "$OUT" "$SRC" \
    -I"$FF_INC" \
    -Wl,--whole-archive \
    "$STATIC_DIR/lib/libavcodec.a" "$STATIC_DIR/lib/libswscale.a" \
    "$STATIC_DIR/lib/libswresample.a" "$STATIC_DIR/lib/libavutil.a" \
    -Wl,--no-whole-archive \
    -lpthread -lm -lz 2>&1 && BUILT=true || echo "[warn] 静态库构建失败，回退系统 FFmpeg ..."
fi

# 回退：系统 FFmpeg（-lavcodec -lswscale -lswresample -lavutil）
if [ "$BUILT" = "false" ]; then
  if pkg-config --exists libavcodec libavutil libswscale libswresample 2>/dev/null; then
    FF_INC="$(pkg-config --variable=includedir libavcodec)"
    echo "=== Building libffdecode.so (system FFmpeg, pkg-config) ==="
    g++ -std=c++17 -shared -fPIC -O2 -o "$OUT" "$SRC" \
      -I"$FF_INC" \
      $(pkg-config --libs libavcodec libavutil libswscale libswresample) \
      -lpthread -lm 2>&1
    BUILT=true
  else
    echo "=== Building libffdecode.so (system FFmpeg, -l 链接) ==="
    # 无 pkg-config 时按常规路径链接（需系统已装 ffmpeg dev 包）
    g++ -std=c++17 -shared -fPIC -O2 -o "$OUT" "$SRC" \
      -lavcodec -lswscale -lswresample -lavutil \
      -lpthread -lm 2>&1
    BUILT=true
  fi
fi

if [ -f "$OUT" ]; then
  echo ""
  echo "SUCCESS: $OUT"
  file "$OUT" 2>/dev/null || true
else
  echo ""
  echo "FAILED"
  exit 1
fi
