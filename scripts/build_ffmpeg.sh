#!/bin/bash
set -euo pipefail

# 构建精简 ffmpeg.exe（仅 dshow + libx264 + aac + flv + rtmp）
# 用于桌面客户端摄像头+麦克风推流到云手机容器。
# 2026-08-28: 增加 geq/lut 滤镜（A16 设备混合流转换：Y full + UV limited）
#             与 rawvideo muxer/demuxer（双进程 Go 泵管线的进程间裸流）。
#
# 前置：MSYS2 + MinGW64，需安装：
#   pacman -S --noconfirm make pkgconf nasm yasm diffutils mingw-w64-x86_64-x264
#
# 用法: bash scripts/build_ffmpeg.sh

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
BUILD_DIR="$PROJECT_ROOT/MYT/camdemo/ffmpeg_build"
OUTPUT_DIR="$PROJECT_ROOT/MYT/camdemo/ffmpeg_minimal"

FFMPEG_VERSION="n7.1"
FFMPEG_TARBALL="ffmpeg-7.1.tar.xz"
FFMPEG_URL="https://ffmpeg.org/releases/$FFMPEG_TARBALL"

echo "[ffmpeg] Building minimal ffmpeg.exe (dshow + libx264 + aac + flv + rtmp)..."
echo "[ffmpeg] Build dir: $BUILD_DIR"
echo "[ffmpeg] Output dir: $OUTPUT_DIR"

mkdir -p "$BUILD_DIR" "$OUTPUT_DIR"

# ─── 下载 FFmpeg 源码 ──────────────────────────────────────
cd "$BUILD_DIR"
if [ ! -f "$FFMPEG_TARBALL" ]; then
  echo "[ffmpeg] Downloading $FFMPEG_VERSION..."
  curl -L "$FFMPEG_URL" -o "$FFMPEG_TARBALL"
fi

# ─── 解压 ─────────────────────────────────────────────────
SRC_DIR="ffmpeg-7.1"
if [ ! -d "$SRC_DIR" ]; then
  echo "[ffmpeg] Extracting..."
  tar xf "$FFMPEG_TARBALL"
fi
cd "$SRC_DIR"

# ─── Configure: 精简构建 ───────────────────────────────────
# --disable-everything 关闭所有组件，再逐个启用所需的
# 只保留：dshow 输入 + libx264 视频编码 + aac 音频编码 + flv 封装 + rtmp 协议
echo "[ffmpeg] Configuring..."

PKG_CONFIG_PATH="/mingw64/lib/pkgconfig" \
CFLAGS="-I/mingw64/include" \
LDFLAGS="-L/mingw64/lib" \
./configure \
  --prefix="$OUTPUT_DIR" \
  --target-os=mingw64 \
  --arch=x86_64 \
  --enable-gpl \
  --enable-libx264 \
  --enable-w32threads \
  --enable-small \
  --disable-debug \
  --disable-doc \
  --disable-autodetect \
  --disable-programs \
  --disable-ffplay \
  --disable-ffprobe \
  --disable-everything \
  --enable-avdevice \
  --enable-swresample \
  --enable-swscale \
  --enable-indev=dshow \
  --enable-decoder=mjpeg \
  --enable-decoder=pcm_s16le \
  --enable-decoder=pcm_s8 \
  --enable-decoder=rawvideo \
  --enable-encoder=rawvideo \
  --enable-filter=format \
  --enable-filter=scale \
  --enable-filter=geq \
  --enable-filter=lut \
  --enable-filter=aresample \
  --enable-filter=anull \
  --enable-filter=null \
  --enable-encoder=libx264 \
  --enable-encoder=aac \
  --enable-encoder=pcm_s16le \
  --enable-muxer=flv \
  --enable-muxer=h264 \
  --enable-muxer=rawvideo \
  --enable-muxer=pcm_s16le \
  --enable-protocol=rtmp \
  --enable-protocol=file \
  --enable-protocol=pipe \
  --enable-demuxer=flv \
  --enable-demuxer=rawvideo \
  --enable-parser=aac \
  --enable-parser=h264 \
  --enable-ffmpeg \
  --extra-cflags="-I/mingw64/include" \
  --extra-ldflags="-L/mingw64/lib -lwinpthread"

# ─── 编译 ─────────────────────────────────────────────────
echo "[ffmpeg] Compiling (this takes ~5-10 min)..."
make -j$(nproc)

# ─── 安装 + strip ────────────────────────────────────────
echo "[ffmpeg] Installing..."
make install

# strip 去掉调试符号减小体积
FFMPEG_EXE="$OUTPUT_DIR/bin/ffmpeg.exe"
if [ -f "$FFMPEG_EXE" ]; then
  strip "$FFMPEG_EXE"
  SIZE=$(stat -c%s "$FFMPEG_EXE" 2>/dev/null || stat -f%z "$FFMPEG_EXE")
  SIZE_MB=$((SIZE / 1024 / 1024))
  echo "[ffmpeg] Done: $FFMPEG_EXE (${SIZE_MB} MB, $((SIZE / 1024)) KB)"
else
  echo "[ffmpeg] ERROR: ffmpeg.exe not found at $FFMPEG_EXE"
  exit 1
fi