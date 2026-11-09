#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

# 禁用代理
export NO_PROXY="*" no_proxy="*" HTTP_PROXY="" HTTPS_PROXY="" http_proxy="" https_proxy=""

# === 1. 构建主程序（不签名） ===
echo "=== 步骤1: 构建主程序 ==="

# manifest 切换为管理员权限
MANIFEST="${PROJECT_DIR}/build/windows/wails.exe.manifest"
MANIFEST_BACKUP="${MANIFEST}.dev"
if [ -f "$MANIFEST" ]; then
  cp -f "$MANIFEST" "$MANIFEST_BACKUP"
  sed -i 's/level="asInvoker"/level="requireAdministrator"/' "$MANIFEST"
  echo "[build] manifest: asInvoker → requireAdministrator"
fi
restore_manifest() {
  if [ -f "$MANIFEST_BACKUP" ]; then
    mv -f "$MANIFEST_BACKUP" "$MANIFEST"
    echo "[build] manifest 已恢复为 asInvoker"
  fi
}
trap restore_manifest EXIT

# 构建投屏播放器
PLAYER_DIR="${PROJECT_DIR}/../myt-client-player"
if [ -d "${PLAYER_DIR}" ]; then
  echo "=== 构建投屏播放器 go_legacy (production) ==="
  cd "$PLAYER_DIR"
  go build -tags production -trimpath -ldflags="-w -s -H windowsgui" -o go_legacy.exe .
  cd "$PROJECT_DIR"
  mkdir -p "${PROJECT_DIR}/embedded"
  cp -f "${PLAYER_DIR}/go_legacy.exe" "${PROJECT_DIR}/embedded/go_legacy.exe" 2>/dev/null || true
  echo "=== go_legacy.exe 已复制到 embedded/ ==="
fi

# 清除前端 dev 产物，强制 production 重建
rm -rf "${PROJECT_DIR}/frontend/dist"

# 编译主程序
wails3 task windows:build
echo "=== 构建完成 ==="

# 读取版本号
APP_VERSION=$(awk '/^info:/{found=1} found && /^  version:/{gsub(/["'"'"']/, "", $2); print $2; exit}' "${PROJECT_DIR}/build/config.yml")
if [ -z "$APP_VERSION" ]; then
  echo "[error] 无法读取版本号"
  exit 1
fi
echo "当前版本号: ${APP_VERSION}"

# === 2. 生成在线更新 zip ===
echo "=== 步骤2: 生成在线更新包 ==="
UPDATE_DIR="${PROJECT_DIR}/dist/update"
mkdir -p "$UPDATE_DIR"

ZIP_NAME="myt-update-${APP_VERSION}-windows-amd64.zip"
ZIP_PATH="${UPDATE_DIR}/${ZIP_NAME}"
rm -f "$ZIP_PATH"

cd "${PROJECT_DIR}/bin"
if command -v zip &>/dev/null; then
  zip -r "$ZIP_PATH" myt.exe
else
  MYT_EXE_WIN=$(cygpath -w "${PROJECT_DIR}/bin/myt.exe")
  ZIP_WIN=$(cygpath -w "$ZIP_PATH")
  powershell.exe -Command "Compress-Archive -Path '${MYT_EXE_WIN}' -DestinationPath '${ZIP_WIN}' -Force"
fi
cd "$PROJECT_DIR"

SHA256_HASH=$(sha256sum "$ZIP_PATH" | awk '{print $1}')
FILE_SIZE=$(stat -c%s "$ZIP_PATH" 2>/dev/null || wc -c < "$ZIP_PATH")
echo "${SHA256_HASH}  ${ZIP_NAME}" > "${ZIP_PATH}.sha256"

echo ""
echo "==========================================="
echo " 打包完成！"
echo "==========================================="
echo ""
echo " 在线更新:"
echo "   ZIP:    ${ZIP_PATH}"
echo "   SHA256: ${SHA256_HASH}"
echo "   大小:   ${FILE_SIZE} bytes"
echo ""
echo " 后端 API 需返回:"
echo "   {"
echo "     \"version\": \"${APP_VERSION}\","
echo "     \"downloadUrl\": \"https://<CDN>/${ZIP_NAME}\","
echo "     \"checksum\": \"${SHA256_HASH}\","
echo "     \"fileSize\": ${FILE_SIZE},"
echo "     \"platform\": \"windows-amd64\""
echo "   }"
