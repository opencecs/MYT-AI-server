package v4host

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 镜像下载：POST /image/pull 带齐 variant/url/filename，解析 task_id
func TestInstanceClientPullImage(t *testing.T) {
	srv := newV4Server(t, func(path, body string) (int, string) {
		if path != "/image/pull" {
			t.Errorf("path = %q, want /image/pull", path)
		}
		var req map[string]string
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("pull body 非法 JSON: %v", err)
		}
		if req["variant"] != "V1" || req["filename"] != "system.img" || req["url"] == "" {
			t.Errorf("pull body 字段不符: %v", req)
		}
		return 200, `{"code":0,"message":"ok","data":{"task_id":"task-1"}}`
	})
	res, err := NewInstanceClient(srv.host(), "", "").PullImage(context.Background(), PullRequest{
		Variant: "V1", URL: "http://x/system.img", Filename: "system.img",
	})
	if err != nil {
		t.Fatalf("PullImage 失败: %v", err)
	}
	if res.TaskID != "task-1" {
		t.Errorf("task_id = %q, want task-1", res.TaskID)
	}
}

// 非法文件名在本地就拦下（不打服务端）
func TestInstanceClientPullImageBadFilename(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		t.Error("非法文件名不应发起请求")
		return 200, `{"code":0,"data":{"task_id":"x"}}`
	})
	_, err := NewInstanceClient(srv.host(), "", "").PullImage(context.Background(), PullRequest{
		Variant: "V1", URL: "http://x/a", Filename: "a.img",
	})
	if err == nil || !strings.Contains(err.Error(), "文件名") {
		t.Errorf("非法文件名应报错, got %v", err)
	}
}

// 下载进度：解析 status/downloaded/total/error
func TestInstanceClientImageStatus(t *testing.T) {
	srv := newV4Server(t, func(path, _ string) (int, string) {
		if path != "/image/status/task-1" {
			t.Errorf("path = %q, want /image/status/task-1", path)
		}
		return 200, `{"code":0,"message":"ok","data":{"task_id":"task-1","variant":"V1",
			"filename":"system.img","status":"running","downloaded":512,"total":1024,"error":""}}`
	})
	st, err := NewInstanceClient(srv.host(), "", "").ImageStatus(context.Background(), "task-1")
	if err != nil {
		t.Fatalf("ImageStatus 失败: %v", err)
	}
	if st.Status != "running" || st.Downloaded != 512 || st.Total != 1024 || st.Variant != "V1" {
		t.Errorf("进度字段不符: %+v", st)
	}
}

// 失败任务：error 透出
func TestInstanceClientImageStatusFailed(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 200, `{"code":0,"message":"ok","data":{"task_id":"t","status":"failed","error":"下载失败: dial tcp"}}`
	})
	st, err := NewInstanceClient(srv.host(), "", "").ImageStatus(context.Background(), "t")
	if err != nil {
		t.Fatalf("ImageStatus 失败: %v", err)
	}
	if st.Status != "failed" || !strings.Contains(st.Error, "dial tcp") {
		t.Errorf("失败任务字段不符: %+v", st)
	}
}

// 删除变体：DELETE /image/{variant}
func TestInstanceClientDeleteImage(t *testing.T) {
	srv := newV4Server(t, func(path, _ string) (int, string) {
		if path != "/image/V1" {
			t.Errorf("path = %q, want /image/V1", path)
		}
		return 200, `{"code":0,"message":"ok","data":{}}`
	})
	if err := NewInstanceClient(srv.host(), "", "").DeleteImage(context.Background(), "V1"); err != nil {
		t.Fatalf("DeleteImage 失败: %v", err)
	}
	if got := srv.paths[0]; !strings.HasPrefix(got, "DELETE ") {
		t.Errorf("method = %q, want DELETE", got)
	}
}

// 删除失败：message 透出
func TestInstanceClientDeleteImageError(t *testing.T) {
	srv := newV4Server(t, func(_, _ string) (int, string) {
		return 400, `{"code":400,"message":"variant 非法","data":null}`
	})
	err := NewInstanceClient(srv.host(), "", "").DeleteImage(context.Background(), "bad name")
	if err == nil || !strings.Contains(err.Error(), "variant 非法") {
		t.Errorf("删除失败应透出 message, got %v", err)
	}
}

// 上传：multipart 字段名为 file + filename，响应解析
func TestInstanceClientUploadImage(t *testing.T) {
	var gotVariant, gotFile, gotFilename string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVariant = strings.TrimPrefix(r.URL.Path, "/image/upload/")
		mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mt, "multipart/") {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			switch p.FormName() {
			case "file":
				gotFile = p.FileName()
			case "filename":
				buf := make([]byte, 32)
				n, _ := p.Read(buf)
				gotFilename = string(buf[:n])
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"variant":"V1","filename":"system.img","size":1,"ready":false}}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := srv.URL[strings.LastIndex(srv.URL, "/")+1:]
	res, err := NewInstanceClient(host, "", "").UploadImage(context.Background(), "V1", path, "system.img", nil)
	if err != nil {
		t.Fatalf("UploadImage 失败: %v", err)
	}
	if gotVariant != "V1" || gotFile != "payload.bin" || gotFilename != "system.img" {
		t.Errorf("multipart 不符: variant=%q file=%q filename=%q", gotVariant, gotFile, gotFilename)
	}
	if res.Variant != "V1" || res.Size != 1 || res.Ready {
		t.Errorf("上传结果不符: %+v", res)
	}
}

// 上传单文件但文件名非法：本地拦下
func TestInstanceClientUploadImageBadName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "random.bin")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewInstanceClient("127.0.0.1:1", "", "").UploadImage(context.Background(), "V1", path, "", nil)
	if err == nil || !strings.Contains(err.Error(), "文件名") {
		t.Errorf("非法文件名应本地报错, got %v", err)
	}
}

// zip 包方式：不传 filename 字段（服务端按包内结构处理）
func TestInstanceClientUploadImageZip(t *testing.T) {
	var fields []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mt, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err != nil {
				break
			}
			fields = append(fields, p.FormName())
		}
		_ = mt
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"variant":"V1","filename":"","size":3,"ready":true}}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.zip")
	if err := os.WriteFile(path, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := srv.URL[strings.LastIndex(srv.URL, "/")+1:]
	res, err := NewInstanceClient(host, "", "").UploadImage(context.Background(), "V1", path, "", nil)
	if err != nil {
		t.Fatalf("UploadImage(zip) 失败: %v", err)
	}
	if !res.Ready {
		t.Error("zip 上传应返回 ready=true")
	}
	for _, f := range fields {
		if f == "filename" {
			t.Error("zip 方式不应携带 filename 字段")
		}
	}
}

// 上传进度回调：按 progressChunk 节流触发 + 收尾 100%（sent==total）
func TestInstanceClientUploadImageProgress(t *testing.T) {
	// 服务端慢速消费请求体（读一点睡一下），给进度回调留触发窗口
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","data":{"variant":"V1","filename":"system.img","size":0,"ready":false}}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "big.img")
	payload := make([]byte, 12*1024*1024) // 12MB，> progressChunk(5MB)，应至少回调 2 次
	for i := range payload {
		payload[i] = byte(i)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	host := srv.URL[strings.LastIndex(srv.URL, "/")+1:]

	var mu sync.Mutex
	var calls int
	var lastSent, lastTotal int64
	fn := func(sent, total int64) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		lastSent, lastTotal = sent, total
	}
	if _, err := NewInstanceClient(host, "", "").UploadImage(context.Background(), "V1", path, "system.img", fn); err != nil {
		t.Fatalf("UploadImage(progress) 失败: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls < 2 {
		t.Errorf("12MB 应至少回调 2 次（>5MB 节流 + 收尾 100%%），实际 %d 次", calls)
	}
	if lastTotal != int64(len(payload)) {
		t.Errorf("total = %d, want %d", lastTotal, len(payload))
	}
	if lastSent != lastTotal {
		t.Errorf("最后一次回调应为 100%%: sent=%d total=%d", lastSent, lastTotal)
	}
}

// 上传失败（非 2xx）：标准信封 message 透出
func TestInstanceClientUploadImageErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":500,"message":"磁盘空间不足","data":null}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "payload.zip")
	if err := os.WriteFile(path, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := srv.URL[strings.LastIndex(srv.URL, "/")+1:]
	_, err := NewInstanceClient(host, "", "").UploadImage(context.Background(), "zzz-test", path, "", nil)
	if err == nil || !strings.Contains(err.Error(), "磁盘空间不足") {
		t.Errorf("标准信封 message 应透出, got %v", err)
	}
}

// 上传失败（非 2xx）：非标准响应体（空 message / 纯文本 / 空 JSON）时原始片段透出
func TestInstanceClientUploadImageErrorRawBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("unzip: entry system.img missing"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "payload.zip")
	if err := os.WriteFile(path, []byte("zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := srv.URL[strings.LastIndex(srv.URL, "/")+1:]
	_, err := NewInstanceClient(host, "", "").UploadImage(context.Background(), "zzz-test", path, "", nil)
	if err == nil || !strings.Contains(err.Error(), "unzip: entry system.img missing") {
		t.Errorf("非标准响应体应透出原始片段, got %v", err)
	}
}

// 取消路径：ctx 取消后上传立即结束并返回 errUploadCancelled，服务端请求中断
func TestInstanceClientUploadImageCancel(t *testing.T) {
	requestDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 读到 EOF/错误即结束（客户端取消 → 管道断开 → 请求体中断）
		buf := make([]byte, 32*1024)
		for {
			if _, err := r.Body.Read(buf); err != nil {
				break
			}
		}
		close(requestDone)
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "big.img")
	// 大文件 + 服务端不读完不返回，让上传 goroutine 挂在 Write 上等待取消
	if err := os.WriteFile(path, make([]byte, 64*1024*1024), 0o600); err != nil {
		t.Fatal(err)
	}
	host := srv.URL[strings.LastIndex(srv.URL, "/")+1:]

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := NewInstanceClient(host, "", "").UploadImage(ctx, "V1", path, "system.img", nil)
		errCh <- err
	}()

	// 等请求确实到达服务端（注册到 ctx 取消监听之前取消会竞态丢失）
	select {
	case <-requestDone:
	case <-time.After(10 * time.Second):
		t.Fatal("上传请求未到达服务端")
	}
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, errUploadCancelled) {
			t.Errorf("取消后应返回 errUploadCancelled, got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("取消后上传未结束（管道未断开？）")
	}
}
