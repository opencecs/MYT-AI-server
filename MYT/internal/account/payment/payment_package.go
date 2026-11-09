package payment

import (
	"encoding/json"
	"fmt"
	"log"
)

// GetPackages 获取套餐列表
func (s *Service) GetPackages(token string) ([]Package, error) {
	data := map[string]string{"token": token}

	body, err := s.postAPI("get_package", data)
	if err != nil {
		return nil, err
	}

	var apiResp struct {
		Code any       `json:"code"`
		Data []Package `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("解析套餐响应失败: %w", err)
	}

	if err := checkResponse(apiResp.Code, body); err != nil {
		return nil, err
	}

	return apiResp.Data, nil
}

// GetDeviceBindStatus 查询设备绑定状态
// API 要求双重 JSON 序列化：data 内的 data 字段本身也是 JSON 字符串
func (s *Service) GetDeviceBindStatus(deviceIDs []string, token string) ([]DeviceBindInfo, error) {
	// 内层 data：{"host":["device1","device2"]}
	innerData, _ := json.Marshal(map[string][]string{"host": deviceIDs})

	// 外层 data：{"act":"get","data":"{\"host\":[\"device1\",\"device2\"]}","token":"xxx"}
	data := map[string]string{
		"act":   "get",
		"data":  string(innerData),
		"token": token,
	}

	log.Printf("[payment] GetDeviceBindStatus: count=%d, innerData=%s", len(deviceIDs), string(innerData)[:min(len(innerData), 200)])

	body, err := s.postAPI("user_host_oper", data)
	if err != nil {
		return nil, err
	}

	var apiResp struct {
		Code any              `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("解析绑定状态响应失败: %w", err)
	}

	log.Printf("[payment] GetDeviceBindStatus响应: code=%v, data=%s", apiResp.Code, string(apiResp.Data)[:min(len(apiResp.Data), 200)])

	if err := checkResponse(apiResp.Code, body); err != nil {
		return nil, err
	}

	// data 可能是 map[string]float64 或数组，兼容处理
	statusMap := make(map[string]int)
	if len(apiResp.Data) > 0 && apiResp.Data[0] == '[' {
		// 数组格式：按 deviceIDs 顺序对应
		var arr []float64
		if err := json.Unmarshal(apiResp.Data, &arr); err == nil {
			for i, v := range arr {
				if i < len(deviceIDs) {
					statusMap[deviceIDs[i]] = int(v)
				}
			}
		}
	} else {
		// map 格式：key 是 device_id
		var m map[string]float64
		if err := json.Unmarshal(apiResp.Data, &m); err == nil {
			for id, v := range m {
				statusMap[id] = int(v)
			}
		}
	}

	var result []DeviceBindInfo
	for _, id := range deviceIDs {
		status := 0
		if v, exists := statusMap[id]; exists {
			status = v
		}
		result = append(result, DeviceBindInfo{DeviceID: id, BindStatus: status})
	}

	return result, nil
}

// BindHost 绑定单台设备到当前账号
// 绑定时 host 字段传单个设备ID字符串（非数组），与查询的数组格式不同
func (s *Service) BindHost(deviceID, token string) error {
	innerData, _ := json.Marshal(map[string]string{"host": deviceID})
	data := map[string]string{
		"act":   "bind",
		"data":  string(innerData),
		"token": token,
	}

	log.Printf("[payment] BindHost: deviceID=%s", deviceID)

	body, err := s.postAPI("user_host_oper", data)
	if err != nil {
		return err
	}

	var apiResp struct {
		Code any    `json:"code"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("解析绑定响应失败: %w", err)
	}

	if err := checkResponse(apiResp.Code, body); err != nil {
		return err
	}

	log.Printf("[payment] BindHost成功: deviceID=%s", deviceID)
	return nil
}

// UnbindHost 批量解绑设备，需要短信验证码（vcode/vkey）
func (s *Service) UnbindHost(deviceIDs []string, token, vcode, vkey string) error {
	innerData, _ := json.Marshal(map[string]interface{}{
		"hlist": deviceIDs,
		"vcode": vcode,
		"vkey":  vkey,
	})
	data := map[string]string{
		"act":   "batchUnbind",
		"data":  string(innerData),
		"token": token,
	}

	log.Printf("[payment] UnbindHost: deviceIDs=%v", deviceIDs)

	body, err := s.postAPI("user_host_oper", data)
	if err != nil {
		return err
	}

	var apiResp struct {
		Code any    `json:"code"`
		Msg string `json:"msg"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("解析解绑响应失败: %w", err)
	}

	if err := checkResponse(apiResp.Code, body); err != nil {
		return err
	}

	log.Printf("[payment] UnbindHost成功: deviceIDs=%v", deviceIDs)
	return nil
}

// GetSlotExpireInfo 查询设备坑位到期信息（委托给 slot 服务）
func (s *Service) GetSlotExpireInfo(deviceIDs []string) ([]DeviceSlotInfo, error) {
	results := s.slotSvc.QuerySlotInfo(deviceIDs)
	var deviceSlots []DeviceSlotInfo
	for _, r := range results {
		if r.Error != "" {
			return nil, fmt.Errorf("查询设备%s坑位失败: %s", r.DeviceID, r.Error)
		}
		slots := make([]SlotExpireInfo, len(r.Slots))
		for i, sl := range r.Slots {
			slots[i] = SlotExpireInfo{
				SlotIndex: sl.Index,
				ExpireTs:  sl.ExpireTime,
				State:     sl.State,
			}
		}
		deviceSlots = append(deviceSlots, DeviceSlotInfo{
			DeviceID: r.DeviceID,
			Slots:    slots,
		})
	}
	return deviceSlots, nil
}
