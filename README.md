<div align="center">

<img src="MYT/build/appicon.png" width="160" alt="MoYunTeng AI Logo"/>

# MoYunTeng AI 客户端

**云手机 / 云真机 桌面客户端 — 设备管理 · 实时投屏 · AI 助手 · RPA**

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Wails v3](https://img.shields.io/badge/Wails-v3%20alpha-E34F26?logo=wails&logoColor=white)](https://v3.wails.io)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=white)](https://react.dev)

[📖 项目简介](#-项目简介) · [✨ 功能特性](#-功能特性) · [🛠️ 环境要求](#%EF%B8%8F-环境要求) · [🚀 快速开始](#-快速开始) · [📦 编译构建](#-编译构建) · [📁 项目结构](#-项目结构) · [📖 使用指南](#-使用指南-1) · [⚙️ 配置说明](#%EF%B8%8F-配置说明) · [🏗️ 技术栈](#%EF%B8%8F-技术栈) · [🤝 贡献指南](#-贡献指南) · [❓ FAQ](#-faq)

</div>

---

## 📖 项目简介

MoYunTeng AI 客户端（魔云腾-AI）是基于 **Wails v3（Go + React）** 的云手机 / 云真机桌面客户端，对硬件设备与云机实例进行集中管理：

- **设备管理**：局域网 UDP 广播自动发现（local 模式）或经线上服务拉取（public 模式），兼容 V2 / V4 多代主机协议
- **实时投屏**：自研 RTSP/WebRTC 播放器 `go_legacy`，支持单台与矩阵批量投屏
- **AI 助手 / RPA**：内置 AI Agent 任务编排、技能管理与 MCP 工具
- **双形态运行**：GUI 桌面应用（`production` 构建）与无 GUI HTTP Server 模式（`server` 构建 tag + Docker）

## ✨ 功能特性

### 🖥️ 设备发现与管理
- UDP 广播自动发现局域网设备（local），线上网关拉取设备列表（public）；V2 主机与 V4（tebox）设备各成一套连接、扫描与心跳逻辑
- 设备状态上报与自动刷新，支持手动添加 / 扫描添加设备

### 📱 云机实例管理
- 容器云机与模拟器云机两套独立客户端（`internal/mytos/container` / `emulator`）
- 镜像管理（上传 / 推送 / 下载进度 / ROM），批量创建实例、批量文件分发、批量截图

### 🎬 实时投屏
- 自研播放器 `go_legacy`：RTSP（gortsplib）/ WebRTC（pion）拉流 + FFmpeg C 解码（`ffdecode`）
- 单台投屏、矩阵投屏（多台批量编排）、键鼠回控、剪贴板同步、黑帧检测、码率显示、截图

### 🌐 流媒体推送
- 内置流媒体服务器生命周期管理（LAL / SRS，RTMP）
- 摄像头 + 麦克风推流到云机（camdemo：精简 FFmpeg 推流器 `windows_pusher`）、虚拟摄像头 / 屏幕 WGC 采集推流

### 🧩 AI 助手与自动化
- AI Agent：任务编排、执行历史、技能（Skill）管理、设备工具面板；RPA 自动化（`internal/rpa`）
- 内置 MCP Server：浏览器 / CDP / UI 映像 / 截图等工具集（`internal/mcpserver`）

### 🛠️ 系统与运维
- VPC 网络（节点 / 分组 / 网卡 / 路由 / 规则）、iSCSI target 与 SSH 远程磁盘管理
- 远程终端（xterm）、文件管理、自动更新（SHA256 校验）、i18n 中英双语

## 🛠️ 环境要求

| 依赖 | 最低版本 | 说明 |
| --- | --- | --- |
| [Go](https://go.dev/dl/) | 1.26+ | 主程序与播放器子模块均为 go 1.26.5 |
| [Node.js](https://nodejs.org/) + npm | 20+ | 前端构建（Vite 5 / React 19） |
| [Wails CLI v3](https://v3.wails.io) | v3 alpha | `go install github.com/wailsapp/wails/v3/cmd/wails3@latest` |
| Git / Git Bash | 2.x+ | Windows 下运行 `scripts/*.sh` 需 Git Bash |
| [NSIS](https://nsis.sourceforge.io/) | 3.x | Windows 打包安装包（`makensis`） |
| Xcode CLT + FFmpeg | — | 仅 macOS：`xcode-select --install`、`brew install ffmpeg`（`ffdecode` 构建依赖） |

## 🚀 快速开始

```bash
# 1. 克隆仓库
git clone https://github.com/opencecs/MYT-AI-server.git
cd MYT-AI-server

# 2. 开发模式运行（自动完成：构建播放器 → 生成绑定 → 前端依赖 → wails3 dev）
bash scripts/dev.sh

# Windows 用户加 windows 参数（Git Bash 中）：
bash scripts/dev.sh windows
```

开发模式会自动完成全部前置步骤（构建 `go_legacy` 到 `MYT/embedded/`、`wails3 generate bindings`、安装前端依赖），无需手动准备。前端 Vite 开发服务器端口为 `9245`。

## 📦 编译构建

### 一键打包 / 构建

```bash
bash scripts/package.sh   # Windows：NSIS 安装包 + 更新 zip；macOS：.app + DMG + 更新 zip（输出到 MYT/dist/）
bash scripts/build.sh     # 仅构建：Windows 产出 bin/myt.exe；macOS 产出 bin/myt.app
```

### 手动构建（理解构建链路）

构建产物不入库（`MYT/embedded/`、`MYT/frontend/dist`、`MYT/frontend/bindings` 均在 `.gitignore` 中），手动构建必须按以下顺序：

```bash
cd MYT

# 1. 构建投屏播放器子模块，产物复制到 embedded/（主程序 embed 依赖，缺了编译直接报错）
#    Windows：
(cd ../myt-client-player && go build -tags production -trimpath -ldflags="-w -s -H windowsgui" -o go_legacy.exe .)
mkdir -p embedded && cp ../myt-client-player/go_legacy.exe embedded/
#    macOS（编译 ffdecode/metalrender dylib + go_legacy，需 brew install ffmpeg）：
bash ../myt-client-player/scripts/build_macos.sh

# 2. 生成 Wails 绑定（产出 frontend/bindings/）
wails3 generate bindings

# 3. 构建前端（产出 frontend/dist/）
cd frontend && npm install && npm run build && cd ..

# 4. 编译主程序（正式构建：-tags production，注入版本号）
wails3 task windows:build    # Windows
wails3 task darwin:build     # macOS
```

### 构建参数

| 参数 | 说明 |
| --- | --- |
| `production`（构建 tag） | 正式构建默认追加（见 `build/windows/Taskfile.yml` / `build/darwin/Taskfile.yml`） |
| `server`（构建 tag） | 无 GUI HTTP Server 模式：`wails3 task build:server`（Docker：`build:docker` / `run:docker`，默认映射 8080 端口） |
| `DEV=true` / `EXTRA_TAGS` / `ARCH` | 开发构建 / 追加 tag / 目标架构（amd64、arm64） |

## 📁 项目结构

```
MYT-AI-server/
├── MYT/                        # 主程序（Wails v3 桌面应用）
│   ├── main.go                 # 入口：调试开关（--debug / APP_DEBUG）
│   ├── app_service.go          # 应用主服务（版本注入、更新检查）
│   ├── discover_service*.go    # 设备发现（UDP 广播 / 云端拉取）
│   ├── screencast_service*.go  # 投屏编排（单台 / 矩阵 / 批量）
│   ├── v2_service*.go / v4_service*.go   # V2 / V4 (tebox) 主机服务
│   ├── rtmp_server.go          # 流媒体服务器生命周期（LAL/SRS）
│   ├── ai_service.go / mcp_service.go    # AI 助手 / MCP 服务
│   ├── embedded/               # go_legacy 运行产物（构建生成，不入库）
│   ├── frontend/               # React 19 + TS + Vite 前端
│   │   ├── src/pages/          # host / agent / vpc / mirror / stream / settings / backup / diskless / groupcontrol / maintenance ...
│   │   ├── src/i18n/           # i18next 中英语言包（zh-CN / en-US）
│   │   └── bindings/           # Wails 生成的 TS 绑定（自动生成）
│   ├── internal/               # 业务内部包
│   │   ├── device/discover/    # UDP 设备发现协议
│   │   ├── mytos/              # 云机客户端（container / emulator）
│   │   ├── v2host/ v4host/     # V2 / V4 主机协议实现
│   │   └── ...                 # iscsitarget / mcpserver / updater / sdkapi / device / mirror / screencast / util ...
│   ├── camdemo/                # 摄像头推流（精简 FFmpeg / windows_pusher）
│   ├── build/                  # Wails 构建资产（Taskfile / config.yml / 图标 / NSIS）
│   └── vendor/                 # Go vendor 依赖
├── myt-client-player/          # 投屏播放器（独立 Go module：go_legacy）
│   ├── main.go                 # 播放器入口
│   ├── rtsp.go / teboxcast/    # RTSP / tebox 拉流
│   ├── ffdecode/               # FFmpeg C 解码（ffdecode.c / d3d11 / metal）
│   ├── camera_push*.go         # 摄像头 / 屏幕推流编码
│   ├── ui/                     # 内嵌侧边面板（JS/CSS/HTML）
│   └── scripts/                # 平台构建脚本（build_macos.sh 等）
└── scripts/                    # 顶层转发脚本（转发到 MYT/scripts/）
    ├── dev.sh                  # 开发模式
    ├── build.sh                # 构建
    ├── package.sh              # 打包（NSIS / DMG / 更新 zip）
    └── build_ffmpeg.sh         # 构建精简 FFmpeg 推流器
```

## 📖 使用指南

### 连接设备
1. 启动客户端，进入 **设备管理** 页面
2. **local 模式**：同一局域网内的主机会通过 UDP 广播自动发现并列出
3. **public 模式**：登录线上账号后，从网关拉取已绑定的设备列表
4. 手动添加：点击「添加设备」→ 输入 IP 或扫描发现（V2 / V4 设备各走对应扫描流程）

### 实时投屏
1. 在设备列表选中目标设备，点击 **投屏**
2. 单台投屏窗口内可直接键鼠回控、剪贴板同步、截图
3. 多台设备使用 **矩阵投屏**：批量编排窗口排布，统一注入操作

### 云机管理
1. 进入 **镜像管理** 页面上传 / 推送镜像与 ROM
2. 对容器或模拟器实例执行创建、重启、批量操作
3. 在设备详情中打开 xterm 远程终端，进行文件上传 / 下载

### AI 助手
1. 进入 **AI 助手** 页面，下发自然语言任务
2. 在 **技能管理** 中维护可复用的技能（Skill）
3. 通过 MCP 工具面板查看 Agent 可调用的设备工具与执行历史

## ⚙️ 配置说明

| 配置 | 路径 | 说明 |
| --- | --- | --- |
| 构建资产配置 | `MYT/build/config.yml` | 产品名（魔云腾-AI）、版本号（当前 4.1.2）、companyName 等 |
| Windows 版本号 | `MYT/build/windows/Taskfile.yml` | `APP_VERSION`，与 config.yml 同步；`build.sh` 正式构建时自动递增 patch 位 |
| 部署模式 | 运行时配置 `deploy_mode` | `local`（局域网直连）/ `public`（线上服务转发）/ `v2` / `v4`，各模式设备列表独立存储 |
| 调试模式 | `--debug` 参数或 `APP_DEBUG=true` | 开启 WebView2 CDP 远程调试端口（127.0.0.1:9333），`scripts/dev.sh` 已默认开启 |
| Vite 端口 | 环境变量 `WAILS_VITE_PORT` | 前端开发服务器端口，默认 `9245` |

## 🏗️ 技术栈

- **后端**：Go 1.26 + [Wails v3 alpha](https://v3.wails.io)；[gortsplib](https://github.com/bluenviron/gortsplib) / [pion](https://github.com/pion)（RTSP / WebRTC）；[LAL](https://github.com/q191201771/lal)（流媒体服务器）；MCP Go SDK
- **前端**：React 19 + TypeScript 5 + Vite 5；Tailwind CSS 4 + GSAP + Three.js；xterm（远程终端）、i18next（国际化）
- **投屏播放器**：独立 Go module（`go_legacy`），产物经 `embed` 注入主程序；FFmpeg C 解码层（`ffdecode/`）：Windows D3D11 / macOS Metal 渲染

### 通信协议

| 协议 | 用途 |
| --- | --- |
| UDP 广播 | 局域网设备发现（local 模式） |
| WebSocket | 设备状态上报、终端、V4 适配 |
| RTSP | 投屏视频拉流（go_legacy 播放器） |
| WebRTC (pion) | Web 端投屏拉流 |
| RTMP | 流媒体推送（LAL/SRS、摄像头推流） |
| HTTP/SFTP | 文件管理、镜像分发、云机控制 API |

## 🤝 贡献指南

1. **Fork** 本仓库并创建特性分支：`git checkout -b feature/your-feature`
2. 提交前确保构建通过：`bash scripts/build.sh`
3. 提交信息遵循 **Conventional Commits** 规范（`feat:` / `fix:` / `docs:` / `refactor:` / `chore:`）
4. 发起 Pull Request，描述清楚改动动机与验证方式

> 注意：`MYT/embedded/`、`MYT/frontend/dist`、`MYT/frontend/bindings` 为构建产物，不要提交到仓库。

## ❓ FAQ

<details>
<summary><b>编译报错 <code>embed: no matching files found</code>？</b></summary>

主程序通过 `//go:embed` 加载 `MYT/embedded/` 下的播放器产物，该目录不入库。先运行 `bash scripts/build.sh`（或 `scripts/dev.sh`）自动构建 `go_legacy` 并复制到 `MYT/embedded/`，再编译主程序。
</details>

<details>
<summary><b>macOS 编译失败，提示缺少 FFmpeg？</b></summary>

播放器 `ffdecode` 的 C 解码层依赖 FFmpeg 头文件与动态库：`brew install ffmpeg`，同时确保已安装 Xcode Command Line Tools（`xcode-select --install`）。
</details>

<details>
<summary><b>前端 <code>dist</code> 或 <code>bindings</code> 目录缺失？</b></summary>

两者均为构建产物：`wails3 generate bindings` 生成 `frontend/bindings/`，`npm run build`（或 Taskfile 的 `build:frontend` 任务）生成 `frontend/dist/`。`wails3 task windows:build` / `darwin:build` 会自动按依赖顺序执行，无需手动操作。
</details>

<details>
<summary><b>Windows 打包提示找不到 <code>makensis</code>？</b></summary>

NSIS 安装包需要 [NSIS](https://nsis.sourceforge.io/)。安装后 `package.sh` 会自动在默认路径（`C:\Program Files (x86)\NSIS`）中查找 `makensis`。
</details>

## 📄 许可证

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

本项目基于 [Apache License 2.0](LICENSE)（SPDX: `Apache-2.0`）开源发布。使用、修改或分发本项目代码时，请遵循该许可证的条款，并保留原始的版权与许可声明。
