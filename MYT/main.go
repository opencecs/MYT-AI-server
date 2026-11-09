package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/moyunteng/myt/internal/config"
	"github.com/moyunteng/myt/internal/updater"
	"github.com/moyunteng/myt/internal/util"
)

func main() {
	// 禁止所有 HTTP 请求走系统代理（必须在任何 HTTP 请求之前执行）
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		t.Proxy = nil
	}

	// 调试模式
	envDebug := os.Getenv("APP_DEBUG")
	enableDebug := strings.ToLower(envDebug) == "true" || envDebug == "1"
	for _, arg := range os.Args[1:] {
		if arg == "--debug" || arg == "-debug" {
			enableDebug = true
		}
	}
	if enableDebug {
		log.Printf("[Main] 调试模式已启用")
	}
	util.AttachConsole()

	// --update 模式：提权更新进程，不启动 Wails
	if isUpdateMode() {
		zipPath, pid := parseUpdateFlags()
		if zipPath != "" {
			updater.RunUpdateMode(zipPath, pid)
		}
		return
	}

	// 提取嵌入的 go_legacy.exe 到同目录
	// 提取失败时 findPlayer 会静默回退到旧版本 exe，必须显式告警
	if p := ExtractGoLegacy(); p == "" {
		log.Printf("[go_legacy] 警告: 播放器提取失败，投屏可能运行旧版本或不可用")
	}

	rootDir := getRootDir()

	// 断电恢复：检查未完成的更新
	updater.CheckIncompleteUpdate()

	// --restart-from-update 模式：清理上次更新遗留的 .old 文件
	if isRestartFromUpdate() {
		go updater.CleanupOldFiles()
		go updater.CleanupElevatedUpdate()
		cleanInternalArgs()
	}

	// 初始化配置
	cm := config.NewManager(rootDir)
	if err := cm.InitDirs(); err != nil {
		log.Fatalf("初始化目录失败: %v", err)
	}
	if err := cm.Load(); err != nil {
		log.Printf("加载配置失败: %v", err)
	}
	if err := cm.LoadAll(); err != nil {
		log.Printf("加载数据体系失败: %v", err)
	}
	cm.StartFlushLoop()
	defer cm.StopFlushLoop()

	// MCP 调试服务开关：关闭时设环境变量，vendor 补丁的 startMCPServer 门控读取后跳过启动
	// （默认 true 不用设；生产构建未编译 mcp tag，本环境变量无效果）
	if !cm.GetMCPEnabled() {
		os.Setenv("WAILS_MCP_ENABLED", "0")
	}

	initLogger(cm.LogsDir())

	// 组装服务
	r := setupServices(cm, rootDir, enableDebug)

	// 创建 Wails 应用（MCP 开启时给 WebView2 加随机 CDP 调试端口，供 UI 级工具）
	app := newWailsApp(r, rootDir, enableDebug, cm.GetMCPEnabled())
	r.setApp(app)

	// 启动后台服务
	r.startBackgroundServices()
	defer r.stopBackgroundServices()

	// 自建 MCP server：配置开启则启动（热启停经 MCPService.SetMCPEnabled）
	if err := r.mcpSvc.StartIfEnabled(); err != nil {
		log.Printf("[mcp] MCP server 启动失败: %v", err)
	}

	// 异步自动登录
	go r.paymentSvc.AutoLogin()

	// 创建主窗口
	mainWindow := newMainWindow(app, enableDebug)
	registerFileDropHandler(mainWindow, app, r.fileSvc)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func getRootDir() string {
	// macOS: 使用 ~/Library/Application Support/ 避免只读位置写入失败
	if dir := getDarwinDataDir(); dir != "" {
		return dir
	}
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	dir := filepath.Dir(exe)
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return dir
	}
	return resolved
}

func initLogger(logsDir string) {
	logPath := filepath.Join(logsDir, "myt.log")
	// 按大小轮转（单文件 100MB、保留 3 份，总占用上限约 400MB）：原先直接追加写且无上限，
	// dev 模式 Debugf 全开（每条 API 请求 + 每秒截图轮询都记），实测 1.5 小时就涨到 3.1GB。
	w := util.NewRotatingWriter(logPath, 100<<20, 3)
	// 先探测可写性：打不开就保持默认 stderr 输出，避免日志静默消失
	if err := w.Open(); err != nil {
		return
	}
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	// 开发构建：文件 + stdout 双写，让 wails3 dev 终端实时显示全部 Go 日志
	// （生产构建 DebugEnabled=false，且无控制台，保持纯文件输出不变）。
	if util.DebugEnabled {
		log.SetOutput(io.MultiWriter(w, os.Stdout))
	} else {
		log.SetOutput(w)
	}
}
