package sdkapi

import (
	"encoding/json"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("10.10.0.1")
	if c.BaseURL != "http://10.10.0.1:8000" {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, "http://10.10.0.1:8000")
	}
	if c.Username != "admin" {
		t.Errorf("Username = %q, want %q", c.Username, "admin")
	}
	if c.Password != "" {
		t.Errorf("Password should be empty by default, got %q", c.Password)
	}
	if c.httpClient == nil {
		t.Error("httpClient should not be nil")
	}
}

func TestNewClientWithTimeout(t *testing.T) {
	c := NewClientWithTimeout("10.10.0.2", 5e9)
	if c.BaseURL != "http://10.10.0.2:8000" {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, "http://10.10.0.2:8000")
	}
}

func TestSetPassword(t *testing.T) {
	c := NewClient("10.10.0.1")
	c.SetPassword("secret")
	if c.Password != "secret" {
		t.Errorf("Password = %q, want %q", c.Password, "secret")
	}
}

func TestDeviceConstants(t *testing.T) {
	if DevicePort != 8000 {
		t.Errorf("DevicePort = %d, want 8000", DevicePort)
	}
	if DeviceUser != "admin" {
		t.Errorf("DeviceUser = %q, want %q", DeviceUser, "admin")
	}
}

func TestIsSuccessCode(t *testing.T) {
	tests := []struct {
		code json.RawMessage
		want bool
	}{
		{json.RawMessage(`0`), true},
		{json.RawMessage(`200`), true},
		{json.RawMessage(`"0"`), true},
		{json.RawMessage(`"200"`), true},
		{json.RawMessage(`404`), false},
		{json.RawMessage(`"404"`), false},
		{json.RawMessage(`null`), false},
		{json.RawMessage(`true`), false},
	}
	for _, tt := range tests {
		got := isSuccessCode(tt.code)
		if got != tt.want {
			t.Errorf("isSuccessCode(%s) = %v, want %v", string(tt.code), got, tt.want)
		}
	}
}

func TestParseAPIResponse(t *testing.T) {
	body := `{"code":0,"msg":"ok","data":{"count":1,"list":[{"id":"1"}]}}`
	resp, err := parseAPIResponse([]byte(body))
	if err != nil {
		t.Fatalf("parseAPIResponse() error: %v", err)
	}
	if resp == nil {
		t.Fatal("resp should not be nil")
	}
}

func TestParseAPIResponseError(t *testing.T) {
	body := `{"code":61,"msg":"auth failed","data":null}`
	_, err := parseAPIResponse([]byte(body))
	if err == nil {
		t.Error("expected error for non-success code, got nil")
	}
}

func TestParseListData(t *testing.T) {
	data := json.RawMessage(`{"count":2,"list":[{"name":"a"},{"name":"b"}]}`)
	var result []struct {
		Name string `json:"name"`
	}
	if err := parseListData(data, &result); err != nil {
		t.Fatalf("parseListData() error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("len(result) = %d, want 2", len(result))
	}
}

func TestParseListDataFlatArray(t *testing.T) {
	data := json.RawMessage(`[{"name":"x"}]`)
	var result []struct {
		Name string `json:"name"`
	}
	if err := parseListData(data, &result); err != nil {
		t.Fatalf("parseListData() error: %v", err)
	}
	if len(result) != 1 || result[0].Name != "x" {
		t.Errorf("result = %v, unexpected", result)
	}
}
