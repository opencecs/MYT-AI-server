#!/usr/bin/env bash
# 一键编译 go_legacy (macOS) 的所有依赖 dylib + Go 主程序
# 用法: bash scripts/build_macos.sh
# 前置: Xcode Command Line Tools, brew install ffmpeg (或 third_party FFmpeg)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
FFDECODE_DIR="$PROJECT_ROOT/ffdecode"

echo "=========================================="
echo "  go_legacy (macOS) 一键编译"
echo "=========================================="
echo "项目根目录: $PROJECT_ROOT"
echo ""

# ---- 1. 编译 ffdecode.dylib ----
echo "[1/3] 编译 ffdecode.dylib ..."
bash "$FFDECODE_DIR/build_macos.sh"
echo ""

# ---- 2. 编译 metalrender.dylib ----
echo "[2/3] 编译 metalrender.dylib ..."
bash "$FFDECODE_DIR/build_metal.sh"
echo ""

# ---- 3. 编译 go_legacy (Go) ----
echo "[3/3] 编译 go_legacy ..."
cd "$PROJECT_ROOT"
CGO_ENABLED=1 go build -trimpath -ldflags="-w -s" -o go_legacy .
echo "  -> go_legacy OK ($(stat -f%z "$PROJECT_ROOT/go_legacy") bytes)"
echo ""

# ---- 4. 复制到 MYT ----
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
