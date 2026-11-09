package v4host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// --- V4 创建实例与镜像列表（8088 REST）---
//
//	POST /android/create  → {instance}（创建后 state=stopped，需再 /android/start）
//	GET  /image/list      → {root, variants:[{variant,files,ready}], total}
//
// 两个接口都走统一的 Basic Auth 与 {code,message,data} 外壳（见 instance.go 的 do/unwrapEnvelope）。

// CreateRequest 创建实例参数（POST /android/create）。
//
// 全部可选：name 缺省由服务端自动生成，资源配置缺省 4 核 / 4096MB 内存 / 8192MB 磁盘。
// 镜像两种指定方式（二选一，三件套路径优先级更高）：
//   - Variant：镜像管理里的变体名（GET /image/list 的 variants[].variant）；
//   - SystemImage/VendorImage/InitramfsImage：三件套绝对路径。
//
// 都不传 = 用 tebox-api 的默认系统镜像。
type CreateRequest struct {
	Name     string `json:"name,omitempty"`
	CPUCores int32  `json:"cpu_cores,omitempty"`
	MemoryMB int32  `json:"memory_mb,omitempty"`
	DiskMB   int32  `json:"disk_mb,omitempty"`
	Backend  string `json:"backend,omitempty"`

	Variant        string `json:"variant,omitempty"`
	SystemImage    string `json:"system_image,omitempty"`
	VendorImage    string `json:"vendor_image,omitempty"`
	InitramfsImage string `json:"initramfs_image,omitempty"`
}

// CreateResult POST /android/create 的 data 结构
type CreateResult struct {
	Instance *Instance `json:"instance"`
}

// Create 创建实例（POST /android/create）。返回的实例处于 stopped，需再调 Start。
func (c *InstanceClient) Create(ctx context.Context, req CreateRequest) (*CreateResult, error) {
	body, err := c.do(ctx, http.MethodPost, "/android/create", req, opTimeout)
	if err != nil {
		return nil, err
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, err
	}
	var res CreateResult
	if len(data) > 0 {
		if err := json.Unmarshal(data, &res); err != nil {
			return nil, fmt.Errorf("/android/create 响应解析失败: %w", err)
		}
	}
	if res.Instance == nil {
		return nil, fmt.Errorf("/android/create 未返回实例信息")
	}
	return &res, nil
}

// ImageFile 变体内单个镜像文件的状态
type ImageFile struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Ready bool   `json:"ready"`
}

// ImageVariant 一个镜像变体（三件套就绪状态）
type ImageVariant struct {
	Variant string      `json:"variant"`
	Files   []ImageFile `json:"files"`
	// Ready 三件套齐全且均大于 0 字节（可用于创建该变体的实例）
	Ready bool `json:"ready"`
}

// ImageListResult GET /image/list 的 data 结构
type ImageListResult struct {
	Root     string         `json:"root"`
	Variants []ImageVariant `json:"variants"`
	Total    int            `json:"total"`
}

// ListImages 拉取镜像变体列表（GET /image/list）。
func (c *InstanceClient) ListImages(ctx context.Context) (*ImageListResult, error) {
	body, err := c.do(ctx, http.MethodGet, "/image/list", nil, listTimeout)
	if err != nil {
		return nil, err
	}
	data, err := unwrapEnvelope(body)
	if err != nil {
		return nil, err
	}
	var res ImageListResult
	if len(data) > 0 {
		if err := json.Unmarshal(data, &res); err != nil {
			return nil, fmt.Errorf("/image/list 响应解析失败: %w", err)
		}
	}
	if res.Variants == nil {
		res.Variants = []ImageVariant{}
	}
	return &res, nil
}
