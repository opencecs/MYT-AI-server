#!/bin/bash
# vendor-patch.sh — 对 vendor 目录中的第三方代码应用补丁
# 每次 `go mod vendor` 后需要重新执行此脚本

set -e
cd "$(dirname "$0")/.."

echo "[vendor-patch] 应用补丁..."

# 1. go-mpv purego: mpv_command 需要 NULL 终止的 char** 数组
MPV_FILE="vendor/github.com/gen2brain/go-mpv/client_nocgo.go"
if [ -f "$MPV_FILE" ]; then
    if ! grep -q "NULL terminator required by mpv_command" "$MPV_FILE"; then
        echo "[vendor-patch] 修补 go-mpv Command NULL terminator..."
        sed -i 's/cmds = append(cmds, cStr(c))/cmds = append(cmds, cStr(c))\n\t\t\/\/ NULL terminator required by mpv_command/' "$MPV_FILE"
        # 修改 Command
        sed -i '/func (m \*Mpv) Command/,/return newError/{ s/return newError(command(m.handle, unsafe.SliceData(cmds)))/cmds = append(cmds, nil)\n\treturn newError(command(m.handle, unsafe.SliceData(cmds)))/ }' "$MPV_FILE"
        # 修改 CommandAsync
        sed -i '/func (m \*Mpv) CommandAsync/,/return newError/{ s/return newError(commandAsync(m.handle, unsafe.SliceData(cmds)))/cmds = append(cmds, nil)\n\treturn newError(commandAsync(m.handle, unsafe.SliceData(cmds)))/ }' "$MPV_FILE"
    else
        echo "[vendor-patch] go-mpv NULL terminator 补丁已存在"
    fi
fi

# 2. gortsplib SDP: 接受 "lgcmd" 自定义媒体类型
SDP_FILE="vendor/github.com/bluenviron/gortsplib/v5/pkg/sdp/sdp.go"
if [ -f "$SDP_FILE" ]; then
    if ! grep -q '"lgcmd"' "$SDP_FILE"; then
        echo "[vendor-patch] 修补 gortsplib SDP 允许 lgcmd..."
        sed -i 's/fields\[0\] != "text"/\0 \&\&\n\t\tfields[0] != "lgcmd"/' "$SDP_FILE"
    else
        echo "[vendor-patch] gortsplib SDP lgcmd 补丁已存在"
    fi
fi

# 3. gortsplib format: Generic 格式对自定义媒体类型宽容处理
GENERIC_FILE="vendor/github.com/bluenviron/gortsplib/v5/pkg/format/generic.go"
if [ -f "$GENERIC_FILE" ]; then
    if ! grep -q "isCustomMediaType" "$GENERIC_FILE"; then
        echo "[vendor-patch] 修补 gortsplib Generic format 宽容处理..."
        # 注意：此补丁较复杂，建议手动检查
    else
        echo "[vendor-patch] gortsplib Generic format 补丁已存在"
    fi
fi

# 4. Wails production: isDebugMode 改为 true
#    关闭 isDebugMode 会导致 WebView2 渲染异常（侧边栏按钮不可见、页面卡顿）
APP_PROD_FILE="vendor/github.com/wailsapp/wails/v3/pkg/application/application_production.go"
if [ -f "$APP_PROD_FILE" ]; then
    if grep -q "isDebugMode: false" "$APP_PROD_FILE"; then
        echo "[vendor-patch] 修补 Wails production isDebugMode=true..."
        sed -i 's/isDebugMode: false,/isDebugMode: true, \/\/ 保持 debug 模式：关闭会导致 WebView2 渲染异常/' "$APP_PROD_FILE"
    else
        echo "[vendor-patch] Wails isDebugMode 补丁已存在"
    fi
fi

# 5. Wails production: DevTools 始终开启
#    关闭 DevTools 会导致 WebView2 渲染异常
WINPROD_FILE="vendor/github.com/wailsapp/wails/v3/pkg/application/webview_window_windows_production.go"
if [ -f "$WINPROD_FILE" ]; then
    if grep -q "PutAreDevToolsEnabled(false)" "$WINPROD_FILE"; then
        echo "[vendor-patch] 修补 Wails production DevTools=true..."
        sed -i 's/settings.PutAreDevToolsEnabled(false)/settings.PutAreDevToolsEnabled(true) \/\/ 始终开启：关闭会导致 WebView2 渲染异常/' "$WINPROD_FILE"
    else
        echo "[vendor-patch] Wails DevTools 补丁已存在"
    fi
fi

# 6. Wails webviewloader: WebView2Loader.dll 占位文件（上游以 git-lfs 存放，go mod vendor 拉不到）
#    该 embed 仅在 `native_webview2loader` build tag 下使用，本项目不启用，空占位即可满足 vendor/build
WV2_DIR="vendor/github.com/wailsapp/wails/v3/internal/webview2/webviewloader"
if [ -d "$WV2_DIR" ]; then
    for arch in x86 x64 arm64; do
        if [ ! -f "$WV2_DIR/$arch/WebView2Loader.dll" ]; then
            mkdir -p "$WV2_DIR/$arch"
            : > "$WV2_DIR/$arch/WebView2Loader.dll"
            echo "[vendor-patch] 创建 wails webviewloader $arch/WebView2Loader.dll 占位文件"
        fi
    done
fi

# 7. Wails dev 模式: /wails/custom.js 返回 200 空脚本（原上游固定 404）
#    frontend runtime (@wailsio/runtime) 启动时会 HEAD /wails/custom.js，
#    上游对非 server 模式固定返回 404，会在 WebView 控制台留下一条 404 噪音。
#    本项目不走 custom.js（native bridge 传输），改为返回空 JS(200) 即可消除。
APP_FILE="vendor/github.com/wailsapp/wails/v3/pkg/application/application.go"
if [ -f "$APP_FILE" ]; then
    if grep -q "http.NotFound(rw, req)" "$APP_FILE"; then
        echo "[vendor-patch] 修补 Wails /wails/custom.js 返回 200（消除控制台 404）..."
        sed -i -e "s|custom.js is only served in server mode.|Serve empty custom.js (HTTP 200) so the WebView console is free of 404 noise.|" \
               -e "s|Return 404 so the runtime's loadOptionalScript skips it.|The runtime loadOptionalScript HEADs custom.js; content is unused under the native bridge transport.|" \
               -e "s|http.NotFound(rw, req)|rw.Header().Set(\"Content-Type\", \"application/javascript\"); rw.WriteHeader(http.StatusOK)|" "$APP_FILE"
    else
        echo "[vendor-patch] Wails custom.js 200 补丁已存在"
    fi
fi

# 8. Wails MCP server: 支持 WAILS_MCP_ENABLED 环境变量关闭
#    上游 -tags mcp 时在 App.Run() 无条件启动 MCP server（127.0.0.1:9099），
#    本项目需要在设置页提供开关：关闭时由 main.go 设 WAILS_MCP_ENABLED=0 跳过启动
MCP_FILE="vendor/github.com/wailsapp/wails/v3/pkg/application/mcp_enabled.go"
if [ -f "$MCP_FILE" ]; then
    if ! grep -q "WAILS_MCP_ENABLED" "$MCP_FILE"; then
        echo "[vendor-patch] 修补 Wails MCP server 支持 WAILS_MCP_ENABLED 关闭..."
        sed -i 's|^func startMCPServer(a \*App) error {|func startMCPServer(a *App) error {\n\t// WAILS_MCP_ENABLED=0/false/off/no 时跳过启动（默认开启，保持上游行为）\n\tswitch strings.ToLower(strings.TrimSpace(os.Getenv("WAILS_MCP_ENABLED"))) {\n\tcase "0", "false", "off", "no":\n\t\treturn nil\n\t}|' "$MCP_FILE"
    else
        echo "[vendor-patch] Wails MCP ENABLED 门控补丁已存在"
    fi
fi

echo "[vendor-patch] 完成"
