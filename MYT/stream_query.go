package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// getActiveStreams 获取当前活跃推流列表
func (r *rtmpServer) getActiveStreams() []StreamInfo {
	if r.useSrs {
		return r.getActiveStreamsFromSrs()
	}
	resp, err := http.Get("http://127.0.0.1:8084/api/stat/all_group")
	if err != nil {
		return []StreamInfo{}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []StreamInfo{}
	}
	var stat LalStatResponse
	if err := json.Unmarshal(body, &stat); err != nil {
		return []StreamInfo{}
	}
	var streams []StreamInfo
	for _, group := range stat.Data.Groups {
		if group.PubSession != nil {
			streams = append(streams, StreamInfo{
				StreamName:   group.StreamName,
				AppName:      group.AppName,
				PublisherIP:  strings.Split(group.PubSession.RemoteAddr, ":")[0],
				StartTime:    group.PubSession.StartTime,
				VideoBitrate: group.PubSession.BitrateKbits * 1024,
			})
		}
	}
	return streams
}

func (r *rtmpServer) getActiveStreamsFromSrs() []StreamInfo {
	resp, err := http.Get("http://127.0.0.1:1985/api/v1/streams")
	if err != nil {
		return []StreamInfo{}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []StreamInfo{}
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return []StreamInfo{}
	}
	streamsRaw, ok := payload["streams"].([]interface{})
	if !ok {
		return []StreamInfo{}
	}
	var streams []StreamInfo
	for _, item := range streamsRaw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		streamName := streamReadString(m, "stream")
		if streamName == "" {
			streamName = streamReadString(m, "name")
		}
		appName := streamReadString(m, "app")
		publisherIP := ""
		startTime := ""
		videoBitrate := 0
		audioBitrate := 0
		if publish := streamReadMap(m, "publish"); publish != nil {
			publisherIP = streamReadString(publish, "ip")
			startTime = streamReadString(publish, "start_time")
		}
		if kbps := streamReadMap(m, "kbps"); kbps != nil {
			videoBitrate = int(streamReadFloat(kbps, "video") * 1024)
			audioBitrate = int(streamReadFloat(kbps, "audio") * 1024)
		}
		if streamName != "" {
			streams = append(streams, StreamInfo{
				StreamName:   streamName,
				AppName:      appName,
				PublisherIP:  publisherIP,
				StartTime:    startTime,
				VideoBitrate: videoBitrate,
				AudioBitrate: audioBitrate,
			})
		}
	}
	return streams
}

// getStreamPullers 获取指定流的拉流客户端
func (r *rtmpServer) getStreamPullers(streamName string) []PullerInfo {
	if r.useSrs {
		return r.getStreamPullersFromSrs(streamName)
	}
	resp, err := http.Get("http://127.0.0.1:8084/api/stat/all_group")
	if err != nil {
		return []PullerInfo{}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []PullerInfo{}
	}
	var stat LalStatResponse
	if err := json.Unmarshal(body, &stat); err != nil {
		return []PullerInfo{}
	}
	var pullers []PullerInfo
	for _, group := range stat.Data.Groups {
		if group.StreamName == streamName {
			for _, sub := range group.SubSessions {
				ip := strings.Split(sub.RemoteAddr, ":")[0]
				pullers = append(pullers, PullerInfo{
					DeviceIP:   ip,
					Type:       sub.Type,
					StreamName: sub.StreamName,
				})
			}
		}
	}
	return pullers
}

func (r *rtmpServer) getStreamPullersFromSrs(streamName string) []PullerInfo {
	resp, err := http.Get("http://127.0.0.1:1985/api/v1/streams")
	if err != nil {
		return []PullerInfo{}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []PullerInfo{}
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return []PullerInfo{}
	}
	streamsRaw, ok := payload["streams"].([]interface{})
	if !ok {
		return []PullerInfo{}
	}
	streamID := ""
	for _, item := range streamsRaw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := streamReadString(m, "stream")
		if name == "" {
			name = streamReadString(m, "name")
		}
		if name == streamName {
			streamID = streamReadString(m, "id")
			if streamID == "" {
				streamID = streamReadString(m, "stream_id")
			}
			break
		}
	}
	if streamID == "" {
		return []PullerInfo{}
	}
	clientsResp, err := http.Get(fmt.Sprintf("http://127.0.0.1:1985/api/v1/streams/%s/clients", streamID))
	if err != nil {
		return []PullerInfo{}
	}
	defer clientsResp.Body.Close()
	clientsBody, err := io.ReadAll(clientsResp.Body)
	if err != nil {
		return []PullerInfo{}
	}
	var clientsPayload map[string]interface{}
	if err := json.Unmarshal(clientsBody, &clientsPayload); err != nil {
		return []PullerInfo{}
	}
	clientsRaw, ok := clientsPayload["clients"].([]interface{})
	if !ok {
		return []PullerInfo{}
	}
	var pullers []PullerInfo
	for _, item := range clientsRaw {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		clientType := streamReadString(m, "type")
		if clientType == "publish" {
			continue
		}
		ip := streamReadString(m, "ip")
		if ip == "" {
			ip = streamReadString(m, "client_ip")
		}
		pullers = append(pullers, PullerInfo{
			DeviceIP:   ip,
			Type:       clientType,
			StreamName: streamName,
		})
	}
	return pullers
}
