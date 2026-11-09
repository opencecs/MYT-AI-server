#!/usr/bin/env bash
# scripts/install-sdk.sh —— 面向客户的「首次安装」一体化脚本（自包含，幂等）
#
# 交付形态（三选一，都能装）：
#   A. 传包安装：客户拿到两个文件，执行一条命令即可完成安装：
#      tebox-sdk.zip    安装包（zip 根目录三件套：linux/arm64 二进制 tobox_player
#                       + hash（二进制 sha256 hex）+ version（版本号数字））；
#                       推荐传到 /mmc/ 下
#      install-sdk.sh   本脚本（放任意目录均可）
#   B. 远端取包（newapi 两步取包，默认联网路径）：只传本脚本，本地没有 zip 时
#      自动走 newapi（对齐 myt-sdk-go v3 的 newApi.go 流程）：
#        1) GET {base}/api/v1/sdk_ver/latest?sdk_type=tebox_sdk → 最新版本号
#        2) 已装版本 ≥ 远端版本时提示「已是最新」直接退出（不重复安装）
#        3) GET {base}/api/v1/sdk/download-url?version=N&filename=tebox-sdk.zip
#           &sdk_type=tebox_sdk → 服务端下发的真实 zip 地址
#        4) 下载该 zip（zip 内容与 build.sh 产出的 tebox-sdk.zip 相同：
#           tobox_player + hash + version 三件套，hash/version 核对后安装）
#   C. 直链下载：--download-url 或 TOBOX_DOWNLOAD_URL 指定完整 zip URL（通用模式）
#   D. 同目录安装：tebox-sdk.zip 与本脚本放同一目录（兼容旧用法）
#
# zip 查找/下载顺序：--zip 显式指定 > /mmc/tebox-sdk.zip 本地 > newapi 远端
# （两步取包，默认）> 直链（--download-url / TOBOX_DOWNLOAD_URL / SDK_DOWNLOAD_BASE）
# > 本脚本同目录。
#
#   sudo bash install-sdk.sh
#
# 脚本从 zip 里解出一切（二进制、可选 config），不依赖项目目录结构。
# 全程带 [N/8] 进度与 ✓/✘ 状态输出；每一步失败即退出并给出修复提示。
#
# 做的事（可重复执行，不会破坏已就绪的安装）：
#   [1/8] 创建目录结构（安装目录 + 镜像根目录）
#   [2/8] 解压 tebox-sdk.zip → 校验三件套（tobox_player+hash+version）
#         → 核对二进制 sha256 → 版本防降级检查 → 备份旧二进制 → 原子就位
#   [3/8] 二进制自检（-check 输出版本号）
#   [4/8] 写 config.yaml（已存在则保留；优先级：脚本旁 config.yaml > zip 内 config.yaml > 内置默认模板）
#   [5/8] 安装 run.sh 守护脚本（内置，自更新/回滚依赖它常驻拉起）
#   [6/8] 配置 crontab @reboot 自启（幂等：已有则跳过）
#   [7/8] 启动服务（已在跑且健康则跳过；守护在跑则等它拉起）
#   [8/8] 健康验证：/health 轮询（最多 30s）+ /self/version 显示版本
#
# 用法：
#   sudo bash install-sdk.sh [选项]
#   bash install-sdk.sh                     # 非 root 时自动 sudo 提权
#   TOBOX_SUDO_PASS=myt bash install-sdk.sh # 无人值守提权（密码从环境变量读）
#   bash install-sdk.sh --help
#
# 选项：
#   --host <ip:port>        tebox-api 的 gRPC 地址（写入 config 的 tebox.address；
#                           也可用环境变量 TOBOX_TEBOX_ADDR），默认 127.0.0.1:50051
#   --port <port>           SDK HTTP 监听端口，默认 8088
#   --dir <path>            安装目录，默认 /home/user/tobox_player
#   --image-root <path>     镜像管理根目录（自动创建），默认 /mmc/tebox/user-images
#   --zip <path>            显式指定 tebox-sdk.zip 路径（优先级最高，跳过远端取包）
#   --api-base <url>        newapi 服务基地址（默认 NEWAPI_BASE 常量；测试用）
#   --download-url <url>    直链下载 tebox-sdk.zip 的完整 URL（http/https；
#                           也可用环境变量 TOBOX_DOWNLOAD_URL；指定后覆盖 newapi 取包）
#   --sha256 <hex>          下载包的 sha256 校验值（可选；不指定则用 zip 内
#                           hash 文件核对二进制完整性）
#   --force                 跳过版本防降级检查（允许安装 ≤ 当前版本的包）
#                           （对 newapi 取包：即使远端版本 ≤ 本机也重新安装）
#   -h, --help              显示本帮助
#
# 内置默认 config 要点：clientMaxBodySize 16G、readTimeout 1h、
# screenshot/terminal 均默认开启（显式置 false 才会关闭）、
# selfupdate.enabled true（可用 deploy.sh 远程热更新，
# 默认凭据 admin/myt —— 上线前务必修改）。
#
# 退出码：0 成功（含「已安装且运行中」）；1 安装/启动失败；2 参数错误。

set -euo pipefail

# ========================= 可调参数（默认值） =========================
ZIP_NAME="tebox-sdk.zip"
ZIP_DEFAULT="/mmc/$ZIP_NAME"
INSTALL_DIR="/home/user/tobox_player"
BIN_NAME="tobox_player"
PORT="8088"
TEBOX_ADDR="${TOBOX_TEBOX_ADDR:-127.0.0.1:50051}"
IMAGE_ROOT="/mmc/tebox/user-images"
SKIP_ARCH_CHECK=0
# 远端下载（直链模式）：固定交付服务器 base URL。留空表示未配置——直链环节跳过。
# 配置方式：改这里，或运行时用 --download-url <完整zip URL> /
# 环境变量 TOBOX_DOWNLOAD_URL 覆盖。
SDK_DOWNLOAD_BASE=""
SDK_ZIP_PATH="tebox-sdk.zip"     # 拼在 base 后的相对路径

# newapi 两步取包（对齐 myt-sdk-go v3 utility/newApi/newApi.go）：
#   1) GET {NEWAPI_BASE}/api/v1/sdk_ver/latest?sdk_type={SDK_TYPE}
#      → JSON data.latest_version（字符串数字）
#   2) GET {NEWAPI_BASE}/api/v1/sdk/download-url?version=N&filename={SDK_FILENAME}
#      &sdk_type={SDK_TYPE} → JSON data.download_url（服务端下发的真实 zip 地址）
# 上传到 tebox_sdk 类型的包用 ./scripts/upload-tebox-sdk.py（build.sh 产物直接上传）。
NEWAPI_BASE="https://newapi.moyunteng.com"
SDK_TYPE="tebox_sdk"
SDK_FILENAME="tebox-sdk.zip"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ZIP_PATH=""          # 空表示未显式指定，解析后按候选顺序兜底
API_BASE=""          # --api-base：覆盖 NEWAPI_BASE（测试用）
DOWNLOAD_URL="${TOBOX_DOWNLOAD_URL:-}"
EXPECT_SHA256=""     # --sha256：对下载的 zip 做整体校验（可选）
FORCE_INSTALL=0      # --force：跳过版本防降级

usage() {
  cat <<EOF
用法: bash install-sdk.sh [选项]

面向客户的首次安装一体化脚本（自包含）：执行一条命令完成安装
（解包 → 守护 → 配置 → 自启 → 启动 → 验证）。

tebox-sdk.zip 的获取顺序：
  1. --zip 显式指定的路径
  2. /mmc/$ZIP_NAME（推荐：把 zip 传到 /mmc 下，离线安装，不打远端）
  3. newapi 两步取包（本地没有 zip 时的默认路径，需联网）：
     查最新版本 → 已装且不比远端新则退出 → 换下载地址 → 下载
     （--api-base 可覆盖 newapi 基地址）
  4. 直链下载：--download-url <完整zip URL> 或环境变量 TOBOX_DOWNLOAD_URL
     （脚本顶部 SDK_DOWNLOAD_BASE 配置了交付服务器时，也可不带参数直接拉）
  5. 本脚本所在目录下的 $ZIP_NAME

zip 包格式（三件套，build.sh 产出；newapi 下发的包与此格式一致）：
  tobox_player   linux/arm64 二进制
  hash           tobox_player 的 sha256 hex（安装时核对，防篡改）
  version        版本号数字（安装时做防降级检查：≤ 当前版本默认跳过，--force 覆盖）
  ⚠ 上传到 newapi（scripts/upload-tebox-sdk.py）的包必须含 hash 与 version 文件
    （即 build.sh 产出的 tebox-sdk.zip 原样上传），否则安装时校验失败。

选项:
  --host <ip:port>     tebox-api gRPC 地址（默认 127.0.0.1:50051，
                       也可用环境变量 TOBOX_TEBOX_ADDR）
  --port <port>        SDK HTTP 监听端口（默认 8088）
  --dir <path>         安装目录（默认 /home/user/tobox_player）
  --image-root <path>  镜像管理根目录（默认 /mmc/tebox/user-images）
  --zip <path>         显式指定 tebox-sdk.zip 路径（优先级最高，跳过远端取包）
  --api-base <url>     newapi 服务基地址（默认 $NEWAPI_BASE，测试用）
  --download-url <url> 直链下载 tebox-sdk.zip 的完整 URL（覆盖 newapi 取包）
                       （也可用环境变量 TOBOX_DOWNLOAD_URL）
  --sha256 <hex>       下载 zip 的 sha256 校验值（可选，64 位 hex；
                       不指定则用 zip 内 hash 文件核对二进制）
  --force              跳过版本防降级检查（允许安装 ≤ 当前版本的包）
  -h, --help           显示本帮助

前置条件:
  - tebox-sdk.zip（本地 / newapi 远端 / 直链 / 同目录，任一即可；
    zip 根目录含 tobox_player + hash + version）
    推荐放到 /mmc/ 下，或与本脚本放同一目录，或用 --zip/--download-url 指定
  - newapi 取包/直链下载需要 curl 或 wget
  - 可选：脚本同目录放 config.yaml（存在则优先使用，否则用内置默认模板生成）
  - root 或免密 sudo（装 crontab、写 /mmc 镜像目录、启动服务）

示例:
  sudo bash install-sdk.sh                       # /mmc 有包用本地，没有则走 newapi 取最新
  sudo bash install-sdk.sh --host 192.168.1.20:50051 --port 8088
  sudo bash install-sdk.sh --zip /tmp/tebox-sdk.zip
  sudo bash install-sdk.sh --force               # 远端同版本也强制重装
  TOBOX_SUDO_PASS=myt bash install-sdk.sh --dir /opt/tobox
EOF
}

# ========================= 参数解析 =========================
# 原始参数先存档：非 root 提权 re-exec 本脚本时要原样传下去（否则 --download-url 等丢失）
ORIG_ARGS=("$@")
while [ $# -gt 0 ]; do
  case "$1" in
    --host)       [ $# -ge 2 ] || { echo "✘ --host 需要参数" >&2; exit 2; }; TEBOX_ADDR="$2"; shift 2 ;;
    --port)       [ $# -ge 2 ] || { echo "✘ --port 需要参数" >&2; exit 2; }; PORT="$2"; shift 2 ;;
    --dir)        [ $# -ge 2 ] || { echo "✘ --dir 需要参数" >&2; exit 2; }; INSTALL_DIR="$2"; shift 2 ;;
    --image-root) [ $# -ge 2 ] || { echo "✘ --image-root 需要参数" >&2; exit 2; }; IMAGE_ROOT="$2"; shift 2 ;;
    --zip)        [ $# -ge 2 ] || { echo "✘ --zip 需要参数" >&2; exit 2; }; ZIP_PATH="$2"; shift 2 ;;
    --api-base)   [ $# -ge 2 ] || { echo "✘ --api-base 需要参数" >&2; exit 2; }; API_BASE="$2"; shift 2 ;;
    --download-url) [ $# -ge 2 ] || { echo "✘ --download-url 需要参数" >&2; exit 2; }; DOWNLOAD_URL="$2"; shift 2 ;;
    --sha256)     [ $# -ge 2 ] || { echo "✘ --sha256 需要参数" >&2; exit 2; }; EXPECT_SHA256="$2"; shift 2 ;;
    --force)      FORCE_INSTALL=1; shift ;;
    --skip-arch-check) SKIP_ARCH_CHECK=1; shift ;;   # 测试用隐藏参数
    -h|--help)    usage; exit 0 ;;
    "")           shift ;;
    *)            echo "未知参数: $1" >&2; echo >&2; usage >&2; exit 2 ;;
  esac
done

case "$PORT" in
  ''|*[!0-9]*) echo "✘ --port 必须是数字端口: $PORT" >&2; exit 2 ;;
esac
case "$TEBOX_ADDR" in
  *:*) : ;;
  *)   echo "✘ --host 需要形如 ip:port 的地址: $TEBOX_ADDR" >&2; exit 2 ;;
esac

case "$EXPECT_SHA256" in
  '') : ;;
  *[!0-9a-fA-F]*|[!0-9a-fA-F]*) echo "✘ --sha256 需要 64 位 hex: $EXPECT_SHA256" >&2; exit 2 ;;
esac
if [ -n "$EXPECT_SHA256" ] && [ "${#EXPECT_SHA256}" -ne 64 ]; then
  echo "✘ --sha256 需要 64 位 hex（当前 ${#EXPECT_SHA256} 位）: $EXPECT_SHA256" >&2; exit 2
fi

NEWAPI_BASE="$([ -n "$API_BASE" ] && printf '%s' "$API_BASE" || printf '%s' "$NEWAPI_BASE")"
NEWAPI_BASE="${NEWAPI_BASE%/}"   # 去掉末尾斜杠，统一拼接
case "$NEWAPI_BASE" in
  http://*|https://*) : ;;
  *) echo "✘ --api-base 需要 http(s):// 开头的完整 URL: $NEWAPI_BASE" >&2; exit 2 ;;
esac

log()  { printf '==> %s\n' "$*"; }
ok()   { printf '✔ %s\n' "$*"; }
warn() { printf '⚠ %s\n' "$*" >&2; }
die()  { printf '✘ %s\n' "$*" >&2; exit 1; }
step() { printf '\n[%d/8] %s\n' "$1" "$2"; }

# ========================= sha256 工具 =========================
# 返回 "sha256sum 可用" 之类的形态不统一，这里统一出 hex（小写）或空。
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    printf ''
  fi
}
SHA256_AVAILABLE=1
if ! command -v sha256sum >/dev/null 2>&1 && ! command -v shasum >/dev/null 2>&1; then
  SHA256_AVAILABLE=0
fi

# zip 缺失的检查放在提权之前：普通用户也能直接看到最相关的错误。
# 获取顺序：--zip 显式指定 > /mmc/tebox-sdk.zip 本地 > newapi 两步取包（默认）
#           > 直链（--download-url / TOBOX_DOWNLOAD_URL / SDK_DOWNLOAD_BASE）
#           > 脚本同目录。
DOWNLOAD_TMP=""   # 非空表示 zip 是本次下载的，用完要清理
if [ -n "$ZIP_PATH" ]; then
  [ -f "$ZIP_PATH" ] || die "--zip 指定的文件不存在: $ZIP_PATH
（默认候选位置：$ZIP_DEFAULT 或本脚本同目录；远端可用 newapi 自动取包或 --download-url <url>）"
else
  # 下载器：curl 优先，wget 兜底（两者都没有时禁用远端取包）
  HAVE_DOWNLOADER=""
  if command -v curl >/dev/null 2>&1; then HAVE_DOWNLOADER="curl"
  elif command -v wget >/dev/null 2>&1; then HAVE_DOWNLOADER="wget"
  fi

  # download_zip <url> <目标文件>：带进度下载（curl 显示大小/百分比，wget 自带进度）
  download_zip() {
    local url="$1" dst="$2" rc=0
    log "从远端下载: $url"
    case "$HAVE_DOWNLOADER" in
      curl)
        # -# 进度条 + http 设置：下载大小/百分比/耗时；失败重试 2 次
        curl -L --fail --retry 2 --connect-timeout 15 --progress-bar \
             -o "$dst" "$url" || rc=$?
        ;;
      wget)
        wget --tries=2 --timeout=15 -O "$dst" "$url" || rc=$?
        ;;
      *)
        echo "系统缺少 curl 和 wget，无法远端下载。请安装其一，或改用本地包：
  --zip <path> 指定本地 zip（推荐传到 $ZIP_DEFAULT）" >&2
        return 3
        ;;
    esac
    return "$rc"
  }

  # ---- newapi 两步取包（对齐 myt-sdk-go v3 utility/newApi/newApi.go）----
  # 返回 0 并把 zip 下到 $1（目标文件）= 成功；非 0 = newapi 不可用/无包，调用方回退。
  newapi_fetch_zip() {
    local dst="$1" api latest_json latest_ver dl_json dl_url
    # 第 1 步：查最新版本（GET /api/v1/sdk_ver/latest?sdk_type=tebox_sdk）
    api="$NEWAPI_BASE/api/v1"
    if [ "$HAVE_DOWNLOADER" = "curl" ]; then
      latest_json="$(curl -sS --fail --connect-timeout 15 --max-time 30 \
        "$api/sdk_ver/latest?sdk_type=$SDK_TYPE" 2>/dev/null)" || latest_json=""
    else
      latest_json="$(wget -qO- --timeout=15 "$api/sdk_ver/latest?sdk_type=$SDK_TYPE" 2>/dev/null)" || latest_json=""
    fi
    if [ -z "$latest_json" ]; then
      warn "newapi 最新版本查询失败（无外网/服务不可达）: $api/sdk_ver/latest?sdk_type=$SDK_TYPE"
      return 1
    fi
    # data.latest_version 是字符串数字：grep -o 提取（容忍冒号后空格/数字非引号；
    # python3 兜底解析）
    latest_ver="$(printf '%s' "$latest_json" \
      | grep -oE '"latest_version"[[:space:]]*:[[:space:]]*"?[0-9]+' | head -n1 | grep -o '[0-9]\+$' || true)"
    if [ -z "$latest_ver" ] && command -v python3 >/dev/null 2>&1; then
      latest_ver="$(printf '%s' "$latest_json" | python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("data",{}).get("latest_version","") or "")
except Exception: pass' 2>/dev/null || true)"
    fi
    case "$latest_ver" in
      ''|*[!0-9]*)
        warn "newapi latest 响应解析失败（无 latest_version 或非数字）: ${latest_json:0:200}"
        return 1 ;;
    esac
    # 本机已装版本（-check 输出整型版本号；拿不到则 unknown，不跳过）
    local cur_ver=""
    if [ -x "$INSTALL_DIR/$BIN_NAME" ]; then
      cur_ver="$("$INSTALL_DIR/$BIN_NAME" -check 2>/dev/null | head -n1 | tr -d ' \t\r\n' || true)"
      case "$cur_ver" in ''|*[!0-9]*) cur_ver="" ;; esac
    fi
    if [ -n "$cur_ver" ]; then
      log "远端最新版本: v$latest_ver，当前已装: v$cur_ver"
      if [ "$latest_ver" -le "$cur_ver" ] && [ "$FORCE_INSTALL" -eq 0 ]; then
        ok "已是最新版本（v$cur_ver ≥ 远端 v$latest_ver），无需安装"
        exit 0
      fi
      if [ "$latest_ver" -le "$cur_ver" ]; then
        warn "远端版本 v$latest_ver ≤ 当前已装 v$cur_ver（--force 指定，强制重新安装）"
      fi
    else
      log "远端最新版本: v$latest_ver，当前已装: unknown（未安装或无法获取）"
    fi
    # 第 2 步：换真实下载地址（GET /api/v1/sdk/download-url?version=N&filename=...&sdk_type=...）
    if [ "$HAVE_DOWNLOADER" = "curl" ]; then
      dl_json="$(curl -sS --fail --connect-timeout 15 --max-time 30 \
        "$api/sdk/download-url?version=$latest_ver&filename=$SDK_FILENAME&sdk_type=$SDK_TYPE" 2>/dev/null)" || dl_json=""
    else
      dl_json="$(wget -qO- --timeout=15 "$api/sdk/download-url?version=$latest_ver&filename=$SDK_FILENAME&sdk_type=$SDK_TYPE" 2>/dev/null)" || dl_json=""
    fi
    if [ -z "$dl_json" ]; then
      warn "newapi 下载地址查询失败: $api/sdk/download-url?version=$latest_ver&sdk_type=$SDK_TYPE"
      return 1
    fi
    dl_url="$(printf '%s' "$dl_json" \
      | grep -oE '"download_url"[[:space:]]*:[[:space:]]*"[^"]*"' | head -n1 \
      | sed 's/^"download_url"[[:space:]]*:[[:space:]]*"//; s/"$//' || true)"
    if [ -z "$dl_url" ] && command -v python3 >/dev/null 2>&1; then
      dl_url="$(printf '%s' "$dl_json" | python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("data",{}).get("download_url","") or "")
except Exception: pass' 2>/dev/null || true)"
    fi
    case "$dl_url" in
      https://*|http://*) : ;;
      *)
        warn "newapi download-url 响应解析失败（无合法 download_url）: ${dl_json:0:200}"
        return 1 ;;
    esac
    # 第 3 步：下载 zip（带进度）
    download_zip "$dl_url" "$dst"
  }

  if [ -f "$ZIP_DEFAULT" ]; then
    ZIP_PATH="$ZIP_DEFAULT"
  else
    DL_DIR="${TMPDIR:-/tmp}"
    mkdir -p "$DL_DIR" 2>/dev/null || DL_DIR="$HOME"
    DOWNLOAD_TMP="$DL_DIR/tobox-sdk-download.$$.$ZIP_NAME"

    # 优先级 3：newapi 两步取包（默认联网路径；直链指定时跳过 newapi）
    FETCHED=0
    if [ -z "$DOWNLOAD_URL" ]; then
      if [ "$SHA256_AVAILABLE" -eq 0 ]; then
        warn "本机没有 sha256sum/shasum，远端取包后无法核对完整性，跳过 newapi 取包"
      elif [ -z "$HAVE_DOWNLOADER" ]; then
        warn "本机没有 curl/wget，无法 newapi 远端取包"
      elif newapi_fetch_zip "$DOWNLOAD_TMP" && [ -s "$DOWNLOAD_TMP" ]; then
        FETCHED=1
      fi
    fi

    # 优先级 4：直链（--download-url / TOBOX_DOWNLOAD_URL / SDK_DOWNLOAD_BASE）
    if [ "$FETCHED" -eq 0 ]; then
      rm -f "$DOWNLOAD_TMP" 2>/dev/null || true
      if [ -z "$DOWNLOAD_URL" ] && [ -n "$SDK_DOWNLOAD_BASE" ]; then
        # 去掉 base 末尾斜杠后拼接固定包名（服务端只需把 build 产物放到该目录）
        DOWNLOAD_URL="${SDK_DOWNLOAD_BASE%/}/$SDK_ZIP_PATH"
      fi
      if [ -n "$DOWNLOAD_URL" ]; then
        case "$DOWNLOAD_URL" in
          http://*|https://*) : ;;
          *) die "--download-url / TOBOX_DOWNLOAD_URL 需要 http(s):// 开头的完整 URL: $DOWNLOAD_URL" ;;
        esac
        if [ "$SHA256_AVAILABLE" -eq 0 ]; then
          die "远端下载需要 sha256sum/shasum 做完整性校验，本机两者都没有。
请安装（busybox 通常自带 sha256sum），或改用本地包（--zip / $ZIP_DEFAULT）。"
        fi
        if [ -z "$HAVE_DOWNLOADER" ]; then
          die "远端下载需要 curl 或 wget，本机两者都没有。
请安装其一，或改用本地包（--zip / $ZIP_DEFAULT）。"
        fi
        log "本地未找到 $ZIP_DEFAULT，尝试直链下载"
        if download_zip "$DOWNLOAD_URL" "$DOWNLOAD_TMP" && [ -s "$DOWNLOAD_TMP" ]; then
          FETCHED=1
        else
          rm -f "$DOWNLOAD_TMP" 2>/dev/null || true
          die "远端下载失败: $DOWNLOAD_URL
排查：
  curl -I $DOWNLOAD_URL          # 看状态码/网络是否可达
  交付服务器未上传包？或改用本地方式：
  --zip <path> / 把 zip 传到 $ZIP_DEFAULT"
        fi
      fi
    fi

    if [ "$FETCHED" -eq 1 ]; then
      # 整包 sha256 校验（--sha256 指定时）
      DL_SHA="$(sha256_of "$DOWNLOAD_TMP")"
      if [ -n "$EXPECT_SHA256" ]; then
        if [ "$DL_SHA" != "$(printf '%s' "$EXPECT_SHA256" | tr 'A-Z' 'a-z')" ]; then
          rm -f "$DOWNLOAD_TMP"
          die "下载包 sha256 校验失败：
  期望: $EXPECT_SHA256
  实际: $DL_SHA
（包可能被篡改或下载不完整；确认 URL 正确或向交付方核对 --sha256 值）"
        fi
        ok "下载包 sha256 校验通过: $DL_SHA"
      else
        ok "下载完成（$(ls -lh "$DOWNLOAD_TMP" | awk '{print $5}')，sha256: ${DL_SHA:0:16}…）
提示: 可用 --sha256 <hex> 对整个下载包做强校验（向交付方索取校验值）"
      fi
      ZIP_PATH="$DOWNLOAD_TMP"
    elif [ -f "$SCRIPT_DIR/$ZIP_NAME" ]; then
      ZIP_PATH="$SCRIPT_DIR/$ZIP_NAME"
      warn "本地 $ZIP_DEFAULT 无包且远端取包未成功，回退使用脚本同目录的: $ZIP_PATH"
    else
      die "缺少 $ZIP_NAME：请把 zip 传到 $ZIP_DEFAULT（推荐，离线安装），或与本脚本放到同一目录（$SCRIPT_DIR），
或用 --zip <path> 显式指定路径；联网环境应自动从 newapi（$NEWAPI_BASE）取包，
取包失败请检查外网连通性：
  curl \"$NEWAPI_BASE/api/v1/sdk_ver/latest?sdk_type=$SDK_TYPE\""
    fi
  fi
fi

# ========================= 提权（装 crontab / 启动服务需要 root） =========================
if [ "$(id -u)" -ne 0 ]; then
  if [ -n "${TOBOX_SUDO_PASS:-}" ]; then
    log "非 root，用 TOBOX_SUDO_PASS 提权执行"
    printf '%s\n' "$TOBOX_SUDO_PASS" | sudo -S -p '' bash "${BASH_SOURCE[0]}" "${ORIG_ARGS[@]}"
    exit $?
  elif sudo -n true 2>/dev/null; then
    log "非 root，用免密 sudo 提权执行"
    exec sudo -n bash "${BASH_SOURCE[0]}" "${ORIG_ARGS[@]}"
  else
    die "需要 root 权限（装 crontab、写镜像目录、启动服务）。请任选其一：
  sudo bash install-sdk.sh
  TOBOX_SUDO_PASS=<sudo密码> bash install-sdk.sh"
  fi
fi

# ========================= 前置检查 =========================
log "前置检查"

if command -v unzip >/dev/null 2>&1; then
  UNZIP_CMD=(unzip -o -q)
elif command -v python3 >/dev/null 2>&1; then
  # 无 unzip 时用 python3 zipfile 兜底
  UNZIP_CMD=(python3 -c 'import zipfile,sys; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])')
else
  die "系统缺少 unzip 和 python3，无法解压 $ZIP_NAME。请安装其一：apt install unzip / yum install unzip"
fi

ARCH="$(uname -m)"
if [ "$ARCH" != "aarch64" ]; then
  if [ "$SKIP_ARCH_CHECK" -eq 1 ]; then
    warn "跳过架构检查（--skip-arch-check）：本机 $ARCH，tobox_player 为 linux/arm64，-check 自检多半会失败"
  else
    warn "本机架构是 $ARCH（tobox_player 为 linux/arm64）。若确属 x86 等其它部署场景请继续，
否则 -check 自检会失败；纯测试流程可用 --skip-arch-check 跳过。"
  fi
fi

DISK_FREE_MB="$(df -Pm "$(dirname "$INSTALL_DIR")" 2>/dev/null | awk 'NR==2{print $4}' || echo 0)"
if [ "${DISK_FREE_MB:-0}" -lt 1024 ]; then
  warn "磁盘可用空间不足 1GB（当前 ${DISK_FREE_MB:-0}MB），安装可能失败，继续尝试"
else
  ok "磁盘可用 ${DISK_FREE_MB}MB"
fi
ok "前置检查通过（zip: $ZIP_PATH）"

# ========================= [1/8] 目录结构 =========================
step 1 "创建目录结构"
# resource/public 是二进制启动硬依赖（SetServerRoot），必须存在，否则启动即 FATA
mkdir -p "$INSTALL_DIR/data" "$INSTALL_DIR/manifest/config" "$INSTALL_DIR/resource/public"
mkdir -p "$IMAGE_ROOT" || die "无法创建镜像根目录: $IMAGE_ROOT"
ok "安装目录: $INSTALL_DIR"
ok "镜像根目录: $IMAGE_ROOT"

BIN="$INSTALL_DIR/$BIN_NAME"
CFG_DST="$INSTALL_DIR/manifest/config/config.yaml"
RUN_SH_DST="$INSTALL_DIR/run.sh"
HEALTH_URL="http://127.0.0.1:$PORT/health"
TS="$(date +%Y%m%d%H%M%S)"

# ========================= [2/8] 解压 zip → 二进制就位 =========================
step 2 "解压 $ZIP_NAME 并安装二进制"
# 临时目录：优先 /tmp，不存在/不可写则用 $HOME/tmp（部分嵌入式系统没有 /tmp）
TMP_BASE="/tmp"
if ! mkdir -p "$TMP_BASE" 2>/dev/null || [ ! -w "$TMP_BASE" ]; then
  TMP_BASE="${TMPDIR:-$HOME/tmp}"
  mkdir -p "$TMP_BASE" || die "无法创建临时目录（/tmp 不可用且 $TMP_BASE 创建失败）"
fi
EXTRACT_DIR="$(mktemp -d "$TMP_BASE/tobox-install.XXXXXX")"
# 统一用 `-d DIR` 语法：GNU unzip 与 BusyBox unzip 均支持
# （注意 BusyBox unzip 不支持 `unzip f.zip dir` 的位置参数目录写法）
unzip -o -q "$ZIP_PATH" -d "$EXTRACT_DIR" || { rm -rf "$EXTRACT_DIR"; die "解压失败: $ZIP_PATH"; }

if [ ! -f "$EXTRACT_DIR/$BIN_NAME" ]; then
  # 兼容 zip 内多包了一层目录的包：根目录只有条目时直接用
  ONLY_DIR="$(find "$EXTRACT_DIR" -mindepth 1 -maxdepth 1 | head -n1)"
  if [ -d "$ONLY_DIR" ] && [ -f "$ONLY_DIR/$BIN_NAME" ] && [ "$(find "$EXTRACT_DIR" -mindepth 1 -maxdepth 1 | wc -l)" -eq 1 ]; then
    EXTRACT_DIR="$ONLY_DIR"
    warn "zip 内容在子目录 $ONLY_DIR 内（预期根目录直接放二进制），已自动适配"
  else
    rm -rf "$TMP_BASE"/tobox-install.* 2>/dev/null || true
    die "zip 根目录没有 $BIN_NAME 二进制（包格式：zip 根目录 tobox_player + hash + version 三件套）"
  fi
fi
chmod +x "$EXTRACT_DIR/$BIN_NAME"

# ---- 三件套校验：tobox_player + hash + version ----
# hash/version 由 build.sh 生成打入；缺任一视为旧格式/损坏包，拒绝安装。
NEW_HASH_FILE="$EXTRACT_DIR/hash"
NEW_VERSION_FILE="$EXTRACT_DIR/version"
[ -f "$NEW_HASH_FILE" ] || { rm -rf "$EXTRACT_DIR"; die "zip 内缺少 hash 文件（期望三件套：tobox_player + hash + version）。
旧格式包？请用 ./scripts/build.sh 重新打 tebox-sdk.zip 后再装。"; }
[ -f "$NEW_VERSION_FILE" ] || { rm -rf "$EXTRACT_DIR"; die "zip 内缺少 version 文件（期望三件套：tobox_player + hash + version）。
旧格式包？请用 ./scripts/build.sh 重新打 tebox-sdk.zip 后再装。"; }
NEW_HASH="$(tr -d ' 	
' < "$NEW_HASH_FILE")"
NEW_VERSION_PKG="$(tr -d ' 	
' < "$NEW_VERSION_FILE")"
case "$NEW_HASH" in
  [0-9a-fA-F][0-9a-fA-F]*) : ;;   # 只查合法性，完整 64 位由 sha256 核对兜底
  *) rm -rf "$EXTRACT_DIR"; die "zip 内 hash 文件不是合法 sha256 hex: $NEW_HASH" ;;
esac
case "$NEW_VERSION_PKG" in
  ''|*[!0-9]*) rm -rf "$EXTRACT_DIR"; die "zip 内 version 文件不是数字版本号: $NEW_VERSION_PKG" ;;
esac
ok "zip 三件套齐备：tobox_player + hash(${NEW_HASH:0:12}…) + version(v$NEW_VERSION_PKG)"

# ---- 版本防降级（只升不降，对齐 myt-sdk-go v3；--force 可覆盖） ----
SKIP_BINARY_INSTALL=0
CUR_VERSION=""
if [ -f "$BIN" ]; then
  CUR_VERSION="$("$BIN" -check 2>/dev/null | head -n1 | tr -d ' 	
' || true)"
fi
if [ "$SKIP_ARCH_CHECK" -eq 1 ] && [ -z "$CUR_VERSION" ]; then
  # 非本机架构（x86 测试机跑 arm64 包）无法执行 -check：跳过防降级检查
  warn "本机无法执行现有二进制 -check（架构不符），跳过版本防降级检查"
elif [ -n "$CUR_VERSION" ]; then
  case "$CUR_VERSION" in
    ''|*[!0-9]*) CUR_VERSION="" ;;   # 版本输出异常时跳过比较
  esac
fi
if [ -n "$CUR_VERSION" ] && [ "$NEW_VERSION_PKG" -le "$CUR_VERSION" ]; then
  if [ "$FORCE_INSTALL" -eq 1 ]; then
    warn "包版本 v$NEW_VERSION_PKG ≤ 当前版本 v$CUR_VERSION（--force 指定，强制安装）"
  else
    SKIP_BINARY_INSTALL=1
    warn "包版本 v$NEW_VERSION_PKG ≤ 当前已安装版本 v$CUR_VERSION，跳过二进制安装（版本防降级）
   如确需覆盖安装（同版本重装/回退），加 --force 重试"
  fi
fi

if [ "$SKIP_BINARY_INSTALL" -eq 1 ]; then
  rm -rf "$EXTRACT_DIR"
  ok "保留现有二进制: $BIN（v$CUR_VERSION）"
else
  # ---- 二进制完整性：sha256 核对（防 zip 被篡改/下载损坏） ----
  if [ "$SHA256_AVAILABLE" -eq 1 ]; then
    BIN_SHA="$(sha256_of "$EXTRACT_DIR/$BIN_NAME")"
    if [ -z "$BIN_SHA" ]; then
      rm -rf "$EXTRACT_DIR"
      die "无法计算二进制 sha256（sha256sum/shasum 均不可用），无法核对完整性"
    fi
    if [ "$BIN_SHA" != "$NEW_HASH" ]; then
      rm -rf "$EXTRACT_DIR"
      die "二进制 sha256 与 zip 内 hash 不一致（包可能被篡改或损坏）：
  hash 文件: $NEW_HASH
  实际计算: $BIN_SHA"
    fi
    ok "二进制 sha256 校验通过: $BIN_SHA"
  else
    warn "本机没有 sha256sum/shasum，跳过二进制 sha256 核对（不安全，建议安装后补验）"
  fi

  # ---- 原子替换：先写同目录 .new，核对一致后 rename（对齐 myt-sdk-go v3） ----
  BIN_NEW="$BIN.new"
  cp "$EXTRACT_DIR/$BIN_NAME" "$BIN_NEW" || { rm -rf "$EXTRACT_DIR"; die "复制二进制到 $BIN_NEW 失败（磁盘满或权限不足）"; }
  chmod 0755 "$BIN_NEW"
  if [ "$SHA256_AVAILABLE" -eq 1 ]; then
    NEW_COPY_SHA="$(sha256_of "$BIN_NEW")"
    if [ "$NEW_COPY_SHA" != "$NEW_HASH" ]; then
      rm -f "$BIN_NEW"; rm -rf "$EXTRACT_DIR"
      die "落盘副本 $BIN_NEW 的 sha256 与 hash 不一致（写入过程损坏）：
  期望: $NEW_HASH
  实际: $NEW_COPY_SHA"
    fi
  fi
  if [ -f "$BIN" ]; then
    cp -p "$BIN" "$BIN.bak.$TS"
    ok "已备份旧二进制: $BIN.bak.$TS"
  fi
  # 同一文件系统内 rename，原子生效：正在运行的旧进程不受影响，新进程用新文件
  mv -f "$BIN_NEW" "$BIN"
  # zip 内可选的 config.yaml：安装目录还没有配置且脚本旁也没有时，采用 zip 内的
  if [ -f "$EXTRACT_DIR/config.yaml" ] && [ ! -f "$CFG_DST" ] && [ ! -f "$SCRIPT_DIR/config.yaml" ]; then
    mv -f "$EXTRACT_DIR/config.yaml" "$CFG_DST"
    ok "采用 zip 内自带的 config.yaml"
  fi
  rm -rf "$EXTRACT_DIR"
  ok "二进制已原子就位: $BIN（v$NEW_VERSION_PKG）"
fi
# 下载的临时 zip 用完即删
if [ -n "$DOWNLOAD_TMP" ] && [ -f "$DOWNLOAD_TMP" ]; then
  rm -f "$DOWNLOAD_TMP"
  ok "已清理下载临时文件: $DOWNLOAD_TMP"
fi

# ========================= [3/8] 二进制自检 =========================
step 3 "二进制自检（-check）"
if [ "${SKIP_BINARY_INSTALL:-0}" -eq 1 ] && [ -z "$CUR_VERSION" ]; then
  # 防降级跳过安装且现有二进制本机跑不起来（x86 测试机装 arm64 包）：跳过自检
  warn "本机无法执行二进制 -check（架构不符），跳过自检（仅测试场景）"
else
  BIN_VERSION="$("$BIN" -check 2>/dev/null || true)"
  if [ -n "$BIN_VERSION" ]; then
    ok "自检通过：$BIN_VERSION"
  else
    die "二进制自检无输出（架构不符或文件损坏）。排查：
  file $BIN          # 应显示 ARM aarch64
  $BIN -check        # 手动看报错"
  fi
fi

# ========================= [4/8] config.yaml =========================
step 4 "配置 config.yaml"

# 内置默认配置模板（自包含，不依赖项目目录）。占位符：__PORT__ / __TEBOX_ADDR__ / __IMAGE_ROOT__
write_default_config() {
  cat <<'CFGEOF'
# tobox_player 默认配置（由 install-sdk.sh 生成）
server:
  address: ":__PORT__"        # SDK HTTP 监听端口
  dumpRouterMap: false
  accessLogEnabled: false
  openapiPath: "/api.json"
  swaggerPath: "/swagger"
  # 请求体上限：GoFrame 默认 8MB 会拦掉数 GB 的镜像上传；单 system.img 可达
  # 9GB（coloros 大包解压），设为 16GiB 留足余量
  clientMaxBodySize: "16G"
  # 大文件上传是长连接慢速读，放宽到 1 小时
  readTimeout: "1h"

logger:
  level: "all"
  stdout: true
  writerColorEnable: true

# tebox-api（gRPC）对接：SDK 固定连接该地址
tebox:
  address: "__TEBOX_ADDR__"
  auth:                       # Basic Auth，默认关闭
    enabled: false
    username: "admin"
    password: "admin"

# 镜像管理根目录（须与 tebox-api 容器挂载一致）
image:
  root: "__IMAGE_ROOT__"

# 自更新：开启后可用 deploy.sh 走 /self/update 远程热更新。
# self/* 接口独立 Basic Auth（即使 tebox.auth 关闭也强制要求）。
# ⚠ 默认凭据 admin/myt，上线前务必修改！
selfupdate:
  enabled: true
  username: "admin"
  password: "myt"

# 事件源（/ws/events 事件推送的实例状态轮询周期）
eventsource:
  poll_interval: "3s"

# 截图（GET /android/:id/screenshot），默认开启；显式置 false 才会关闭
screenshot:
  enabled: true
  timeout: "5s"
  container: "tebox-minimal"
  media_port: 9901
  adb_server_port: "5039"

# 终端（GET /android/:id/terminal，WebSocket），默认开启；显式置 false 才会关闭
terminal:
  enabled: true
  container: "tebox-minimal"
  adb_server_port: "15037"
  connect_timeout: "10s"
CFGEOF
}

# sed 替换用的转义（值里可能含 | 与 &）
sed_escape() { printf '%s' "$1" | sed -e 's/[&|]/\\&/g'; }

if [ -f "$CFG_DST" ]; then
  ok "config.yaml 已存在，保留原配置: $CFG_DST"
elif [ -f "$SCRIPT_DIR/config.yaml" ]; then
  install -m 0644 "$SCRIPT_DIR/config.yaml" "$CFG_DST"
  ok "采用脚本旁的 config.yaml: $CFG_DST"
else
  write_default_config | sed -e "s|__PORT__|$(sed_escape "$PORT")|g" \
                             -e "s|__TEBOX_ADDR__|$(sed_escape "$TEBOX_ADDR")|g" \
                             -e "s|__IMAGE_ROOT__|$(sed_escape "$IMAGE_ROOT")|g" > "$CFG_DST"
  chmod 0644 "$CFG_DST"
  ok "已写入内置默认配置: $CFG_DST"
fi

# 从配置里读 selfupdate 凭据（/self/version 验证用；读不到用默认值）
SELFUSER="$(awk '/^selfupdate:/{f=1;next} /^[a-zA-Z]/{f=0} f&&/^[[:space:]]*username:/{print $2; exit}' "$CFG_DST" | tr -d '"')"
SELFPASS="$(awk '/^selfupdate:/{f=1;next} /^[a-zA-Z]/{f=0} f&&/^[[:space:]]*password:/{print $2; exit}' "$CFG_DST" | tr -d '"')"
SELFUSER="${SELFUSER:-admin}"
SELFPASS="${SELFPASS:-myt}"

# ========================= [5/8] 安装 run.sh 守护脚本 =========================
step 5 "安装守护脚本 run.sh"

# 内置 run.sh（与 deploy/run.sh 同逻辑：循环拉起，自更新/回滚依赖）。
# 占位符：__INSTALL_DIR__（安装目录；TMPDIR 默认 $HOME/tmp，脚本内自动创建）
write_run_sh() {
  cat <<'RUNEOF'
#!/bin/bash
# tobox_player 守护脚本（由 install-sdk.sh 安装）
#
# 用途：tobox_player 的「自更新」流程在替换二进制后 os.Exit(0)，由本脚本
# 把进程重新拉起来加载新版本。因此本脚本必须常驻：
#   while true; do ./tobox_player; sleep 1; done
#
#   POST /self/update   → 备份旧二进制、原子替换 → 2 秒后进程退出
#   POST /self/rollback → 回滚到最近一次备份 → 2 秒后进程退出
#   run.sh              → 发现进程退出 → sleep 1 → 拉起（此时已是新二进制）
#
# 环境变量（均可选）：
#   TOBOX_INSTALL_DIR  安装目录，默认内置
#   TOBOX_BIN          二进制文件名，默认 tobox_player
#   TMPDIR             子进程临时目录（远端 /tmp 受限时必须指向可写目录）
#   TOBOX_SUDO_PASS    非 root 运行且 sudo 需要密码时使用
#
# 日志：
#   <安装目录>/sdk.log     tobox_player 自身的 stdout/stderr（追加）
#   <安装目录>/runsh.log   本脚本的事件（启动/退出码/重启），追加
#
# 手动用法：
#   <安装目录>/run.sh                 # 前台常驻（Ctrl-C 结束守护）
#   nohup ./run.sh >/dev/null 2>&1 &  # 后台常驻
#
# 退出：本脚本只在安装目录/二进制缺失等致命错误时退出，正常情况永不返回。

set -uo pipefail   # 不加 -e：守护循环必须能容忍单次启动失败

INSTALL_DIR="${TOBOX_INSTALL_DIR:-__INSTALL_DIR__}"
BIN_NAME="${TOBOX_BIN:-tobox_player}"
export TMPDIR="${TMPDIR:-$HOME/tmp}"

SELF_LOG="$INSTALL_DIR/runsh.log"
SDK_LOG="$INSTALL_DIR/sdk.log"

log() { echo "[run.sh] $(date '+%F %T') $*" >>"$SELF_LOG" 2>/dev/null || true; }

cd "$INSTALL_DIR" 2>/dev/null || { echo "[run.sh] 安装目录不存在: $INSTALL_DIR" >&2; exit 1; }
if [ ! -x "./$BIN_NAME" ]; then
  echo "[run.sh] 找不到可执行文件: $INSTALL_DIR/$BIN_NAME" >&2
  exit 1
fi

# TMPDIR 必须存在且可写：远端 /tmp 受限，不设置会导致启动失败。
if ! mkdir -p "$TMPDIR" 2>/dev/null || [ ! -w "$TMPDIR" ]; then
  echo "[run.sh] TMPDIR 不可写: $TMPDIR（用 TMPDIR=\$HOME/tmp 指定一个可写目录）" >&2
  exit 1
fi

# 写镜像目录需要 root：非 root 时走 sudo。
SUDO_CMD=""
if [ "$(id -u)" -ne 0 ]; then
  if ! command -v sudo >/dev/null 2>&1; then
    echo "[run.sh] 当前用户非 root 且没有 sudo，无法启动（镜像目录需要 root 权限）" >&2
    exit 1
  fi
  if sudo -n true 2>/dev/null; then
    SUDO_CMD="sudo -n"
  elif [ -n "${TOBOX_SUDO_PASS:-}" ]; then
    SUDO_CMD="sudo -S"     # 密码由下面的 printf 从 stdin 喂入
  else
    echo "[run.sh] 非 root 且 sudo 需要密码：请用 root 运行，或设置 TOBOX_SUDO_PASS" >&2
    exit 1
  fi
fi

log "start: dir=$INSTALL_DIR bin=$BIN_NAME TMPDIR=$TMPDIR user=$(id -un) sudo=${SUDO_CMD:-none}"

# 单次运行：返回 tobox_player 的退出码。
run_once() {
  if [ -z "$SUDO_CMD" ]; then
    env TMPDIR="$TMPDIR" "./$BIN_NAME" >>"$SDK_LOG" 2>&1
    return $?
  fi
  if [ "$SUDO_CMD" = "sudo -S" ]; then
    printf '%s\n' "$TOBOX_SUDO_PASS" | sudo -S env TMPDIR="$TMPDIR" "./$BIN_NAME" >>"$SDK_LOG" 2>&1
    return $?
  fi
  sudo -n env TMPDIR="$TMPDIR" "./$BIN_NAME" >>"$SDK_LOG" 2>&1
  return $?
}

# 主循环：进程退出后 1 秒重启（自更新/回滚依赖此行为）。
while true; do
  run_once
  rc=$?
  log "tobox_player exited (rc=$rc), restarting in 1s"
  sleep 1
done
RUNEOF
}

# 先写 .new 再原子 rename：正在运行的旧 run.sh 保持自己的 inode，不会被边读边写破坏。
write_run_sh | sed -e "s|__INSTALL_DIR__|$(sed_escape "$INSTALL_DIR")|g" > "$RUN_SH_DST.new"
chmod 0755 "$RUN_SH_DST.new"
if [ -f "$RUN_SH_DST" ] && cmp -s "$RUN_SH_DST" "$RUN_SH_DST.new"; then
  rm -f "$RUN_SH_DST.new"
  ok "run.sh 已是最新（内容一致，跳过）"
else
  if [ -f "$RUN_SH_DST" ]; then
    cp -p "$RUN_SH_DST" "$RUN_SH_DST.bak.$TS"
    ok "已备份旧 run.sh: $RUN_SH_DST.bak.$TS"
  fi
  mv -f "$RUN_SH_DST.new" "$RUN_SH_DST"
  ok "run.sh 已安装: $RUN_SH_DST"
fi
# 安装目录属主不是 root 时保持归属一致，便于用户维护
DIR_OWNER="$(stat -c '%U:%G' "$INSTALL_DIR" 2>/dev/null || true)"
if [ -n "$DIR_OWNER" ] && [ "$DIR_OWNER" != "root:root" ]; then
  chown "$DIR_OWNER" "$RUN_SH_DST" 2>/dev/null || true
fi

# ========================= [6/8] crontab @reboot 自启（幂等） =========================
step 6 "配置 crontab @reboot 自启"

# TMPDIR：crond 启动时 HOME=/root（root crontab），不能依赖 $HOME；
# 取安装目录的上一级 + /tmp（默认安装目录 /home/user/tobox_player 时即
# /home/user/tmp，与项目既有约定一致），不可写再落到 /root/tmp。
TMPDIR_INSTALL="$(dirname "$INSTALL_DIR")/tmp"
if ! mkdir -p "$TMPDIR_INSTALL" 2>/dev/null || [ ! -w "$TMPDIR_INSTALL" ]; then
  TMPDIR_INSTALL="/root/tmp"
  mkdir -p "$TMPDIR_INSTALL" 2>/dev/null || true
fi
CRON_LINE="@reboot sleep 10 && env TMPDIR=$TMPDIR_INSTALL $RUN_SH_DST >> $INSTALL_DIR/runsh.log 2>&1"

CUR_CRON="$(crontab -l 2>/dev/null || true)"
if printf '%s\n' "$CUR_CRON" | grep -Fq "$RUN_SH_DST"; then
  EXIST_LINE="$(printf '%s\n' "$CUR_CRON" | grep -F "$RUN_SH_DST" | head -n1)"
  ok "crontab @reboot 已存在（跳过，不重复添加）"
  printf '   %s\n' "$EXIST_LINE"
  if [ "$EXIST_LINE" != "$CRON_LINE" ]; then
    warn "已有 crontab 条目与本次生成的不一致（保留原条目）：
   现有: $EXIST_LINE
   本次: $CRON_LINE
如需统一，手工 crontab -e 修改。"
  fi
else
  {
    printf '%s\n' "$CUR_CRON" | sed '/^$/d'
    printf '%s\n' "$CRON_LINE"
  } | crontab -
  ok "crontab 已更新（用户: $(id -un)）"
  printf '   %s\n' "$CRON_LINE"
fi

# ========================= [7/8] 启动服务 =========================
step 7 "启动服务"

health_ok() {
  if command -v curl >/dev/null 2>&1; then
    curl -sS --max-time 5 "$HEALTH_URL" 2>/dev/null | grep -q '"status":"ok"'
  else
    wget -qO- --timeout=5 "$HEALTH_URL" 2>/dev/null | grep -q '"status":"ok"'
  fi
}

# 进程模式锚定结尾，避免匹配到 run.sh 命令行里出现的路径字符串
service_running() { pgrep -f "(^|/)${BIN_NAME}\$" >/dev/null 2>&1; }
daemon_running()  { pgrep -f "${RUN_SH_DST}\$" >/dev/null 2>&1; }

if service_running && health_ok; then
  ok "服务已在运行且健康（pid: $(pgrep -f "(^|/)${BIN_NAME}\$" | tr '\n' ' ')），跳过启动"
elif daemon_running; then
  # 守护脚本在跑（可能在重启窗口），不能再起第二个（会争抢端口），等它拉起
  log "守护脚本已在运行（pid: $(pgrep -f "${RUN_SH_DST}\$" | tr '\n' ' ')），等待其拉起服务"
else
  # 清掉同路径的残留进程（可能刚被手工停掉但进程还挂着）
  if service_running; then
    log "发现不健康的旧进程，先停止: $(pgrep -f "(^|/)${BIN_NAME}\$" | tr '\n' ' ')"
    pkill -f "(^|/)${BIN_NAME}\$" 2>/dev/null || true
    sleep 1
  fi
  if [ -f "$RUN_SH_DST" ] && [ ! -x "$RUN_SH_DST" ]; then chmod +x "$RUN_SH_DST"; fi
  log "启动守护脚本（TMPDIR=$TMPDIR_INSTALL）"
  cd "$INSTALL_DIR"
  if command -v setsid >/dev/null 2>&1; then
    setsid env TMPDIR="$TMPDIR_INSTALL" "$RUN_SH_DST" >>"$INSTALL_DIR/runsh.log" 2>&1 &
  else
    nohup env TMPDIR="$TMPDIR_INSTALL" "$RUN_SH_DST" >>"$INSTALL_DIR/runsh.log" 2>&1 &
  fi
  log "守护进程已拉起（pid $!），等待 /health 就绪"
fi

# ========================= [8/8] 健康验证 =========================
step 8 "健康验证（最多等 30s）"
READY_AT=""
i=1
while [ "$i" -le 30 ]; do
  if health_ok; then
    READY_AT="$i"
    break
  fi
  sleep 1
  i=$((i + 1))
done

if [ -z "$READY_AT" ]; then
  die "服务 ${READY_AT:-30} 秒内未就绪，查看日志排查：
  tail -n 50 $INSTALL_DIR/sdk.log
  tail -n 20 $INSTALL_DIR/runsh.log"
fi
ok "/health 正常（等待 ${READY_AT}s）"

if command -v curl >/dev/null 2>&1; then
  SDK_VERSION="$(curl -sS --max-time 5 -u "$SELFUSER:$SELFPASS" "http://127.0.0.1:$PORT/self/version" 2>/dev/null || true)"
else
  SDK_VERSION="$(wget -qO- --timeout=5 --auth-no-challenge "--user=$SELFUSER" "--password=$SELFPASS" "http://127.0.0.1:$PORT/self/version" 2>/dev/null || true)"
fi
if [ -n "$SDK_VERSION" ]; then
  ok "SDK 版本: $SDK_VERSION"
else
  warn "获取 /self/version 失败（selfupdate 凭据可能不是 $SELFUSER/****，不影响服务运行）"
fi

# ========================= 安装摘要 =========================
printf '\n'
ok "安装完成"
printf '   安装目录   : %s\n' "$INSTALL_DIR"
printf '   二进制     : %s（%s）\n' "$BIN" "${BIN_VERSION:-版本未知}"
printf '   配置文件   : %s\n' "$CFG_DST"
printf '   监听端口   : %s\n' "$PORT"
printf '   tebox-api  : %s\n' "$TEBOX_ADDR"
printf '   镜像根目录 : %s\n' "$IMAGE_ROOT"
printf '   守护脚本   : %s（循环拉起，支撑 /self/update 热更新）\n' "$RUN_SH_DST"
printf '   日志       : %s/sdk.log、%s/runsh.log\n' "$INSTALL_DIR" "$INSTALL_DIR"
printf '   开机自启   : crontab @reboot（%s）\n' "$(id -un)"

cat <<'SUMEOF'

后续操作:
  远程热更新（开发机执行，二进制经 /self/update 上传）:
    ./scripts/deploy.sh --host <本机IP>          # 默认凭据 admin/myt
  手动更新: 替换二进制文件后，服务由 run.sh 在 1 秒内自动拉起新版本
  回滚: curl -u admin:myt -X POST http://127.0.0.1:<端口>/self/rollback
  卸载:
    crontab -l | grep -v <安装目录>/run.sh | crontab -   # 移除自启
    pkill -f '(^|/)tobox_player$'; pkill -f '<安装目录>/run.sh$'  # 停服务
    rm -rf <安装目录>                                    # 删除程序与数据
SUMEOF
