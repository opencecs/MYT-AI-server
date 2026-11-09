#!/usr/bin/env bash
# 一键编译 go_legacy.exe 的所有依赖 DLL + Go 主程序
# 用法: bash scripts/build_dlls.sh
# 前置: MSYS2/MinGW64 在 PATH, MSVC Build Tools 2022 已安装

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
FFDECODE_DIR="$PROJECT_ROOT/ffdecode"

echo "=========================================="
echo "  go_legacy.exe 一键编译"
echo "=========================================="
echo "项目根目录: $PROJECT_ROOT"
echo ""

# ---- 1. 编译 ffdecode.dll (GCC) ----
echo "[1/3] 编译 ffdecode.dll ..."
export PATH="/c/msys64/mingw64/bin:$PATH"
cd "$FFDECODE_DIR"
FF_INC=third_party/ffmpeg-8.1/include
FF_LIB=third_party/ffmpeg-8.1/lib
g++ -shared -O2 -static -o "$PROJECT_ROOT/ffdecode.dll" ffdecode.c \
  -I"$FF_INC" \
  "$FF_LIB/libavcodec.a" \
  "$FF_LIB/libswscale.a" \
  "$FF_LIB/libswresample.a" \
  "$FF_LIB/libavutil.a" \
  -ld3d11 -ldxgi -ldxva2 -lws2_32 -lbcrypt -lSecur32 -lOle32 -lUser32 -lShlwapi -lAdvapi32 -lShell32 -lm \
  -Wl,--whole-archive -lwinpthread -Wl,--no-whole-archive
echo "  -> ffdecode.dll OK ($(stat -c%s "$PROJECT_ROOT/ffdecode.dll") bytes)"
echo ""

# ---- 2. 编译 d3d11render.dll (MSVC) ----
echo "[2/3] 编译 d3d11render.dll ..."
MSVC_ROOT='C:\Program Files (x86)\Microsoft Visual Studio\2022\BuildTools\VC\Tools\MSVC\14.44.35207'
MSVC_BIN="$MSVC_ROOT\\bin\\Hostx64\\x64"
SDK_ROOT='C:\Program Files (x86)\Windows Kits\10'
SDK_VER='10.0.26100.0'

# 复制源文件到无中文路径，避免 cmd.exe 中文路径问题
TMP_BUILD="/c/tmp/d3d11build"
rm -rf "$TMP_BUILD"
mkdir -p "$TMP_BUILD"
cp "$FFDECODE_DIR/d3d11render.cpp" "$TMP_BUILD/"
cp "$FFDECODE_DIR/d3d11render_wgc.cpp" "$TMP_BUILD/"
cp "$FFDECODE_DIR/d3d11render.def" "$TMP_BUILD/"

powershell.exe -Command "& '${MSVC_BIN}\cl.exe' /LD /utf-8 /O2 /MT /EHa /std:c++17 C:\tmp\d3d11build\d3d11render.cpp C:\tmp\d3d11build\d3d11render_wgc.cpp /I'${MSVC_ROOT}\include' /I'${SDK_ROOT}\Include\${SDK_VER}\ucrt' /I'${SDK_ROOT}\Include\${SDK_VER}\um' /I'${SDK_ROOT}\Include\${SDK_VER}\shared' /I'${SDK_ROOT}\Include\${SDK_VER}\cppwinrt' /link /LIBPATH:'${MSVC_ROOT}\lib\x64' /LIBPATH:'${SDK_ROOT}\Lib\${SDK_VER}\ucrt\x64' /LIBPATH:'${SDK_ROOT}\Lib\${SDK_VER}\um\x64' d3d11.lib dxgi.lib d3dcompiler.lib User32.lib WindowsApp.lib /DEF:C:\tmp\d3d11build\d3d11render.def /OUT:C:\tmp\d3d11build\d3d11render.dll" 2>&1 | grep -E "error|warning|d3d11render"

if [ ! -f "$TMP_BUILD/d3d11render.dll" ]; then
    echo "  -> d3d11render.dll 编译失败!"
    exit 1
fi
cp "$TMP_BUILD/d3d11render.dll" "$PROJECT_ROOT/d3d11render.dll"
echo "  -> d3d11render.dll OK ($(stat -c%s "$PROJECT_ROOT/d3d11render.dll") bytes)"
echo ""

# ---- 3. 编译 go_legacy.exe (Go) ----
echo "[3/3] 编译 go_legacy.exe ..."
cd "$PROJECT_ROOT"
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build -o go_legacy.exe .
echo "  -> go_legacy.exe OK ($(stat -c%s "$PROJECT_ROOT/go_legacy.exe") bytes)"
echo ""

# ---- 4. 复制到 MYT ----
MYT_ROOT="$(cd "$PROJECT_ROOT/../MYT" && pwd)"
if [ -d "$MYT_ROOT" ]; then
    echo "[额外] 复制到 MYT ..."
    cp "$PROJECT_ROOT/go_legacy.exe" "$MYT_ROOT/bin/go_legacy.exe"
    cp "$PROJECT_ROOT/go_legacy.exe" "$MYT_ROOT/embedded/go_legacy.exe"
    echo "  -> MYT/bin/go_legacy.exe OK"
    echo "  -> MYT/embedded/go_legacy.exe OK"
fi

echo ""
echo "=========================================="
echo "  全部编译完成!"
echo "=========================================="
