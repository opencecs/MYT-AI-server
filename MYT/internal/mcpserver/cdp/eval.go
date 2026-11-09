package cdp

// eval.go — Runtime 求值 helpers（browser 包共用）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrRefNotInSnapshot uid 不在当前快照的 refs 表（从未 ui_map 或表已整体重建）。
var ErrRefNotInSnapshot = errors.New("uid 不在当前快照，请重新 ui_map")

// ErrRefPageChanged uid 在快照表中，但页面内按身份找不到/无法唯一定位
// （界面已刷新、元素消失或多候选）。
var ErrRefPageChanged = errors.New("页面已变化，无法唯一定位该元素，请重新 ui_map")

// CallFunctionOn 在指定 RemoteObject（this = 目标元素）上执行函数并返回结果值。
func (c *Client) CallFunctionOn(ctx context.Context, wsURL, objectID, functionDeclaration string, timeout time.Duration) (any, error) {
	res, err := c.Call(ctx, wsURL, "Runtime.callFunctionOn", map[string]any{
		"objectId":            objectID,
		"functionDeclaration": functionDeclaration,
		"returnByValue":       true,
	}, timeout)
	if err != nil {
		return nil, err
	}
	var out struct {
		Result struct {
			Value any `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text string `json:"text"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, err
	}
	if out.ExceptionDetails != nil {
		return nil, fmt.Errorf("元素操作异常: %s", out.ExceptionDetails.Text)
	}
	return out.Result.Value, nil
}
