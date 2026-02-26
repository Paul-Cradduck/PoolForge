package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
)

//go:embed all:static
var staticFS embed.FS

type Server struct {
	engine engine.EngineService
	mux    *http.ServeMux
}

func New(eng engine.EngineService) *Server {
	s := &Server{engine: eng, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/pools", s.handlePools)
	s.mux.HandleFunc("/api/pools/", s.handlePool)
	s.mux.HandleFunc("/api/disks", s.handleDisks)

	sub, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))
}

func (s *Server) handlePools(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		pools, err := s.engine.ListPools(r.Context())
		if err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		jsonResp(w, pools)
	case http.MethodPost:
		var req struct {
			Name       string   `json:"name"`
			Disks      []string `json:"disks"`
			ParityMode string   `json:"parityMode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		pm, err := engine.ParseParityMode(req.ParityMode)
		if err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		pool, err := s.engine.CreatePool(r.Context(), engine.CreatePoolRequest{
			Name: req.Name, Disks: req.Disks, ParityMode: pm,
		})
		if err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		jsonResp(w, pool)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePool(w http.ResponseWriter, r *http.Request) {
	// Parse: /api/pools/{id}[/action[/extra]]
	path := strings.TrimPrefix(r.URL.Path, "/api/pools/")
	parts := strings.SplitN(path, "/", 3)
	poolID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch action {
	case "":
		s.handlePoolCRUD(w, r, poolID)
	case "disks":
		s.handlePoolDisks(w, r, poolID, parts)
	case "fail-disk":
		s.handleFailDisk(w, r, poolID)
	case "replace-disk":
		s.handleReplaceDisk(w, r, poolID)
	case "rebuild":
		s.handleRebuildSSE(w, r, poolID)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (s *Server) handlePoolCRUD(w http.ResponseWriter, r *http.Request, poolID string) {
	switch r.Method {
	case http.MethodGet:
		status, err := s.engine.GetPoolStatus(r.Context(), poolID)
		if err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		jsonResp(w, status)
	case http.MethodDelete:
		if err := s.engine.DeletePool(r.Context(), poolID); err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		jsonResp(w, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePoolDisks(w http.ResponseWriter, r *http.Request, poolID string, parts []string) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Disk string `json:"disk"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		if err := s.engine.AddDisk(r.Context(), poolID, req.Disk); err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		jsonResp(w, map[string]string{"status": "added"})
	case http.MethodDelete:
		device := ""
		if len(parts) > 2 {
			device = "/dev/" + parts[2]
		}
		if device == "" {
			httpError(w, fmt.Errorf("missing device"), http.StatusBadRequest)
			return
		}
		if err := s.engine.RemoveDisk(r.Context(), poolID, device); err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		jsonResp(w, map[string]string{"status": "removed"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFailDisk(w http.ResponseWriter, r *http.Request, poolID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Disk string `json:"disk"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	if err := s.engine.HandleDiskFailure(r.Context(), poolID, req.Disk); err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	jsonResp(w, map[string]string{"status": "failed"})
}

func (s *Server) handleReplaceDisk(w http.ResponseWriter, r *http.Request, poolID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OldDisk string `json:"oldDisk"`
		NewDisk string `json:"newDisk"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	if err := s.engine.ReplaceDisk(r.Context(), poolID, req.OldDisk, req.NewDisk); err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	jsonResp(w, map[string]string{"status": "replaced"})
}

func (s *Server) handleRebuildSSE(w http.ResponseWriter, r *http.Request, poolID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status, err := s.engine.GetPoolStatus(context.Background(), poolID)
			if err != nil {
				fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
				flusher.Flush()
				return
			}
			data, _ := json.Marshal(status)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

			// Stop if no arrays are rebuilding
			rebuilding := false
			for _, a := range status.ArrayStatuses {
				if a.State == engine.ArrayRebuilding {
					rebuilding = true
				}
			}
			if !rebuilding {
				fmt.Fprintf(w, "event: done\ndata: complete\n\n")
				flusher.Flush()
				return
			}
		}
	}
}

func (s *Server) handleDisks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// List block devices from /sys/block
	type blockDev struct {
		Device string `json:"device"`
		SizeGB float64 `json:"sizeGB"`
	}
	entries, err := fs.ReadDir(sysFS, ".")
	if err != nil {
		jsonResp(w, []blockDev{})
		return
	}
	var devs []blockDev
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "nvme") && !strings.HasPrefix(name, "sd") {
			continue
		}
		if strings.Contains(name, "p") { // skip partitions like nvme0n1p1
			continue
		}
		if name == "nvme0n1" { // skip root
			continue
		}
		sizeBytes := readSysBlockSize(name)
		devs = append(devs, blockDev{
			Device: "/dev/" + name,
			SizeGB: float64(sizeBytes) / (1024 * 1024 * 1024),
		})
	}
	jsonResp(w, devs)
}

func jsonResp(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, err error, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
