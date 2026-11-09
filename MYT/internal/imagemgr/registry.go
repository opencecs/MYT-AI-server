package imagemgr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

// Docker Registry 相关结构体
type Manifest struct {
	Config Descriptor   `json:"config"`
	Layers []Descriptor `json:"layers"`
}

type Descriptor struct {
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
}

type ImageTag struct {
	Host     string
	Name     string
	Version  string
	Hash     string
	Authed   bool
	AuthInfo TokenInfo
}

type TokenInfo struct {
	AccessToken string `json:"access_token"`
	Token       string `json:"token"`
}

type ManifestFile struct {
	Config   string
	RepoTags []string
	Layers   []string
}

func parseImageTag(tag string) *ImageTag {
	parts := strings.SplitAfterN(tag, "/", 2)
	result := &ImageTag{}
	if len(parts) != 2 {
		result.Host = "registry-1.docker.io"
		imageParts := strings.SplitN(tag, ":", 2)
		result.Name = imageParts[0]
		if len(imageParts) == 2 {
			result.Version = imageParts[1]
		} else {
			result.Version = "latest"
		}
		return result
	}
	result.Host = strings.Trim(parts[0], "/")
	imageParts := strings.SplitN(parts[1], ":", 2)
	result.Name = imageParts[0]
	if len(imageParts) == 2 {
		result.Version = imageParts[1]
	} else {
		result.Version = "latest"
	}
	return result
}

func getProtocol(tag *ImageTag) string {
	if net.ParseIP(strings.Split(tag.Host, ":")[0]) != nil {
		return "http"
	}
	return "https"
}

func fetchHash(tag *ImageTag) error {
	url := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", getProtocol(tag), tag.Host, tag.Name, tag.Version)
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return err
	}
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.list.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v1+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	for name, values := range resp.Header {
		for _, value := range values {
			if name == "Etag" {
				tag.Hash = strings.Trim(value, `"`)
				return nil
			}
		}
	}
	return fmt.Errorf("Etag not found")
}

func fetchManifest(ctx context.Context, tag *ImageTag) (*Manifest, error) {
	hash := tag.Hash
	if hash == "" {
		hash = tag.Version
	}
	url := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", getProtocol(tag), tag.Host, tag.Name, hash)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if tag.AuthInfo.Token != "" {
		req.Header.Add("Authorization", "Bearer "+tag.AuthInfo.Token)
	}
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.list.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v1+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		for name, values := range resp.Header {
			for _, value := range values {
				if name == "Www-Authenticate" && !tag.Authed {
					tag.Authed = true
					return doAuth(ctx, tag, value)
				}
			}
		}
		return nil, fmt.Errorf("fetch manifest 状态码: %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func doAuth(ctx context.Context, tag *ImageTag, info string) (*Manifest, error) {
	params := strings.Split(strings.TrimPrefix(info, "Bearer "), ",")
	trimParam := func(input, key string) string {
		input = strings.TrimSpace(input)
		if strings.HasPrefix(input, key) {
			return strings.Trim(strings.TrimPrefix(input, key), `"`)
		}
		return ""
	}
	var realm, service, scope string
	for _, param := range params {
		switch {
		case strings.Contains(param, "realm="):
			realm = trimParam(param, "realm=")
		case strings.Contains(param, "service="):
			service = trimParam(param, "service=")
		case strings.Contains(param, "scope="):
			scope = trimParam(param, "scope=")
		}
	}
	if realm == "" || service == "" || scope == "" {
		return nil, fmt.Errorf("认证信息解析失败: %s", info)
	}
	url := fmt.Sprintf("%s?service=%s&scope=%s", realm, service, scope)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &tag.AuthInfo); err != nil {
		return nil, err
	}
	return fetchManifest(ctx, tag)
}

func downloadAndAddToTar(ctx context.Context, tw *cancelableTarWriter, imageTag *ImageTag, descriptor Descriptor, tarName string, progressCallback func(float64)) error {
	url := fmt.Sprintf("%s://%s/v2/%s/blobs/%s", getProtocol(imageTag), imageTag.Host, imageTag.Name, descriptor.Digest)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	if imageTag.AuthInfo.Token != "" {
		req.Header.Add("Authorization", "Bearer "+imageTag.AuthInfo.Token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 blob 失败: HTTP %d", resp.StatusCode)
	}
	if err := tw.WriteHeader(tarName, descriptor.Size); err != nil {
		return err
	}
	pr := &ProgressReader{
		Reader: resp.Body,
		Size:   descriptor.Size,
		Callback: func(progress float64) {
			if progressCallback != nil {
				progressCallback(progress)
			}
		},
	}
	_, err = io.Copy(tw, pr)
	return err
}
