package v4host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- V4 镜像管理（8088 REST，见 tobox_player/api.json 的「镜像管理」分组）---
//
//	GET    /image/list              → {root, variants:[{variant,files:[{name,size,ready}],ready}], total}
//	POST   /image/pull              → body {variant,url,filename} → {task_id}
//	GET    /image/status/{task_id}  → {task_id,variant,filename,status,downloaded,total,error}
//	POST   /image/upload/{variant}  → multipart（字段 file，可选 filename）→ {variant,filename,size,ready}
//	DELETE /image/{variant}         → {}
//
// 一个完整变体需要三件套 system.img / vendor.img / initramfs.img（upload 支持
// 一次上传 zip 包，服务端解压后三件齐全才算 ready）。pull 是**按单文件**下载的：
// 一个变体要三次 pull（每次一个 filename），每次返回独立 task_id。

const (
	// pullTimeout 触发下载的调用超时：服务端「立即返回 task_id」，实际是毫秒级
	pullTimeout = 20 * time.Second
	// statusTimeout 查询下载进度超时
	statusTimeout = 15 * time.Second
	// deleteTimeout 删除变体超时（删目录，文件多时略慢）
	deleteTimeout = 60 * time.Second
	// uploadTimeout 上传超时：镜像包可达数 GB，按流式写盘给足余量（0 表示不限，
	// 由调用方 ctx 控制；这里给 1 小时避免 goroutine 永挂）
	uploadTimeout = 60 * time.Minute
)

// ImageFileNames 一个变体的三件套文件名（服务端校验的唯一合法集合）
var ImageFileNames = []string{"system.img", "vendor.img", "initramfs.img"}

// PullRequest 触发镜像下载（POST /image/pull）
type PullRequest struct {
	// Variant 变体名（仅允许字母/数字/下划线/中划线）
	Variant string `json:"variant"`
	// URL 镜像文件下载地址（http/https）
	URL string `json:"url"`
	// Filename 目标文件名：system.img / vendor.img / initramfs.img
	Filename string `json:"filename"`
}

// PullResult POST /image/pull 的 data 结构
type PullResult struct {
	TaskID string `json:"task_id"`
}

// ImageStatus GET /image/status/{task_id} 的 data 结构
type ImageStatus struct {
	TaskID     string `json:"task_id"`
	Variant    string `json:"variant"`
	Filename   string `json:"filename"`
	Status     string `json:"status"` // pending / running / completed / failed
	Downloaded int64  `json:"downloaded"`
	Total      int64  `json:"total"`
	Error      string `json:"error"`
}

// UploadResult POST /image/upload/{variant} 的 data 结构
type UploadResult struct {
	Variant  string `json:"variant"`
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
	// Ready 该变体三件套是否已齐全（齐全即可用于创建该变体的实例）
	Ready bool `json:"ready"`
}

// PullImage 触发一次镜像文件下载（POST /image/pull），返回任务 ID。
func (c *InstanceClient) PullImage(ctx context.Context, req PullRequest) (*PullResult, error) {
	if strings.TrimSpace(req.Variant) == "" {
		return nil, fmt.Errorf("变体名不能为空")
	}
	if strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("镜像下载地址不能为空")
	}
	if !isImageFileName(req.Filename) {
		return nil, fmt.Errorf("文件名只能是 %s 之一", strings.Join(ImageFileNames, " / "))
	}
	body, err := c.do(ctx, http.MethodPost, "/image/pull", req, pullTimeout)
	if err != nil {
		return nil, err
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, err
	}
	var res PullResult
	if len(data) > 0 {
		if err := json.Unmarshal(data, &res); err != nil {
			return nil, fmt.Errorf("/image/pull 响应解析失败: %w", err)
		}
	}
	if res.TaskID == "" {
		return nil, fmt.Errorf("/image/pull 未返回任务 ID")
	}
	return &res, nil
}

// ImageStatus 查询下载任务进度（GET /image/status/{task_id}）。
func (c *InstanceClient) ImageStatus(ctx context.Context, taskID string) (*ImageStatus, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("任务 ID 不能为空")
	}
	body, err := c.do(ctx, http.MethodGet, "/image/status/"+taskID, nil, statusTimeout)
	if err != nil {
		return nil, err
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, err
	}
	var res ImageStatus
	if len(data) > 0 {
		if err := json.Unmarshal(data, &res); err != nil {
			return nil, fmt.Errorf("/image/status 响应解析失败: %w", err)
		}
	}
	if res.TaskID == "" {
		res.TaskID = taskID
	}
	return &res, nil
}

// DeleteImage 删除一个镜像变体（DELETE /image/{variant}，整个目录不可恢复）。
func (c *InstanceClient) DeleteImage(ctx context.Context, variant string) error {
	variant = strings.TrimSpace(variant)
	if variant == "" {
		return fmt.Errorf("变体名不能为空")
	}
	body, err := c.do(ctx, http.MethodDelete, "/image/"+variant, nil, deleteTimeout)
	if err != nil {
		return err
	}
	_, err = unwrapEnvelope(body)
	return err
}

// 上传已取消时由监听 goroutine 关闭管道触发（CancelRomUpload 的取消路径）。
var errUploadCancelled = fmt.Errorf("上传已取消")

// progressChunk 每上传多少字节回调一次进度（约 5MB）：数 GB 文件按 32KB 块写，
// 不节流的话每秒能产生上千次回调/事件，前端会被事件淹没。
const progressChunk = 5 * 1024 * 1024

// progressCountingWriter 包装 multipart 的 file part，统计已写字节数并按
// progressChunk 节流回调 progressFn。注意：multipart 边界与表单字段有少量额外
// 字节，sent 会略大于文件大小；回调方按「sent ≥ total」显示 100% 即可。
type progressCountingWriter struct {
	w          io.Writer
	total      int64 // 文件字节数（回调展示用）
	sent       int64 // 已写入 part 的累计字节数
	reported   int64 // 上次回调时的 sent 值
	progressFn func(sentBytes, totalBytes int64)
}

func (p *progressCountingWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	if n > 0 {
		p.sent += int64(n)
		if p.progressFn != nil && p.sent-p.reported >= progressChunk {
			p.reported = p.sent
			p.progressFn(p.sent, p.total)
		}
	}
	return n, err
}

// UploadImage 上传镜像到指定变体（POST /image/upload/{variant}，multipart 字段名 file）。
//
// filePath 可以是：
//   - 一个 zip 包（推荐）：包内恰好包含三件套，服务端校验后一次落盘，三件齐全；
//   - 单个 img 文件：filename 必须显式给出（system.img / vendor.img / initramfs.img），
//     否则服务端按上传文件名判断，普通文件名会被拒绝。
//
// 大文件流式上传（不读进内存），超时由 uploadTimeout 兜底；ctx 被取消时主动断开
// 上传管道（返回 errUploadCancelled）。progressFn 非空时按进度节流回调
// （约每 5MB 一次 + 收尾 100% 一次），回调在内部 goroutine 触发、须自行保证并发安全。
func (c *InstanceClient) UploadImage(ctx context.Context, variant, filePath, filename string, progressFn func(sentBytes, totalBytes int64)) (*UploadResult, error) {
	variant = strings.TrimSpace(variant)
	if variant == "" {
		return nil, fmt.Errorf("变体名不能为空")
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, fmt.Errorf("文件路径不能为空")
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("读取文件信息失败: %w", err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("%s 是目录，请选择镜像文件或 zip 包", filePath)
	}
	base := filepath.Base(filePath)
	// 单文件方式必须显式指定合法文件名；zip 方式服务端忽略 filename
	if !strings.HasSuffix(strings.ToLower(base), ".zip") {
		if filename == "" {
			filename = base
		}
		if !isImageFileName(filename) {
			return nil, fmt.Errorf("文件名只能是 %s 之一（或上传包含三件套的 zip 包）",
				strings.Join(ImageFileNames, " / "))
		}
	} else {
		filename = "" // zip 方式忽略
	}

	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		// 字段名固定为 file（服务端契约）
		part, err := mw.CreateFormFile("file", base)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(&progressCountingWriter{w: part, total: stat.Size(), progressFn: progressFn}, f); err != nil {
			pw.CloseWithError(err)
			return
		}
		// 文件内容写完：把进度顶到 100%（io.Copy 的节流可能恰好没触发最后一次回调）
		if progressFn != nil {
			progressFn(stat.Size(), stat.Size())
		}
		if filename != "" {
			if err := mw.WriteField("filename", filename); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		pw.CloseWithError(mw.Close())
	}()

	// 取消/超时监听：ctx 结束时关闭写端，令 io.Copy 立刻返回错误（否则 1h 超时前
	// 上传 goroutine 会一直挂在 Write 上）。unregister 用于正常结束时解除监听。
	// 单次上传独占一个 ctx（WithTimeout 子上下文），不会误伤同 client 的其他调用。
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				_ = pw.CloseWithError(errUploadCancelled)
			case <-stopWatch:
			}
		}()
	}

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, c.baseURL+"/image/upload/"+variant, pr)
	if err != nil {
		return nil, fmt.Errorf("构造上传请求失败: %w", err)
	}
	req.SetBasicAuth(c.user, c.password)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		// 优先区分取消与超时（两者都是 ctx 错误，但用户语义不同）
		switch {
		case errors.Is(reqCtx.Err(), context.Canceled):
			return nil, errUploadCancelled
		case errors.Is(reqCtx.Err(), context.DeadlineExceeded):
			return nil, fmt.Errorf("上传超时（超过 %v 未完成）", uploadTimeout)
		}
		return nil, fmt.Errorf("上传失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRespSize))
	if err != nil {
		return nil, fmt.Errorf("读取上传响应失败: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("上传认证失败（HTTP 401，检查 Basic Auth 凭据）")
	}
	if resp.StatusCode >= 400 {
		// 标准信封 {"code":..,"message":".."}：优先透出 message
		if msg := envelopeMessage(body); msg != "" {
			return nil, fmt.Errorf("上传失败（HTTP %d）: %s", resp.StatusCode, msg)
		}
		// 非标准体（空体/纯文本/其他 JSON 结构）：透出原始片段，
		// 否则服务端真实报错原因会被「上传失败（HTTP 500）」吞掉
		if snip := strings.TrimSpace(string(body)); snip != "" {
			return nil, fmt.Errorf("上传失败（HTTP %d）: %s", resp.StatusCode, truncateBody([]byte(snip)))
		}
		return nil, fmt.Errorf("上传失败（HTTP %d）", resp.StatusCode)
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, err
	}
	var res UploadResult
	if len(data) > 0 {
		if err := json.Unmarshal(data, &res); err != nil {
			return nil, fmt.Errorf("/image/upload 响应解析失败: %w", err)
		}
	}
	return &res, nil
}

// isImageFileName 是否为三件套合法文件名。
func isImageFileName(name string) bool {
	for _, n := range ImageFileNames {
		if name == n {
			return true
		}
	}
	return false
}
