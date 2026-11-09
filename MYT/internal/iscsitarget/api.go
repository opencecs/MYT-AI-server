package iscsitarget

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"strings"
)

type APIServer struct {
	Manager *Manager
	Target  *Target
	server  *http.Server
}

type DiskCreateReq struct {
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	Type       string `json:"type"`
	StorageDir string `json:"storageDir"`
}

type DiskResponse struct {
	IQN     string `json:"iqn"`
	LUN     int    `json:"lun"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type DiskListResponse struct {
	Disks   []DiskInfo `json:"disks"`
	Success bool       `json:"success"`
}

type DiskInfo struct {
	Name        string `json:"name"`
	LUN         int    `json:"lun"`
	SizeGB      float64 `json:"size_gb"`
	Type        string `json:"type"`
	IQN         string `json:"iqn"`
	StoragePath string `json:"storagePath"`
}

func NewAPIServer(mgr *Manager, target *Target) *APIServer {
	return &APIServer{Manager: mgr, Target: target}
}

const maxBodySize = 1 << 20 // 1MB

func (s *APIServer) Start(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/disk/create", s.handleCreate)
	mux.HandleFunc("/api/v1/disk/delete", s.handleDelete)
	mux.HandleFunc("/api/v1/disk/list", s.handleList)
	mux.HandleFunc("/api/v1/disk/session", s.handleSession)

	s.server = &http.Server{Addr: addr, Handler: mux}
	log.Printf("[iSCSI Target] Management API listening on %s", addr)
	return s.server.ListenAndServe()
}

func (s *APIServer) Stop() {
	if s.server != nil {
		s.server.Close()
	}
}

func (s *APIServer) handleCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req DiskCreateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, DiskResponse{Success: false, Message: "invalid request body"})
		return
	}

	if req.Name == "" || req.Size <= 0 {
		writeJSON(w, DiskResponse{Success: false, Message: "name and size required"})
		return
	}

	if req.Type != "fixed" && req.Type != "thin" {
		writeJSON(w, DiskResponse{Success: false, Message: "type must be 'fixed' or 'thin'"})
		return
	}

	// 使用 CreateAutoID 避免竞态：ID 分配和创建在同一把锁内完成
	isThin := req.Type == "thin"
	lun, err := s.Manager.CreateAutoID(req.Name, req.Size, isThin, req.StorageDir)
	if err != nil {
		writeJSON(w, DiskResponse{Success: false, Message: fmt.Sprintf("create failed: %v", err)})
		return
	}

	// 构建 per-disk IQN
	iqn := diskIQN(req.Name)
	if s.Target != nil {
		s.Target.AddEntry(&TargetEntry{IQN: iqn, LUNs: &SingleLUNProvider{Mgr: s.Manager, LunID: lun.ID}})
	}

	log.Printf("[iSCSI Target] disk created: name=%s lun=%d size=%dGB type=%s iqn=%s", req.Name, lun.ID, req.Size, req.Type, iqn)

	writeJSON(w, DiskResponse{
		IQN:     iqn,
		LUN:     lun.ID,
		Success: true,
		Message: "created",
	})
}

func (s *APIServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		Name  string `json:"name"`
		Force bool   `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, DiskResponse{Success: false, Message: "invalid request body"})
		return
	}

	if req.Name == "" {
		writeJSON(w, DiskResponse{Success: false, Message: "name required"})
		return
	}

	iqn := diskIQN(req.Name)

	// force=true：先关闭该 IQN 的活跃 session
	if req.Force && s.Target != nil {
		s.Target.CloseSessionsByIQN(iqn)
	}

	if err := s.Manager.Delete(req.Name); err != nil {
		writeJSON(w, DiskResponse{Success: false, Message: fmt.Sprintf("delete failed: %v", err)})
		return
	}

	// 从 Target 移除对应 IQN entry
	if s.Target != nil {
		s.Target.RemoveEntry(iqn)
	}

	log.Printf("[iSCSI Target] disk deleted: name=%s force=%v", req.Name, req.Force)
	writeJSON(w, DiskResponse{Success: true, Message: "deleted"})
}

func (s *APIServer) handleList(w http.ResponseWriter, _ *http.Request) {
	luns := s.Manager.List()

	// 构建 LUN ID -> IQN 映射
	lunIQN := map[int]string{}
	if s.Target != nil {
		for _, e := range s.Target.GetEntries() {
			if sp, ok := e.LUNs.(*SingleLUNProvider); ok {
				lunIQN[sp.LunID] = e.IQN
			}
		}
	}

	disks := make([]DiskInfo, 0, len(luns))
	for _, l := range luns {
		// 只暴露盘符，不暴露完整路径
		storagePath := ""
		if l.FilePath != "" {
			storagePath = filepath.VolumeName(l.FilePath)
			if storagePath == "" {
				// 非 Windows：取第一级目录
				parts := strings.SplitN(l.FilePath, "/", 4)
				if len(parts) >= 3 {
					storagePath = "/" + parts[1] + "/" + parts[2]
				}
			}
		}
		iqn := lunIQN[l.ID]
		if iqn == "" {
			iqn = diskIQN(l.Name)
		}
		disks = append(disks, DiskInfo{
			Name:        l.Name,
			LUN:         l.ID,
			SizeGB:      math.Round(float64(l.Backend.Size())/float64(1024*1024*1024)*10) / 10,
			Type:        l.Type(),
			IQN:         iqn,
			StoragePath: storagePath,
		})
	}
	writeJSON(w, DiskListResponse{Disks: disks, Success: true})
}

// diskIQN 根据 disk name 生成 per-disk IQN
func diskIQN(name string) string {
	return "iqn.2024.com.myt:disk." + name
}

func (s *APIServer) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req struct {
		IQN string `json:"iqn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, DiskResponse{Success: false, Message: "invalid request body"})
		return
	}

	if req.IQN == "" {
		writeJSON(w, DiskResponse{Success: false, Message: "iqn required"})
		return
	}

	if s.Target == nil {
		writeJSON(w, DiskResponse{Success: false, Message: "target not running"})
		return
	}

	if err := s.Target.CloseSession(req.IQN); err != nil {
		writeJSON(w, DiskResponse{Success: false, Message: err.Error()})
		return
	}

	log.Printf("[iSCSI Target] session closed: iqn=%s", req.IQN)
	writeJSON(w, DiskResponse{Success: true, Message: "session closed"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
