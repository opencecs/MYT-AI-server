package sdkapi

import (
	"encoding/json"
	"fmt"
)

// IscsiTargetInfo 发现的 Target 信息
type IscsiTargetInfo struct {
	IQN    string `json:"iqn"`
	Portal string `json:"portal"`
}

// IscsiSessionInfo 已挂载的会话信息
type IscsiSessionInfo struct {
	IQN     string `json:"iqn"`
	Portal  string `json:"portal"`
	Device  string `json:"device"`
	LocalIP string `json:"localIP"`
}

// IscsiNicInfo 网卡信息
type IscsiNicInfo struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	Mac  string `json:"mac"`
	IsUp bool   `json:"isUp"`
}

// IscsiDiskImageResult 创建磁盘映像的返回结果
type IscsiDiskImageResult struct {
	IQN     string `json:"iqn"`
	Lun     int    `json:"lun"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// IscsiDiskImageInfo 磁盘映像信息（Target 管理API返回）
type IscsiDiskImageInfo struct {
	Name        string `json:"name"`
	Lun         int    `json:"lun"`
	SizeGB      float64 `json:"size_gb"`
	Type        string `json:"type"`
	IQN         string `json:"iqn"`
	StoragePath string `json:"storagePath"`
}

// IscsiMountResult 挂载操作结果
type IscsiMountResult struct {
	Device             string `json:"device"`
	MountPoint         string `json:"mountPoint"`
	StorageMode        string `json:"storageMode"`
	MigratedContainers int    `json:"migratedContainers"`
	Success            bool   `json:"success"`
}

// IscsiStorageStatus iSCSI 存储状态（GET /mytIscsi/status）
type IscsiStorageStatus struct {
	StorageMode string `json:"storageMode"`
	MountPoint  string `json:"mountPoint"`
	Device      string `json:"device"`
	IsMounted   bool   `json:"isMounted"`
	TotalMB     int64  `json:"totalMB"`
	UsedMB      int64  `json:"usedMB"`
	AvailMB     int64  `json:"availMB"`
}

// IscsiResizeResult 设备端扩容结果（POST /mytIscsi/resize）
type IscsiResizeResult struct {
	Device     string `json:"device"`
	Partition  string `json:"partition"`
	MountPoint string `json:"mountPoint"`
	TotalMB    int64  `json:"totalMB"`
	UsedMB     int64  `json:"usedMB"`
	AvailMB    int64  `json:"availMB"`
	Success    bool   `json:"success"`
}

// IscsiUmountResult 卸载操作结果
type IscsiUmountResult struct {
	StorageMode        string `json:"storageMode"`
	MigratedContainers int    `json:"migratedContainers"`
	Success            bool   `json:"success"`
}

// iscsiDataWrapper SDK iSCSI 接口 data 字段的通用包装
// discover: {"targets":[...]}  listNic: {"nics":[...]}  list: {"sessions":[...]}
type iscsiDiscoverData struct {
	Targets []IscsiTargetInfo `json:"targets"`
}
type iscsiNicData struct {
	Nics []IscsiNicInfo `json:"nics"`
}
type iscsiSessionData struct {
	Sessions []IscsiSessionInfo `json:"sessions"`
}

// DiscoverIscsi 发现 Target
func (c *Client) DiscoverIscsi(portal string) ([]IscsiTargetInfo, error) {
	body, err := c.postJSON("/mytIscsi/discover", map[string]string{"portal": portal})
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var data iscsiDiscoverData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析发现结果失败: %w", err)
	}
	return data.Targets, nil
}

// LoginIscsi 挂载 iSCSI 磁盘
func (c *Client) LoginIscsi(portal, iqn, localIP string) error {
	body, err := c.postJSON("/mytIscsi/login", map[string]string{
		"portal":  portal,
		"iqn":     iqn,
		"localIP": localIP,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// LogoutIscsi 卸载 iSCSI 磁盘
func (c *Client) LogoutIscsi(portal, iqn string) error {
	body, err := c.postJSON("/mytIscsi/logout", map[string]string{
		"portal": portal,
		"iqn":    iqn,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// ListIscsi 查询已挂载列表
func (c *Client) ListIscsi() ([]IscsiSessionInfo, error) {
	body, err := c.get("/mytIscsi/list")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var data iscsiSessionData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析挂载列表失败: %w", err)
	}
	return data.Sessions, nil
}

// ListIscsiNic 获取设备网卡列表
func (c *Client) ListIscsiNic() ([]IscsiNicInfo, error) {
	body, err := c.get("/mytIscsi/listNic")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var data iscsiNicData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析网卡列表失败: %w", err)
	}
	return data.Nics, nil
}

// PartitionIscsi 分区
func (c *Client) PartitionIscsi(device, label string) ([]string, error) {
	body, err := c.postJSON("/mytIscsi/partition", map[string]string{
		"device": device,
		"label":  label,
	})
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var result []string
	if err := parseListData(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("解析分区结果失败: %w", err)
	}
	return result, nil
}

// FormatIscsi 格式化
func (c *Client) FormatIscsi(partition, fsType string) error {
	body, err := c.postJSON("/mytIscsi/format", map[string]string{
		"partition": partition,
		"fsType":    fsType,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// CreateDiskImage 创建磁盘映像
func (c *Client) CreateDiskImage(portal, name string, size int64, diskType string) (*IscsiDiskImageResult, error) {
	body, err := c.postJSON("/mytIscsi/createDiskImage", map[string]any{
		"portal": portal,
		"name":   name,
		"size":   size,
		"type":   diskType,
	})
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var result IscsiDiskImageResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("解析创建磁盘映像结果失败: %w", err)
	}
	return &result, nil
}

// DeleteDiskImage 删除磁盘映像
func (c *Client) DeleteDiskImage(portal, name string) error {
	body, err := c.postJSON("/mytIscsi/deleteDiskImage", map[string]string{
		"portal": portal,
		"name":   name,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// MountIscsi 挂载 iSCSI 磁盘（一体化：登录+分区格式化+挂载+迁移容器）
func (c *Client) MountIscsi(portal, iqn string, autoInit bool, localIP string) (*IscsiMountResult, error) {
	body, err := c.postJSON("/mytIscsi/mount", map[string]any{
		"portal":   portal,
		"iqn":      iqn,
		"autoInit": autoInit,
		"localIP":  localIP,
	})
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var result IscsiMountResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("解析挂载结果失败: %w", err)
	}
	return &result, nil
}

// GetIscsiStatus 查询 iSCSI 存储状态
func (c *Client) GetIscsiStatus() (*IscsiStorageStatus, error) {
	body, err := c.get("/mytIscsi/status")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var result IscsiStorageStatus
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("解析存储状态失败: %w", err)
	}
	return &result, nil
}

// UmountIscsi 卸载 iSCSI 磁盘（一体化：迁移容器+卸载+登出）
func (c *Client) UmountIscsi(portal, iqn string, force bool) (*IscsiUmountResult, error) {
	body, err := c.postJSON("/mytIscsi/umount", map[string]any{
		"portal": portal,
		"iqn":    iqn,
		"force":  force,
	})
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var result IscsiUmountResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("解析卸载结果失败: %w", err)
	}
	return &result, nil
}

// CleanupIscsi 手动清理 iSCSI 残留状态（session、挂载、node 记录）
func (c *Client) CleanupIscsi(portal, iqn string, force bool) error {
	body, err := c.postJSON("/mytIscsi/cleanup", map[string]any{
		"portal": portal,
		"iqn":    iqn,
		"force":  force,
	})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

// ResizeIscsi 设备端扩容（rescan + growpart + resize2fs）
func (c *Client) ResizeIscsi(portal, iqn string) (*IscsiResizeResult, error) {
	body, err := c.postJSON("/mytIscsi/resize", map[string]any{
		"portal": portal,
		"iqn":    iqn,
	})
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var result IscsiResizeResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("解析扩容结果失败: %w", err)
	}
	return &result, nil
}
