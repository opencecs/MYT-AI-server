package sdkapi

import (
	"encoding/json"
)

type SSHInfo struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
	User    string `json:"user"`
}

func (c *Client) GetSSHInfo() (*SSHInfo, error) {
	body, err := c.get("/link/ssh")
	if err != nil {
		return nil, err
	}
	resp, err := parseAPIResponse(body)
	if err != nil {
		return nil, err
	}
	var info SSHInfo
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) ToggleSSH(enable bool) error {
	body, err := c.postJSON("/link/ssh/enable", map[string]bool{"enable": enable})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) ChangeSSHPassword(password string) error {
	body, err := c.postJSON("/link/ssh/changePwd", map[string]string{"password": password})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}

func (c *Client) ToggleSSHRoot(enable bool) error {
	body, err := c.postJSON("/link/ssh/switchRoot", map[string]bool{"enable": enable})
	if err != nil {
		return err
	}
	return c.parseMutationResponse(body)
}
