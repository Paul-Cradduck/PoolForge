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
	"github.com/poolforge/poolforge/internal/safety"
)

//go:embed all:static
var staticFS embed.FS

type Server struct {
	engine  engine.EngineService
	mux     *http.ServeMux
	user    string
	pass    string
	alerter *safety.Alerter
	logs    *safety.LogBuffer
	daemon  *safety.Daemon
}

func (s *Server) SetAlerter(a *safety.Alerter) { s.alerter = a }
func (s *Server) SetLogs(l *safety.LogBuffer)   { s.logs = l }
func (s *Server) SetDaemon(d *safety.Daemon)     { s.daemon = d }

func New(eng engine.EngineService) *Server {
	return NewWithAuth(eng, "", "")
}

func NewWithAuth(eng engine.EngineService, user, pass string) *Server {
	s := &Server{engine: eng, mux: http.NewServeMux(), user: user, pass: pass}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.user != "" {
		u, p, ok := r.BasicAuth()
		if !ok || u != s.user || p != s.pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="PoolForge"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/pools", s.handlePools)
	s.mux.HandleFunc("/api/pools/", s.handlePool)
	s.mux.HandleFunc("/api/disks", s.handleDisks)
	s.mux.HandleFunc("/api/preview-add", s.handlePreviewAdd)
	s.mux.HandleFunc("/api/preview-create", s.handlePreviewCreate)
	s.mux.HandleFunc("/api/preview-remove", s.handlePreviewRemove)
	s.mux.HandleFunc("/api/alerts", s.handleAlerts)
	s.mux.HandleFunc("/api/safety-status", s.handleSafetyStatus)
	s.mux.HandleFunc("/api/import", s.handleImport)
	s.mux.HandleFunc("/api/logs", s.handleLogs)

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
		if pools == nil {
			pools = []engine.PoolSummary{}
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
			s.logError("create pool %q: %v", req.Name, err)
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("pool %q created with %d disks", req.Name, len(req.Disks))
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
	case "start":
		s.handleStartPool(w, r, poolID)
	case "stop":
		s.handleStopPool(w, r, poolID)
	case "autostart":
		s.handleSetAutoStart(w, r, poolID)
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
			s.logError("delete pool %s: %v", poolID, err)
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("pool %s deleted", poolID)
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
			s.logError("add disk %s: %v", req.Disk, err)
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("disk %s added to pool", req.Disk)
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
			s.logError("remove disk %s: %v", device, err)
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("disk %s removed from pool", device)
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
		s.logError("fail disk %s: %v", req.Disk, err)
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	s.logInfo("disk %s marked as failed", req.Disk)
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
		s.logError("replace disk %s → %s: %v", req.OldDisk, req.NewDisk, err)
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	s.logInfo("disk %s replaced with %s — rebuilding", req.OldDisk, req.NewDisk)
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

	type rebuildEvent struct {
		Pool     *engine.PoolStatus    `json:"pool"`
		Progress map[string]float64    `json:"progress"`
	}

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

			progress := map[string]float64{}
			rebuilding := false
			for _, a := range status.ArrayStatuses {
				if a.State == engine.ArrayRebuilding {
					rebuilding = true
					if rp, err := s.engine.GetRebuildProgress(context.Background(), poolID, a.Device); err == nil {
						progress[a.Device] = rp.PercentComplete
					}
				}
			}

			evt := rebuildEvent{Pool: status, Progress: progress}
			data, _ := json.Marshal(evt)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

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
	type blockDev struct {
		Device string `json:"device"`
		SizeGB float64 `json:"sizeGB"`
		InUse  bool   `json:"inUse"`
		Pool   string `json:"pool,omitempty"`
	}

	// Build set of in-use devices
	used := map[string]string{}
	if pools, err := s.engine.ListPools(r.Context()); err == nil {
		for _, p := range pools {
			if status, err := s.engine.GetPoolStatus(r.Context(), p.ID); err == nil {
				for _, d := range status.DiskStatuses {
					used[d.Device] = p.Name
				}
			}
		}
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
		if strings.Contains(name, "p") {
			continue
		}
		if name == "nvme0n1" {
			continue
		}
		dev := "/dev/" + name
		sizeBytes := readSysBlockSize(name)
		poolName := used[dev]
		devs = append(devs, blockDev{
			Device: dev,
			SizeGB: float64(sizeBytes) / (1024 * 1024 * 1024),
			InUse:  poolName != "",
			Pool:   poolName,
		})
	}
	jsonResp(w, devs)
}

func (s *Server) handlePreviewCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Disks      []string `json:"disks"`
		ParityMode string   `json:"parityMode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	pm, _ := engine.ParseParityMode(req.ParityMode)

	// Get sizes for selected disks
	type diskSize struct {
		Device string
		Size   uint64
	}
	var selected []diskSize
	for _, dev := range req.Disks {
		sz := readSysBlockSize(strings.TrimPrefix(dev, "/dev/"))
		if sz > 2*1024*1024 {
			sz -= 2 * 1024 * 1024
		}
		selected = append(selected, diskSize{dev, sz})
	}

	// Compute tiers and capacity
	caps := make([]uint64, len(selected))
	for i, d := range selected {
		caps[i] = d.Size
	}
	tiers := engine.ComputeCapacityTiers(caps)
	var usable uint64
	for _, t := range tiers {
		level, err := engine.SelectRAIDLevel(pm, t.EligibleDiskCount)
		if err != nil {
			continue
		}
		var dataDiskCount int
		switch level {
		case 1:
			dataDiskCount = 1
		case 5:
			dataDiskCount = t.EligibleDiskCount - 1
		case 6:
			dataDiskCount = t.EligibleDiskCount - 2
		}
		usable += t.SliceSizeBytes * uint64(dataDiskCount)
	}

	// Find minimum selected disk size (the Tier 0 slice boundary)
	var minSelected uint64
	for _, d := range selected {
		if minSelected == 0 || d.Size < minSelected {
			minSelected = d.Size
		}
	}

	// Check for excluded disks that are smaller than the smallest selected
	var warnings []string
	allDisks, _ := s.engine.ListPools(r.Context()) // just to get disk list
	_ = allDisks
	entries, _ := fs.ReadDir(sysFS, ".")
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "nvme") && !strings.HasPrefix(name, "sd") || strings.Contains(name, "p") || name == "nvme0n1" {
			continue
		}
		dev := "/dev/" + name
		isSelected := false
		for _, s := range req.Disks {
			if s == dev {
				isSelected = true
			}
		}
		if isSelected {
			continue
		}
		sz := readSysBlockSize(name)
		if sz > 2*1024*1024 {
			sz -= 2 * 1024 * 1024
		}
		if sz > 0 && sz < minSelected {
			warnings = append(warnings, fmt.Sprintf("%s (%s) is smaller than the smallest selected disk — it can NEVER be added to this pool later", dev, formatBytesShort(sz)))
		}
	}

	type preview struct {
		UsableCapacity uint64   `json:"usableCapacity"`
		Tiers          int      `json:"tiers"`
		MinDiskSize    uint64   `json:"minDiskSize"`
		Warnings       []string `json:"warnings"`
	}
	jsonResp(w, preview{
		UsableCapacity: usable,
		Tiers:          len(tiers),
		MinDiskSize:    minSelected,
		Warnings:       warnings,
	})
}

func formatBytesShort(b uint64) string {
	if b >= 1e9 {
		return fmt.Sprintf("%.1fGB", float64(b)/1e9)
	}
	return fmt.Sprintf("%.0fMB", float64(b)/1e6)
}

func (s *Server) handlePreviewRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		PoolID string `json:"poolID"`
		Disk   string `json:"disk"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	status, err := s.engine.GetPoolStatus(r.Context(), req.PoolID)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	// Current capacity
	var currentCap uint64
	for _, a := range status.ArrayStatuses {
		currentCap += a.CapacityBytes
	}

	// Compute projected capacity without this disk
	var remainingCaps []uint64
	for _, d := range status.Pool.Disks {
		if d.Device != req.Disk && d.State != engine.DiskFailed {
			remainingCaps = append(remainingCaps, d.CapacityBytes)
		}
	}

	safe := len(remainingCaps) >= 2
	var projectedCap uint64
	var warnings []string

	if safe {
		tiers := engine.ComputeCapacityTiers(remainingCaps)
		for _, t := range tiers {
			level, err := engine.SelectRAIDLevel(status.Pool.ParityMode, t.EligibleDiskCount)
			if err != nil {
				continue
			}
			var data int
			switch level {
			case 1:
				data = 1
			case 5:
				data = t.EligibleDiskCount - 1
			case 6:
				data = t.EligibleDiskCount - 2
			}
			projectedCap += t.SliceSizeBytes * uint64(data)
		}
		if projectedCap < status.UsedCapacityBytes {
			safe = false
			warnings = append(warnings, "not enough space — used data exceeds projected capacity")
		}
	} else {
		warnings = append(warnings, "pool requires at least 2 disks")
	}

	// Check if any array would drop below minimum members
	for _, d := range status.Pool.Disks {
		if d.Device != req.Disk {
			continue
		}
		for _, a := range status.ArrayStatuses {
			members := len(a.Members)
			for _, sl := range d.Slices {
				if sl.TierIndex == a.TierIndex {
					members--
				}
			}
			if members < 2 {
				safe = false
				warnings = append(warnings, fmt.Sprintf("%s would have only %d member — array destroyed", a.Device, members))
			}
		}
	}

	type preview struct {
		Safe              bool     `json:"safe"`
		CurrentCapacity   uint64   `json:"currentCapacity"`
		ProjectedCapacity uint64   `json:"projectedCapacity"`
		LossBytes         uint64   `json:"lossBytes"`
		Warnings          []string `json:"warnings"`
	}
	loss := uint64(0)
	if currentCap > projectedCap {
		loss = currentCap - projectedCap
	}
	jsonResp(w, preview{
		Safe: safe, CurrentCapacity: currentCap, ProjectedCapacity: projectedCap,
		LossBytes: loss, Warnings: warnings,
	})
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	result, err := s.engine.ImportPool()
	if err != nil {
		s.logError("import pool: %v", err)
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	s.logInfo("pool imported: %s (%d arrays, %d disks remapped)", result.PoolName, result.ArraysFound, result.DisksRemapped)
	jsonResp(w, result)
}

func (s *Server) handleSafetyStatus(w http.ResponseWriter, r *http.Request) {
	if s.daemon == nil {
		httpError(w, fmt.Errorf("safety daemon not running"), http.StatusServiceUnavailable)
		return
	}
	jsonResp(w, s.daemon.Status())
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.alerter == nil {
		jsonResp(w, []safety.Alert{})
		return
	}
	h := s.alerter.History()
	if h == nil {
		h = []safety.Alert{}
	}
	jsonResp(w, h)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		if s.logs != nil {
			s.logs.Clear()
		}
		jsonResp(w, map[string]string{"status": "cleared"})
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.logs == nil {
		jsonResp(w, []safety.LogEntry{})
		return
	}
	jsonResp(w, s.logs.Entries())
}

func (s *Server) handlePreviewAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		PoolID string `json:"poolID"`
		Disk   string `json:"disk"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	status, err := s.engine.GetPoolStatus(r.Context(), req.PoolID)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	// Current capacity from arrays
	var currentCap uint64
	for _, a := range status.ArrayStatuses {
		currentCap += a.CapacityBytes
	}

	// Build future disk list
	caps := make([]uint64, 0, len(status.Pool.Disks)+1)
	for _, d := range status.Pool.Disks {
		if d.State != engine.DiskFailed {
			caps = append(caps, d.CapacityBytes)
		}
	}
	// Get new disk size
	newSize := readSysBlockSize(strings.TrimPrefix(req.Disk, "/dev/"))
	if newSize > 2*1024*1024 {
		newSize -= 2 * 1024 * 1024 // GPT overhead
	}
	caps = append(caps, newSize)

	// Compute projected tiers
	tiers := engine.ComputeCapacityTiers(caps)
	var projectedCap uint64
	for _, t := range tiers {
		level, err := engine.SelectRAIDLevel(status.Pool.ParityMode, t.EligibleDiskCount)
		if err != nil {
			continue
		}
		var dataDiskCount int
		switch level {
		case 1:
			dataDiskCount = 1
		case 5:
			dataDiskCount = t.EligibleDiskCount - 1
		case 6:
			dataDiskCount = t.EligibleDiskCount - 2
		}
		projectedCap += t.SliceSizeBytes * uint64(dataDiskCount)
	}

	type preview struct {
		CurrentCapacity   uint64             `json:"currentCapacity"`
		ProjectedCapacity uint64             `json:"projectedCapacity"`
		GainBytes         uint64             `json:"gainBytes"`
		NewDiskSize       uint64             `json:"newDiskSize"`
		CurrentTiers      int                `json:"currentTiers"`
		ProjectedTiers    int                `json:"projectedTiers"`
	}
	jsonResp(w, preview{
		CurrentCapacity:   currentCap,
		ProjectedCapacity: projectedCap,
		GainBytes:         projectedCap - currentCap,
		NewDiskSize:       newSize,
		CurrentTiers:      len(status.Pool.CapacityTiers),
		ProjectedTiers:    len(tiers),
	})
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

func (s *Server) logError(format string, args ...interface{}) {
	if s.logs != nil {
		s.logs.Error(format, args...)
	}
}

func (s *Server) logInfo(format string, args ...interface{}) {
	if s.logs != nil {
		s.logs.Info(format, args...)
	}
}

// Phase 5: Pool start/stop/autostart handlers

func (s *Server) handleStartPool(w http.ResponseWriter, r *http.Request, poolNameOrID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	force := r.URL.Query().Get("force") == "true"
	result, err := s.engine.StartPool(r.Context(), poolNameOrID, force)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "not found") {
			httpError(w, err, http.StatusNotFound)
		} else if strings.Contains(msg, "already running") {
			httpError(w, err, http.StatusConflict)
		} else {
			httpError(w, err, http.StatusInternalServerError)
		}
		return
	}
	// If warnings with no array results, it's a pending confirmation
	if len(result.Warnings) > 0 && len(result.ArrayResults) == 0 {
		resp := map[string]interface{}{
			"pool_name": result.PoolName,
			"status":    "pending_confirmation",
			"warnings":  result.Warnings,
		}
		jsonResp(w, resp)
		return
	}
	var arrayResults []map[string]interface{}
	for _, ar := range result.ArrayResults {
		entry := map[string]interface{}{
			"device":     ar.Device,
			"tier_index": ar.TierIndex,
			"state":      string(ar.State),
		}
		if len(ar.ReAddedParts) > 0 {
			entry["readded_parts"] = ar.ReAddedParts
		}
		if len(ar.FullRebuilds) > 0 {
			entry["full_rebuilds"] = ar.FullRebuilds
		}
		arrayResults = append(arrayResults, entry)
	}
	resp := map[string]interface{}{
		"pool_name":     result.PoolName,
		"status":        "running",
		"mount_point":   result.MountPoint,
		"array_results": arrayResults,
	}
	if len(result.Warnings) > 0 {
		resp["warnings"] = result.Warnings
	}
	s.logInfo("pool '%s' started", poolNameOrID)
	jsonResp(w, resp)
}

func (s *Server) handleStopPool(w http.ResponseWriter, r *http.Request, poolNameOrID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	err := s.engine.StopPool(r.Context(), poolNameOrID)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "not found") {
			httpError(w, err, http.StatusNotFound)
		} else if strings.Contains(msg, "not running") {
			httpError(w, err, http.StatusConflict)
		} else {
			httpError(w, err, http.StatusInternalServerError)
		}
		return
	}
	s.logInfo("pool '%s' stopped", poolNameOrID)
	jsonResp(w, map[string]string{
		"pool_name": poolNameOrID,
		"status":    "safe_to_power_down",
		"message":   "It is now SAFE to power down the external enclosure.",
	})
}

func (s *Server) handleSetAutoStart(w http.ResponseWriter, r *http.Request, poolNameOrID string) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AutoStart *bool `json:"auto_start"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.AutoStart == nil {
		httpError(w, fmt.Errorf("request body must contain 'auto_start' boolean"), http.StatusBadRequest)
		return
	}
	err := s.engine.SetAutoStart(r.Context(), poolNameOrID, *req.AutoStart)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "not found") {
			httpError(w, err, http.StatusNotFound)
		} else {
			httpError(w, err, http.StatusInternalServerError)
		}
		return
	}
	msg := fmt.Sprintf("Auto-start set to %v for pool '%s'", *req.AutoStart, poolNameOrID)
	if !*req.AutoStart {
		msg = fmt.Sprintf("Auto-start disabled for pool '%s'. Manual start required.", poolNameOrID)
	}
	jsonResp(w, map[string]interface{}{
		"pool_name":  poolNameOrID,
		"auto_start": *req.AutoStart,
		"message":    msg,
	})
}
