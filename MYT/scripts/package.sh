#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

# 禁用代理
export NO_PROXY="*" no_proxy="*" HTTP_PROXY="" HTTPS_PROXY="" http_proxy="" https_proxy=""

# === 1. 完整构建 ===
echo "=== 步骤1: 构建 ==="
bash scripts/build.sh

# 读取版本号（取 config.yml 中 info 下的 version，跳过 YAML 文档版本）
APP_VERSION=$(awk '/^info:/{found=1} found && /^  version:/{gsub(/["'\'']/, "", $2); print $2; exit}' "${PROJECT_DIR}/build/config.yml")
if [ -z "$APP_VERSION" ]; then
  echo "[error] 无法读取版本号"
  exit 1
fi
echo "当前版本号: ${APP_VERSION}"

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    # === 2. 生成 NSIS 安装包 ===
    echo "=== 步骤2: 打包 NSIS 安装包 ==="
    export PATH="/c/Program Files (x86)/NSIS:$PATH"
    MYT_EXE=$(cygpath -w "${PROJECT_DIR}/bin/myt.exe")

    cd "${PROJECT_DIR}/build/windows/nsis"
    makensis -DARG_WAILS_AMD64_BINARY="${MYT_EXE}" project.nsi
    cd "$PROJECT_DIR"

    # === 3. 整理安装包 ===
    INSTALLER=$(ls -t "${PROJECT_DIR}/bin/"*-installer*.exe 2>/dev/null | head -1)
    if [ -n "$INSTALLER" ]; then
      echo "=== 步骤3: 整理安装包 ==="

      DIST_DIR="${PROJECT_DIR}/dist"
      mkdir -p "${DIST_DIR}"
      NEW_NAME="${DIST_DIR}/myt-${APP_VERSION}-amd64-installer.exe"
      mv "$INSTALLER" "$NEW_NAME"
      INSTALLER="$NEW_NAME"
      echo "安装包: ${INSTALLER}"
    fi

    # === 4. 生成在线更新 zip ===
    echo "=== 步骤4: 生成在线更新包 ==="
    UPDATE_DIR="${PROJECT_DIR}/dist/update"
    mkdir -p "${UPDATE_DIR}"

    ZIP_NAME="myt-update-${APP_VERSION}-windows-amd64.zip"
    ZIP_PATH="${UPDATE_DIR}/${ZIP_NAME}"
    rm -f "$ZIP_PATH"
    if command -v zip &>/dev/null; then
      cd "${PROJECT_DIR}/bin"
      zip -r "$ZIP_PATH" myt.exe
      cd "$PROJECT_DIR"
    else
      MYT_EXE_WIN=$(cygpath -w "${PROJECT_DIR}/bin/myt.exe")
      ZIP_WIN=$(cygpath -w "$ZIP_PATH")
      powershell.exe -Command "Compress-Archive -Path '${MYT_EXE_WIN}' -DestinationPath '${ZIP_WIN}' -Force"
    fi

    SHA256_HASH=$(sha256sum "$ZIP_PATH" | awk '{print $1}')
    FILE_SIZE=$(stat -c%s "$ZIP_PATH" 2>/dev/null || wc -c < "$ZIP_PATH")
    echo "${SHA256_HASH}  ${ZIP_NAME}" > "${ZIP_PATH}.sha256"

    echo ""
    echo "==========================================="
    echo " 打包完成！"
    echo "==========================================="
    echo ""
    echo " 安装包: ${INSTALLER}"
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
    ;;

  Darwin)
    # === 2. 打包 .app bundle ===
    echo "=== 步骤2: 打包 .app bundle ==="
    wails3 task darwin:package

    # 重命名 bundle：wails 默认生成 myt.app，重命名为 魔云腾-AI.app
    # （Finder/DMG 里显示名 = .app 文件名，CFBundleName 仅在文件名为英文时回退使用）
    # 必须先清掉残留的旧中文 bundle，否则 [ ! -d ] 条件跳过重命名 → 打包的是旧版本 bundle
    APP_BUNDLE="${PROJECT_DIR}/bin/魔云腾-AI.app"
    rm -rf "$APP_BUNDLE"
    if [ -d "${PROJECT_DIR}/bin/myt.app" ]; then
      mv "${PROJECT_DIR}/bin/myt.app" "$APP_BUNDLE"
      echo "  -> bundle 重命名: myt.app → 魔云腾-AI.app"
    fi

    # 复制 go_legacy 和 dylib 到 .app bundle
    if [ -f "${PROJECT_DIR}/bin/go_legacy" ]; then
      cp -f "${PROJECT_DIR}/bin/go_legacy" "${APP_BUNDLE}/Contents/MacOS/"
      echo "  -> go_legacy 已复制到 .app bundle"
    fi

    # 复制 dylib 到 .app bundle (Framework 路径)
    PLAYER_DIR="${PROJECT_DIR}/../myt-client-player"
    FRAMEWORKS_DIR="${APP_BUNDLE}/Contents/Frameworks"
    mkdir -p "$FRAMEWORKS_DIR"
    for DYLIB in "$PLAYER_DIR"/*.dylib; do
      if [ -f "$DYLIB" ]; then
        cp -f "$DYLIB" "$FRAMEWORKS_DIR/"
        echo "  -> $(basename "$DYLIB") 已复制到 .app bundle"
      fi
    done

    # xattr 清理 macOS Sequoia 15+ 自动添加的 provenance 属性，避免 Gatekeeper 静默拒绝
    xattr -cr "${APP_BUNDLE}" 2>/dev/null || true

    # === 3. 生成 DMG ===
    echo "=== 步骤3: 生成 DMG ==="
    DIST_DIR="${PROJECT_DIR}/dist"
    mkdir -p "${DIST_DIR}"
    DMG_NAME="魔云腾-AI-${APP_VERSION}-macos-arm64.dmg"
    DMG_PATH="${DIST_DIR}/${DMG_NAME}"
    DMG_STAGING="/tmp/myt-dmg-staging"

    rm -rf "$DMG_STAGING" "$DMG_PATH"
    mkdir -p "$DMG_STAGING"
    cp -R "${APP_BUNDLE}" "$DMG_STAGING/"
    ln -sf /Applications "$DMG_STAGING/Applications"

    hdiutil create -volname "魔云腾-AI" \
      -srcfolder "$DMG_STAGING" \
      -ov -format UDZO \
      "$DMG_PATH"

    rm -rf "$DMG_STAGING"

    # 给 DMG 加 quarantine 属性（模拟网络下载），使挂载后运行时触发 Gatekeeper
    # App Translocation，bundle 复制到随机路径运行，正常启动。属性随 DMG → 挂载 → 复制传递。
    xattr -w com.apple.quarantine "0083;5f123456;myt;com.moyunteng.v3ai" "$DMG_PATH" 2>/dev/null || true
    echo "  -> DMG: ${DMG_PATH}（已加 quarantine 属性，触发 App Translocation）"

    # === 4. 生成在线更新 zip ===
    echo "=== 步骤4: 生成在线更新包 ==="
    UPDATE_DIR="${PROJECT_DIR}/dist/update"
    mkdir -p "${UPDATE_DIR}"

    ZIP_NAME="myt-update-${APP_VERSION}-macos-arm64.zip"
    ZIP_PATH="${UPDATE_DIR}/${ZIP_NAME}"
    rm -f "$ZIP_PATH"

    # 只打包 myt 主程序（go_legacy 和 dylib 已 embed 在主程序中，运行时自动提取）
    cd "${PROJECT_DIR}/bin"
    zip -r "$ZIP_PATH" myt
    cd "$PROJECT_DIR"

    SHA256_HASH=$(shasum -a 256 "$ZIP_PATH" | awk '{print $1}')
    FILE_SIZE=$(stat -f%z "$ZIP_PATH")
    echo "${SHA256_HASH}  ${ZIP_NAME}" > "${ZIP_PATH}.sha256"

    echo ""
    echo "==========================================="
    echo " 打包完成！"
    echo "==========================================="
    echo ""
    echo " DMG:    ${DMG_PATH}"
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
    echo "     \"platform\": \"macos-arm64\""
    echo "   }"
    ;;

  *)
    echo "不支持的操作系统: $(uname -s)"
    exit 1
    ;;
esac
