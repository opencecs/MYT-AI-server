#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

# 禁用代理，避免 xray/Clash 等本地代理干扰构建过程中的网络请求
export NO_PROXY="*" no_proxy="*" HTTP_PROXY="" HTTPS_PROXY="" http_proxy="" https_proxy=""

# === 0. 正式构建自动递增版本号 ===
bump_version() {
  # 版本号文件为 Windows 构建专用，macOS 跳过
  case "$(uname -s)" in
    Darwin) echo "[build] macOS 构建，跳过版本号递增"; return ;;
  esac
  local OLD_VERSION=$(grep 'APP_VERSION:' "${PROJECT_DIR}/build/windows/Taskfile.yml" | head -1 | sed 's/.*: *"\(.*\)".*/\1/')
  if [ -z "$OLD_VERSION" ]; then
    echo "[build] 无法读取版本号，跳过递增"
    return
  fi
  MAJOR=$(echo "$OLD_VERSION" | cut -d. -f1)
  MINOR=$(echo "$OLD_VERSION" | cut -d. -f2)
  PATCH=$(echo "$OLD_VERSION" | cut -d. -f3)
  PATCH=$((PATCH + 1))
  if [ "$PATCH" -ge 10 ]; then
    PATCH=0
    MINOR=$((MINOR + 1))
    if [ "$MINOR" -ge 10 ]; then
      MINOR=0
      MAJOR=$((MAJOR + 1))
    fi
  fi
  APP_VERSION="${MAJOR}.${MINOR}.${PATCH}"
  echo "[build] 版本号: ${OLD_VERSION} → ${APP_VERSION}"
  sed -i "s/APP_VERSION: \".*\"/APP_VERSION: \"${APP_VERSION}\"/" "${PROJECT_DIR}/build/windows/Taskfile.yml"
  sed -i "s/version: \".*\"/version: \"${APP_VERSION}\"/" "${PROJECT_DIR}/build/config.yml"
  sed -i "s/\"file_version\": \".*\"/\"file_version\": \"${APP_VERSION}\"/" "${PROJECT_DIR}/build/windows/info.json"
  sed -i "s/\"ProductVersion\": \".*\"/\"ProductVersion\": \"${APP_VERSION}\"/" "${PROJECT_DIR}/build/windows/info.json"
}
bump_version

# === 0. 正式构建使用管理员权限（替换 manifest，仅 Windows）===
MANIFEST="${PROJECT_DIR}/build/windows/wails.exe.manifest"
MANIFEST_BACKUP="${MANIFEST}.dev"
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    if [ -f "$MANIFEST" ]; then
      cp -f "$MANIFEST" "$MANIFEST_BACKUP"
      sed -i 's/level="asInvoker"/level="requireAdministrator"/' "$MANIFEST"
      echo "[build] manifest: asInvoker → requireAdministrator"
    fi
    ;;
esac
restore_manifest() {
  if [ -f "$MANIFEST_BACKUP" ]; then
    mv -f "$MANIFEST_BACKUP" "$MANIFEST"
    echo "[build] manifest 已恢复为 asInvoker"
  fi
}
trap restore_manifest EXIT

# === 1. 构建投屏播放器 ===
PLAYER_DIR="${PROJECT_DIR}/../myt-client-player"
if [ -d "${PLAYER_DIR}" ]; then
  echo "=== 构建投屏播放器 go_legacy (production) ==="

  case "$(uname -s)" in
    MINGW*|MSYS*|CYGWIN*|Windows_NT)
      # 编译 go_legacy.exe
      cd "$PLAYER_DIR"
      go build -tags production -trimpath -ldflags="-w -s -H windowsgui" -o go_legacy.exe .

      cd "$PROJECT_DIR"
      mkdir -p "${PROJECT_DIR}/embedded"
      cp -f "${PLAYER_DIR}/go_legacy.exe" "${PROJECT_DIR}/embedded/go_legacy.exe" 2>/dev/null || true
      echo "=== go_legacy.exe 已复制到 embedded/ ==="
      ;;
    Darwin)
      # 编译 dylib + go_legacy
      cd "$PLAYER_DIR"
      bash scripts/build_macos.sh

      mkdir -p "${PROJECT_DIR}/embedded"
      cp -f "${PLAYER_DIR}/go_legacy" "${PROJECT_DIR}/embedded/go_legacy" 2>/dev/null || true
      echo "=== go_legacy 已复制到 embedded/ ==="
      cd "$PROJECT_DIR"
      ;;
  esac
fi

# === 2. 编译主程序 ===
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*|Windows_NT)
    wails3 task windows:build
    ;;
  Darwin)
    wails3 task darwin:build

    if [ -f "${PROJECT_DIR}/bin/go_legacy" ]; then
      APP_DIR="${PROJECT_DIR}/bin/myt.app/Contents/MacOS"
      if [ -d "$APP_DIR" ]; then
        cp -f "${PROJECT_DIR}/bin/go_legacy" "$APP_DIR/"
      fi
    fi
    ;;
  *)
    echo "不支持的操作系统: $(uname -s)"
    exit 1
    ;;
esac

echo "=== 构建完成 ==="
echo "提示：如需打包安装包，请运行："
echo "  bash scripts/package.sh"
