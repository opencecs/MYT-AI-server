#!/usr/bin/env bash
# 一键编译 go_legacy (Linux) — libffdecode.so + Go 主程序（CGO）
# 用法: bash scripts/build_linux.sh
# 前置: g++, pkg-config, ffmpeg 开发包, X11 开发包, ALSA 开发包
#   Ubuntu/Debian:
#     sudo apt install -y build-essential pkg-config \
#       libavcodec-dev libavutil-dev libswscale-dev libswresample-dev \
#       libx11-dev libasound2-dev
# 产物: 项目根 go_legacy (Linux ELF) → 复制到 MYT/bin/go_legacy 与 MYT/embedded/go_legacy

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
FFDECODE_DIR="$PROJECT_ROOT/ffdecode"

echo "=========================================="
echo "  go_legacy (Linux) 一键编译"
echo "=========================================="
echo "项目根目录: $PROJECT_ROOT"
echo ""

# ---- 1. 编译 libffdecode.so ----
echo "[1/2] 编译 libffdecode.so ..."
bash "$FFDECODE_DIR/build_linux.sh"
echo ""

# ---- 2. 编译 go_legacy (Go, CGO for X11/ALSA) ----
echo "[2/2] 编译 go_legacy ..."
cd "$PROJECT_ROOT"
CGO_ENABLED=1 go build -trimpath -ldflags="-w -s" -o go_legacy .
echo "  -> go_legacy OK ($(stat -c%s "$PROJECT_ROOT/go_legacy") bytes)"
echo ""

# ---- 3. 复制到 MYT ----
MYT_ROOT="$(cd "$PROJECT_ROOT/../MYT" 2>/dev/null && pwd)" || MYT_ROOT=""
if [ -d "$MYT_ROOT" ]; then
    echo "[额外] 复制到 MYT ..."
    mkdir -p "$MYT_ROOT/bin" "$MYT_ROOT/embedded"
    cp "$PROJECT_ROOT/go_legacy" "$MYT_ROOT/bin/go_legacy"
    cp "$PROJECT_ROOT/go_legacy" "$MYT_ROOT/embedded/go_legacy"
    echo "  -> MYT/bin/go_legacy OK"
    echo "  -> MYT/embedded/go_legacy OK"
fi

echo ""
echo "=========================================="
echo "  全部编译完成!"
echo "=========================================="
