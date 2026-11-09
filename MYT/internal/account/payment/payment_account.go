package payment

import (
	"fmt"
	"log"

	"github.com/moyunteng/myt/internal/auth"
	"github.com/moyunteng/myt/internal/util"
)

// CancelAccountResult 注销账号结果
type CancelAccountResult struct {
	Uid     string `json:"uid"`
	HostNum int    `json:"host_num"`
}

// CancelUser 注销当前账号，需要短信验证码（vcode/vkey）。
// 成功返回注销账号的 uid 与关联主机数；失败带服务端原 msg（如验证码错误）。
func (s *Service) CancelUser(token, vcode, vkey string) (*CancelAccountResult, error) {
	result, err := auth.CancelUserAPI(s.client, token, vcode, vkey)
	if err != nil {
		return nil, fmt.Errorf("%s", util.FriendlyNetError(err))
	}
	if !isSuccessCode(result.Code) {
		log.Printf("[payment] CancelUser失败: code=%s, msg=%q", result.Code, result.Msg)
		msg := result.Msg
		if msg == "" {
			msg = fmt.Sprintf("注销失败(code=%s)", result.Code)
		}
		// 带上服务端原始 code，前端 parseCancelError 可按 code 细分文案
		return nil, fmt.Errorf("注销失败[%s]: %s", result.Code, msg)
	}

	log.Printf("[payment] CancelUser成功: uid=%s, host_num=%d", result.Uid, result.HostNum)
	return &CancelAccountResult{Uid: result.Uid, HostNum: result.HostNum}, nil
}
