//go:build production

package application

func newApplication(options Options) *App {
	result := &App{
		isDebugMode: true, // 保持 debug 模式：关闭会导致 WebView2 渲染异常
		options:     options,
	}
	result.init()
	return result
}

func (a *App) logStartup() {}

func (a *App) preRun() error { return nil }

func (a *App) postQuit() error { return nil }
