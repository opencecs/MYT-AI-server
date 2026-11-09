#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
PLATFORM="${1:-$(uname -s)}"

# ── 开启 WebView2 CDP 远程调试端口（仅绑 127.0.0.1，截图/自动化取证用） ──
# setup.go 在 enableDebug 时设置 AdditionalBrowserArgs=--remote-debugging-port=9333；
# enableDebug 由 --debug 参数或 APP_DEBUG 环境变量触发，wails3 dev 默认不传，需显式设置。
# 环境变量 WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS 为双保险（两者值一致，无冲突）。
export APP_DEBUG=true
export WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS="--remote-debugging-port=9333"

# 确保 Windows Go 工具链和 Go 工具在 PATH 中（Git Bash/sh 模式下可能缺失）：
# - $HOME/go/bin（wails3 等 Go 工具）
# - C:/Program Files/Go/bin（go 工具链本体）
for d in "$HOME/go/bin" \
         "/c/Program Files/Go/bin" "/mnt/c/Program Files/Go/bin" "C:/Program Files/Go/bin"; do
  [ -d "$d" ] && export PATH="$PATH:$d"
done

# 确保 mingw gcc 在 PATH 中（CGO 编译需要，Wails syso 链接依赖）
if [ -d "/c/msys64/mingw64/bin" ]; then
  export PATH="/c/msys64/mingw64/bin:$PATH"
elif [ -d "/c/mingw64/bin" ]; then
  export PATH="/c/mingw64/bin:$PATH"
fi

cd "$PROJECT_DIR"

# 构建投屏播放器子模块 go_legacy 并复制到 embedded/（保证子模块改动生效）
# macOS 走 build_macos.sh（含 dylib）；子模块不存在时跳过（需自备 embedded/ 产物）
mkdir -p "${PROJECT_DIR}/embedded"
if [ -d "${PROJECT_DIR}/../myt-client-player" ]; then
  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*|Windows_NT)
      # Windows：编译 go_legacy.exe 并复制到 embedded/（wails3 dev embed 依赖）
      (cd "${PROJECT_DIR}/../myt-client-player" && go build -tags production -trimpath -ldflags="-w -s -H windowsgui" -o go_legacy.exe .)
      cp -f "${PROJECT_DIR}/../myt-client-player/go_legacy.exe" "${PROJECT_DIR}/embedded/go_legacy.exe"
      ;;
    Darwin)
      bash "${PROJECT_DIR}/../myt-client-player/scripts/build_macos.sh"
      ;;
  esac
fi

case "$PLATFORM" in
  Windows|windows|win)
    # 杀掉旧 myt.exe 及其 WebView2 子进程
    taskkill /IM myt.exe /F /T 2>/dev/null || true

    # 释放 vite 端口（仅杀占用 9245 的进程，不影响 LXCode 等其他 node 应用）
    powershell -NoProfile -Command 'Get-NetTCPConnection -LocalPort 9245 -State Listen -EA SilentlyContinue | ForEach-Object { Stop-Process -Id $_.OwningProcess -Force -EA SilentlyContinue }' || true

    # 清理 bin 下缓存数据（保留关键配置）
    rm -rf "${PROJECT_DIR}/bin/cache" 2>/dev/null || true
    # 备份登录凭据，清理后恢复（设备列表不恢复——按当前 deploy_mode 重新加载）
    CRED_BACKUP=""
    if [ -f "${PROJECT_DIR}/bin/config/credentials.json" ]; then
      CRED_BACKUP=$(cat "${PROJECT_DIR}/bin/config/credentials.json")
    fi
    rm -rf "${PROJECT_DIR}/bin/config" 2>/dev/null || true
    rm -rf "${PROJECT_DIR}/bin/data" 2>/dev/null || true
    rm -rf "${PROJECT_DIR}/bin/logs" 2>/dev/null || true
    # 恢复登录凭据
    mkdir -p "${PROJECT_DIR}/bin/config"
    if [ -n "$CRED_BACKUP" ]; then
      echo "$CRED_BACKUP" > "${PROJECT_DIR}/bin/config/credentials.json"
    fi

    sleep 2

    # 复制 libmpv.dll 到 bin 目录（vendor/mpv 不存在时跳过）
    mkdir -p "${PROJECT_DIR}/bin"
    if [ -f "${PROJECT_DIR}/vendor/mpv/windows64/libmpv.dll" ]; then
      cp -f "${PROJECT_DIR}/vendor/mpv/windows64/libmpv.dll" "${PROJECT_DIR}/bin/"
    fi

    # 确保前端依赖已安装
    if [ ! -d "${PROJECT_DIR}/frontend/node_modules" ]; then
      echo "[dev] 安装前端依赖..."
      (cd "${PROJECT_DIR}/frontend" && npm install --silent)
    fi

    # 重新生成 Wails 绑定（避免 Go 签名变更后前端 422 错误）
    wails3 generate bindings

    # wails3 dev 并行启动 Vite + Go 应用，run 任务内 wait-for-frontend.bat
    # 会轮询等待 Vite 就绪后再启动 Go 应用，避免连不上前端而 FATAL
    wails3 dev
    ;;
  Darwin|darwin|macos|mac|*)
    # 杀掉旧客户端进程
    pkill -f "myt.dev.app/Contents/MacOS/myt" 2>/dev/null || true

    # 释放 vite 开发服务器端口
    VITE_PORT=9245
    PID=$(lsof -ti:$VITE_PORT 2>/dev/null || true)
    if [ -n "$PID" ]; then
      kill -9 $PID 2>/dev/null || true
    fi

    sleep 1

    # 重新生成 Wails 绑定（避免 Go 签名变更后前端 422 错误）
    wails3 generate bindings

    wails3 dev
    ;;
esac
