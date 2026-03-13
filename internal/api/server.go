package api

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
	"github.com/poolforge/poolforge/internal/monitoring"
	"github.com/poolforge/poolforge/internal/replication"
	"github.com/poolforge/poolforge/internal/safety"
	"github.com/poolforge/poolforge/internal/sharing"
)

//go:embed all:static
var staticFS embed.FS

type Server struct {
	engine    engine.EngineService
	mux       *http.ServeMux
	user      string
	pass      string
	alerter   *safety.Alerter
	logs      *safety.LogBuffer
	daemon    *safety.Daemon
	collector *monitoring.Collector
	shares    *sharing.ShareManager
	pairing   *replication.PairingManager
	sync      *replication.SyncManager
	version   string
}

func (s *Server) SetAlerter(a *safety.Alerter)              { s.alerter = a }
func (s *Server) SetLogs(l *safety.LogBuffer)                { s.logs = l }
func (s *Server) SetDaemon(d *safety.Daemon)                 { s.daemon = d }
func (s *Server) SetCollector(c *monitoring.Collector)        { s.collector = c }
func (s *Server) SetShares(sm *sharing.ShareManager)          { s.shares = sm }
func (s *Server) SetPairing(pm *replication.PairingManager)   { s.pairing = pm }
func (s *Server) SetSync(sm *replication.SyncManager)         { s.sync = sm }
func (s *Server) SetVersion(v string)                         { s.version = v }

func New(eng engine.EngineService) *Server {
	return NewWithAuth(eng, "", "")
}

func NewWithAuth(eng engine.EngineService, user, pass string) *Server {
	s := &Server{engine: eng, mux: http.NewServeMux(), user: user, pass: pass}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if s.user != "" && r.URL.Path != "/api/pair/exchange" && r.URL.Path != "/api/internal/pools" && r.URL.Path != "/api/internal/node-info" {
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
	s.mux.HandleFunc("/api/disks/label", s.handleDiskLabel)
	s.mux.HandleFunc("/api/disks/locate", s.handleDiskLocate)
	s.mux.HandleFunc("/api/preview-add", s.handlePreviewAdd)
	s.mux.HandleFunc("/api/preview-create", s.handlePreviewCreate)
	s.mux.HandleFunc("/api/preview-remove", s.handlePreviewRemove)
	s.mux.HandleFunc("/api/alerts", s.handleAlerts)
	s.mux.HandleFunc("/api/safety-status", s.handleSafetyStatus)
	s.mux.HandleFunc("/api/import", s.handleImport)
	s.mux.HandleFunc("/api/logs", s.handleLogs)
	s.mux.HandleFunc("/api/users", s.handleUsers)
	s.mux.HandleFunc("/api/users/", s.handleUserDelete)
	s.mux.HandleFunc("/api/monitoring/live", s.handleMonitoringLive)
	s.mux.HandleFunc("/api/monitoring/history", s.handleMonitoringHistory)
	s.mux.HandleFunc("/api/monitoring/clients", s.handleMonitoringClients)
	s.mux.HandleFunc("/api/monitoring/status", s.handleProtocolStatus)
	s.mux.HandleFunc("/api/protocols/toggle", s.handleProtocolToggle)
	s.mux.HandleFunc("/api/protocols/config", s.handleProtocolConfig)
	s.mux.HandleFunc("/api/admin/settings", s.handleAdminSettings)
	s.mux.HandleFunc("/api/admin/restart", s.handleAdminRestart)
	s.mux.HandleFunc("/api/pair/init", s.handlePairInit)
	s.mux.HandleFunc("/api/pair/exchange", s.handlePairExchange)
	s.mux.HandleFunc("/api/internal/pools", s.handleInternalPools)
	s.mux.HandleFunc("/api/internal/node-info", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, map[string]string{"node_name": readConfVal("POOLFORGE_NODE_NAME", "")})
	})
	s.mux.HandleFunc("/api/pair/join", s.handlePairJoin)
	s.mux.HandleFunc("/api/pair/nodes", s.handlePairNodes)
	s.mux.HandleFunc("/api/pair/nodes/", s.handlePairNodeDelete)
	s.mux.HandleFunc("/api/sync/jobs", s.handleSyncJobs)
	s.mux.HandleFunc("/api/sync/jobs/", s.handleSyncJob)
	s.mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		jsonResp(w, map[string]string{"version": s.version})
	})

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
			Name            string   `json:"name"`
			Disks           []string `json:"disks"`
			ParityMode      string   `json:"parityMode"`
			SnapshotReserve int      `json:"snapshotReserve"`
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
			Name: req.Name, Disks: req.Disks, ParityMode: pm, SnapshotReserve: req.SnapshotReserve,
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
	case "files":
		s.handleFiles(w, r, poolID)
	case "shares":
		s.handleShares(w, r, poolID, parts)
	case "snapshots":
		s.handleSnapshots(w, r, poolID, parts)
	case "start":
		s.handleStartPool(w, r, poolID)
	case "stop":
		s.handleStopPool(w, r, poolID)
	case "autostart":
		s.handleSetAutoStart(w, r, poolID)
	case "assemble":
		s.handleAssemble(w, r, poolID)
	case "activate-lvm":
		s.handleActivateLVM(w, r, poolID)
	case "mount":
		s.handleMount(w, r, poolID)
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
			Disk  string   `json:"disk"`
			Disks []string `json:"disks"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		disks := req.Disks
		if len(disks) == 0 && req.Disk != "" {
			disks = []string{req.Disk}
		}
		if len(disks) == 0 {
			httpError(w, fmt.Errorf("no disks specified"), http.StatusBadRequest)
			return
		}
		// Run add-disk async, processing disks sequentially so reshapes don't overlap
		go func() {
			for _, disk := range disks {
				if err := s.engine.AddDisk(context.Background(), poolID, disk); err != nil {
					s.logError("add disk %s: %v", disk, err)
					return
				}
				s.logInfo("disk %s added to pool", disk)
			}
		}()
		jsonResp(w, map[string]string{"status": "adding", "message": fmt.Sprintf("Adding %d disk(s) — reshape in progress", len(disks))})
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
				// Stay open while pool is still expanding (gap between sequential reshapes)
				if status.Pool.State == engine.PoolExpanding {
					continue
				}
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
		Serial string `json:"serial,omitempty"`
		Slot   string `json:"slot,omitempty"`
		Label  string `json:"label,omitempty"`
	}

	// Build set of in-use devices + labels/serial/slot
	used := map[string]string{}
	diskMeta := map[string]engine.DiskStatusInfo{}
	if pools, err := s.engine.ListPools(r.Context()); err == nil {
		for _, p := range pools {
			if status, err := s.engine.GetPoolStatus(r.Context(), p.ID); err == nil {
				for _, d := range status.DiskStatuses {
					used[d.Device] = p.Name
					diskMeta[d.Device] = d
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
		dm := diskMeta[dev]
		label := dm.Label
		serial := dm.Serial
		if poolName == "" {
			if label == "" {
				label = engine.GetStandaloneDiskLabel(dev)
			}
			if serial == "" {
				serial = engine.GetDiskSerial(dev)
			}
		}
		devs = append(devs, blockDev{
			Device: dev,
			SizeGB: float64(sizeBytes) / (1024 * 1024 * 1024),
			InUse:  poolName != "",
			Pool:   poolName,
			Serial: serial,
			Slot:   dm.EnclosureSlot,
			Label:  label,
		})
	}
	jsonResp(w, devs)
}

func (s *Server) handleDiskLabel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Device string `json:"device"`
		Label  string `json:"label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	if err := s.engine.SetDiskLabel(r.Context(), req.Device, req.Label); err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	jsonResp(w, map[string]bool{"ok": true})
}

func (s *Server) handleDiskLocate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Device string `json:"device"`
		On     bool   `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	action := "locate_off"
	if req.On {
		action = "locate"
	}
	err := exec.Command("ledctl", action+"="+req.Device).Run()
	if err != nil {
		// Fallback: try sg_ses if ledctl not available
		httpError(w, fmt.Errorf("ledctl not available or enclosure does not support LED control"), http.StatusInternalServerError)
		return
	}
	jsonResp(w, map[string]bool{"ok": true})
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
	if r.Method == http.MethodPost {
		var req struct {
			Level   string `json:"level"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.Message != "" {
			if req.Level == "error" {
				s.logError("%s", req.Message)
			} else {
				s.logInfo("%s", req.Message)
			}
		}
		jsonResp(w, map[string]string{"status": "ok"})
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

// --- Phase 5: Shares ---

func (s *Server) handleShares(w http.ResponseWriter, r *http.Request, poolID string, parts []string) {
	shareName := ""
	if len(parts) > 2 {
		shareName = parts[2]
	}

	switch r.Method {
	case http.MethodGet:
		pool, err := s.engine.GetPool(r.Context(), poolID)
		if err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		shares := pool.Shares
		if shares == nil {
			shares = []engine.Share{}
		}
		jsonResp(w, shares)
	case http.MethodPost:
		var share engine.Share
		if err := json.NewDecoder(r.Body).Decode(&share); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		if err := s.engine.CreateShare(r.Context(), poolID, share); err != nil {
			s.logError("create share %q: %v", share.Name, err)
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("share %q created", share.Name)
		jsonResp(w, map[string]string{"status": "created"})
	case http.MethodPut:
		if shareName == "" {
			httpError(w, fmt.Errorf("missing share name"), http.StatusBadRequest)
			return
		}
		var share engine.Share
		if err := json.NewDecoder(r.Body).Decode(&share); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		if err := s.engine.UpdateShare(r.Context(), poolID, shareName, share); err != nil {
			s.logError("update share %q: %v", shareName, err)
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("share %q updated", shareName)
		jsonResp(w, map[string]string{"status": "updated"})
	case http.MethodDelete:
		if shareName == "" {
			httpError(w, fmt.Errorf("missing share name"), http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("force") != "true" {
			// Return size for confirmation
			pool, err := s.engine.GetPool(r.Context(), poolID)
			if err != nil {
				httpError(w, err, http.StatusInternalServerError)
				return
			}
			for _, sh := range pool.Shares {
				if sh.Name == shareName {
					size, _ := sharing.GetShareSize(sh.Path)
					jsonResp(w, map[string]interface{}{"confirm": true, "name": shareName, "sizeBytes": size})
					return
				}
			}
			httpError(w, fmt.Errorf("share %q not found", shareName), http.StatusNotFound)
			return
		}
		if err := s.engine.DeleteShare(r.Context(), poolID, shareName); err != nil {
			s.logError("delete share %q: %v", shareName, err)
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("share %q deleted", shareName)
		jsonResp(w, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// --- Phase 5: Users ---

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		pools, err := s.engine.ListPools(r.Context())
		if err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		var users []engine.NASUser
		for _, ps := range pools {
			p, err := s.engine.GetPool(r.Context(), ps.ID)
			if err != nil {
				continue
			}
			users = append(users, p.Users...)
		}
		if users == nil {
			users = []engine.NASUser{}
		}
		jsonResp(w, users)
	case http.MethodPost:
		var req struct {
			Name         string `json:"name"`
			Password     string `json:"password"`
			PoolID       string `json:"pool_id"`
			GlobalAccess bool   `json:"global_access"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		user, err := s.engine.CreateUser(r.Context(), req.PoolID, req.Name, req.Password, req.GlobalAccess)
		if err != nil {
			s.logError("create user %q: %v", req.Name, err)
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("user %q created", req.Name)
		jsonResp(w, user)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if name == "" {
		httpError(w, fmt.Errorf("missing user name"), http.StatusBadRequest)
		return
	}
	// Find which pool owns this user
	pools, _ := s.engine.ListPools(r.Context())
	for _, ps := range pools {
		p, err := s.engine.GetPool(r.Context(), ps.ID)
		if err != nil {
			continue
		}
		for _, u := range p.Users {
			if u.Name == name {
				if err := s.engine.DeleteUser(r.Context(), ps.ID, name); err != nil {
					httpError(w, err, http.StatusInternalServerError)
					return
				}
				s.logInfo("user %q deleted", name)
				jsonResp(w, map[string]string{"status": "deleted"})
				return
			}
		}
	}
	httpError(w, fmt.Errorf("user %q not found", name), http.StatusNotFound)
}

// --- Phase 5: Monitoring ---

func (s *Server) handleMonitoringLive(w http.ResponseWriter, r *http.Request) {
	if s.collector == nil {
		httpError(w, fmt.Errorf("monitoring not running"), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snap := s.collector.Latest()
			data, _ := json.Marshal(snap)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (s *Server) handleMonitoringHistory(w http.ResponseWriter, r *http.Request) {
	if s.collector == nil {
		jsonResp(w, []engine.MetricsSnapshot{})
		return
	}
	rangeStr := r.URL.Query().Get("range")
	dur, err := time.ParseDuration(rangeStr)
	if err != nil {
		dur = time.Hour
	}
	since := time.Now().Add(-dur)
	history := s.collector.DiskHistory(since)
	if history == nil {
		history = []engine.MetricsSnapshot{}
	}
	jsonResp(w, history)
}

func (s *Server) handleMonitoringClients(w http.ResponseWriter, r *http.Request) {
	if s.collector == nil {
		jsonResp(w, []engine.ClientConnection{})
		return
	}
	clients := s.collector.Clients()
	if clients == nil {
		clients = []engine.ClientConnection{}
	}
	jsonResp(w, clients)
}

func (s *Server) handleProtocolStatus(w http.ResponseWriter, r *http.Request) {
	status := map[string]bool{"smb": false, "nfs": false}
	if s.shares != nil {
		status["smb"] = s.shares.SMBRunning()
		status["nfs"] = s.shares.NFSRunning()
	}
	jsonResp(w, status)
}

func (s *Server) handleProtocolToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Protocol string `json:"protocol"`
		Enabled  bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	if s.shares == nil {
		httpError(w, fmt.Errorf("sharing not initialized"), http.StatusInternalServerError)
		return
	}
	switch req.Protocol {
	case "smb":
		s.shares.ToggleSMB(req.Enabled)
	case "nfs":
		s.shares.ToggleNFS(req.Enabled)
	default:
		httpError(w, fmt.Errorf("unknown protocol: %s", req.Protocol), http.StatusBadRequest)
		return
	}
	jsonResp(w, map[string]bool{"ok": true})
}

func (s *Server) handleProtocolConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		smbConf, _ := os.ReadFile("/etc/samba/poolforge.conf")
		exportsData, _ := os.ReadFile("/etc/exports")
		cfg := map[string]interface{}{
			"smb_running":        s.shares != nil && s.shares.SMBRunning(),
			"nfs_running":        s.shares != nil && s.shares.NFSRunning(),
			"smb_workgroup":      readConfVal("POOLFORGE_SMB_WORKGROUP", "WORKGROUP"),
			"smb_server_name":    readConfVal("POOLFORGE_SMB_SERVER_NAME", "PoolForge NAS"),
			"smb_min_protocol":   readConfVal("POOLFORGE_SMB_MIN_PROTOCOL", ""),
			"smb_max_connections": readConfValInt("POOLFORGE_SMB_MAX_CONNECTIONS", 0),
			"nfs_version":        readConfVal("POOLFORGE_NFS_VERSION", ""),
			"nfs_threads":        readConfValInt("POOLFORGE_NFS_THREADS", 8),
			"nfs_default_clients": readConfVal("POOLFORGE_NFS_DEFAULT_CLIENTS", "*"),
			"nfs_root_squash":    readConfVal("POOLFORGE_NFS_ROOT_SQUASH", "yes") == "yes",
			"smb_config_preview": string(smbConf),
			"nfs_exports_preview": string(exportsData),
		}
		jsonResp(w, cfg)
	case http.MethodPost:
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		for k, v := range req {
			switch k {
			case "smb_workgroup":
				writeConfVal("POOLFORGE_SMB_WORKGROUP", fmt.Sprintf("%v", v))
			case "smb_server_name":
				writeConfVal("POOLFORGE_SMB_SERVER_NAME", fmt.Sprintf("%v", v))
			case "smb_min_protocol":
				writeConfVal("POOLFORGE_SMB_MIN_PROTOCOL", fmt.Sprintf("%v", v))
			case "smb_max_connections":
				writeConfVal("POOLFORGE_SMB_MAX_CONNECTIONS", fmt.Sprintf("%.0f", v))
			case "nfs_version":
				writeConfVal("POOLFORGE_NFS_VERSION", fmt.Sprintf("%v", v))
			case "nfs_threads":
				writeConfVal("POOLFORGE_NFS_THREADS", fmt.Sprintf("%.0f", v))
			case "nfs_default_clients":
				writeConfVal("POOLFORGE_NFS_DEFAULT_CLIENTS", fmt.Sprintf("%v", v))
			case "nfs_root_squash":
				if v == true {
					writeConfVal("POOLFORGE_NFS_ROOT_SQUASH", "yes")
				} else {
					writeConfVal("POOLFORGE_NFS_ROOT_SQUASH", "no")
				}
			}
		}
		// Re-apply share configs to pick up new settings
		if s.shares != nil {
			pools, _ := s.engine.ListPools(r.Context())
			for _, p := range pools {
				pool, _ := s.engine.GetPoolStatus(r.Context(), p.ID)
				if pool != nil {
					var shares []sharing.Share
					for _, sh := range pool.Pool.Shares {
						shares = append(shares, sharing.Share{
							Name: sh.Name, Path: sh.Path, NFSClients: sh.NFSClients,
							SMBPublic: sh.SMBPublic, SMBBrowsable: sh.SMBBrowsable, ReadOnly: sh.ReadOnly,
						})
					}
					s.shares.ApplyConfig(shares)
				}
			}
		}
		jsonResp(w, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func readConfVal(key, fallback string) string {
	data, err := os.ReadFile("/etc/poolforge.conf")
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return fallback
}

func readConfValInt(key string, fallback int) int {
	s := readConfVal(key, "")
	if s == "" {
		return fallback
	}
	v := 0
	fmt.Sscanf(s, "%d", &v)
	if v == 0 {
		return fallback
	}
	return v
}

func writeConfVal(key, value string) {
	data, _ := os.ReadFile("/etc/poolforge.conf")
	lines := strings.Split(string(data), "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+"=") {
			if value == "" || value == "0" {
				lines = append(lines[:i], lines[i+1:]...)
			} else {
				lines[i] = key + "=" + value
			}
			found = true
			break
		}
	}
	if !found && value != "" && value != "0" {
		lines = append(lines, key+"="+value)
	}
	os.WriteFile("/etc/poolforge.conf", []byte(strings.Join(lines, "\n")), 0644)
}

func (s *Server) handleAdminSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := map[string]string{
			"username":  readConfVal("POOLFORGE_USER", "admin"),
			"port":      readConfVal("POOLFORGE_ADDR", "0.0.0.0:8080"),
			"node_name": readConfVal("POOLFORGE_NODE_NAME", ""),
		}
		jsonResp(w, cfg)
	case http.MethodPost:
		var req struct {
			Username    string `json:"username"`
			Password    string `json:"password"`
			NewPassword string `json:"new_password"`
			Port        string `json:"port"`
			NodeName    string `json:"node_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		changed := false
		if req.Username != "" {
			writeConfVal("POOLFORGE_USER", req.Username)
			changed = true
		}
		if req.NewPassword != "" {
			writeConfVal("POOLFORGE_PASS", req.NewPassword)
			changed = true
		}
		if req.Port != "" {
			if !strings.Contains(req.Port, ":") {
				req.Port = "0.0.0.0:" + req.Port
			}
			writeConfVal("POOLFORGE_ADDR", req.Port)
			changed = true
		}
		if req.NodeName != "" {
			writeConfVal("POOLFORGE_NODE_NAME", req.NodeName)
		}
		msg := "Settings saved"
		if changed {
			msg = "Settings saved — restart required for changes to take effect"
		}
		jsonResp(w, map[string]string{"status": msg})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAdminRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonResp(w, map[string]bool{"ok": true})
	go func() {
		time.Sleep(500 * time.Millisecond)
		exec.Command("systemctl", "restart", "poolforge").Run()
	}()
}

// --- Phase 6: Snapshots ---

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request, poolID string, parts []string) {
	snapName := ""
	if len(parts) > 2 {
		sub := strings.SplitN(parts[2], "/", 2)
		snapName = sub[0]
		if len(sub) > 1 {
			parts = append(parts, sub[1])
		}
	}
	switch r.Method {
	case http.MethodGet:
		// GET /api/pools/{id}/snapshots/{name}/browse?path=...
		action := ""
		if len(parts) > 3 {
			action = parts[3]
		}
		if snapName != "" && action == "browse" {
			s.handleSnapshotBrowse(w, r, poolID, snapName)
			return
		}
		snaps, err := s.engine.ListSnapshots(r.Context(), poolID)
		if err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		if snaps == nil {
			snaps = []engine.Snapshot{}
		}
		jsonResp(w, snaps)
	case http.MethodPost:
		if snapName == "schedule" {
			var sched engine.SnapshotSchedule
			if err := json.NewDecoder(r.Body).Decode(&sched); err != nil {
				httpError(w, err, http.StatusBadRequest)
				return
			}
			if err := s.engine.SetSnapshotSchedule(r.Context(), poolID, sched); err != nil {
				httpError(w, err, http.StatusInternalServerError)
				return
			}
			jsonResp(w, map[string]string{"status": "updated"})
			return
		}
		// /api/pools/{id}/snapshots/{name}/mount
		action := ""
		if len(parts) > 3 {
			action = parts[3]
		}
		if snapName != "" && action != "" {
			switch action {
			case "mount":
				mountPath, err := s.engine.MountSnapshot(r.Context(), poolID, snapName)
				if err != nil {
					s.logError("mount snapshot %q: %v", snapName, err)
					httpError(w, err, http.StatusInternalServerError)
					return
				}
				s.logInfo("snapshot %q mounted at %s", snapName, mountPath)
				jsonResp(w, map[string]string{"status": "mounted", "mount_path": mountPath})
			case "unmount":
				if err := s.engine.UnmountSnapshot(r.Context(), poolID, snapName); err != nil {
					s.logError("unmount snapshot %q: %v", snapName, err)
					httpError(w, err, http.StatusInternalServerError)
					return
				}
				s.logInfo("snapshot %q unmounted", snapName)
				jsonResp(w, map[string]string{"status": "unmounted"})
			case "restore":
				if err := s.engine.RestoreSnapshot(r.Context(), poolID, snapName); err != nil {
					httpError(w, err, http.StatusInternalServerError)
					return
				}
				s.logInfo("snapshot %q restored — pool data reverted", snapName)
				jsonResp(w, map[string]string{"status": "restored"})
			case "rename":
				var req struct {
					NewName string `json:"new_name"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.NewName == "" {
					httpError(w, fmt.Errorf("new_name required"), http.StatusBadRequest)
					return
				}
				if err := s.engine.RenameSnapshot(r.Context(), poolID, snapName, req.NewName); err != nil {
					httpError(w, err, http.StatusInternalServerError)
					return
				}
				s.logInfo("snapshot %q renamed to %q", snapName, req.NewName)
				jsonResp(w, map[string]string{"status": "renamed", "new_name": req.NewName})
			case "restore-file":
				s.handleSnapshotRestoreFile(w, r, poolID, snapName)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}
		var req struct {
			Name    string `json:"name"`
			Expires string `json:"expires"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		snap, err := s.engine.CreateSnapshot(r.Context(), poolID, req.Name, req.Expires)
		if err != nil {
			s.logError("create snapshot: %v", err)
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("snapshot %q created", snap.Name)
		jsonResp(w, snap)
	case http.MethodDelete:
		if snapName == "" {
			httpError(w, fmt.Errorf("missing snapshot name"), http.StatusBadRequest)
			return
		}
		if err := s.engine.DeleteSnapshot(r.Context(), poolID, snapName); err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("snapshot %q deleted", snapName)
		jsonResp(w, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request, poolID string) {
	pool, err := s.engine.GetPool(r.Context(), poolID)
	if err != nil {
		httpError(w, err, http.StatusNotFound)
		return
	}
	mountPoint := pool.MountPoint
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		relPath = "/"
	}
	cleaned := filepath.Clean("/" + relPath)
	absPath := filepath.Join(mountPoint, cleaned)
	if !strings.HasPrefix(absPath, mountPoint) {
		httpError(w, fmt.Errorf("invalid path"), http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		info, err := os.Stat(absPath)
		if err != nil {
			httpError(w, fmt.Errorf("path not found"), http.StatusNotFound)
			return
		}
		if !info.IsDir() {
			httpError(w, fmt.Errorf("not a directory"), http.StatusBadRequest)
			return
		}
		entries, err := os.ReadDir(absPath)
		if err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		type fileEntry struct {
			Name    string `json:"name"`
			IsDir   bool   `json:"is_dir"`
			Size    int64  `json:"size"`
			ModTime int64  `json:"mod_time"`
		}
		result := make([]fileEntry, 0, len(entries))
		for _, e := range entries {
			fi, _ := e.Info()
			sz, mt := int64(0), int64(0)
			if fi != nil {
				sz = fi.Size()
				mt = fi.ModTime().Unix()
			}
			result = append(result, fileEntry{Name: e.Name(), IsDir: e.IsDir(), Size: sz, ModTime: mt})
		}
		jsonResp(w, map[string]interface{}{"path": cleaned, "entries": result})

	case http.MethodPost:
		var req struct {
			Action string `json:"action"` // "mkdir" or "mkfile"
			Name   string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		if req.Name == "" || strings.Contains(req.Name, "/") || strings.Contains(req.Name, "..") {
			httpError(w, fmt.Errorf("invalid name"), http.StatusBadRequest)
			return
		}
		target := filepath.Join(absPath, req.Name)
		switch req.Action {
		case "mkdir":
			err = os.MkdirAll(target, 0755)
		case "mkfile":
			var f *os.File
			f, err = os.Create(target)
			if f != nil {
				f.Close()
			}
		default:
			httpError(w, fmt.Errorf("action must be mkdir or mkfile"), http.StatusBadRequest)
			return
		}
		if err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		jsonResp(w, map[string]string{"status": "ok"})

	case http.MethodDelete:
		name := r.URL.Query().Get("name")
		if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
			httpError(w, fmt.Errorf("invalid name"), http.StatusBadRequest)
			return
		}
		target := filepath.Join(absPath, name)
		if !strings.HasPrefix(target, mountPoint) {
			httpError(w, fmt.Errorf("invalid path"), http.StatusBadRequest)
			return
		}
		if err := os.RemoveAll(target); err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		jsonResp(w, map[string]string{"status": "deleted"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSnapshotBrowse(w http.ResponseWriter, r *http.Request, poolID, snapName string) {
	snaps, err := s.engine.ListSnapshots(r.Context(), poolID)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	var mountPath string
	for _, snap := range snaps {
		if snap.Name == snapName {
			mountPath = snap.MountPath
			break
		}
	}
	if mountPath == "" {
		httpError(w, fmt.Errorf("snapshot not mounted"), http.StatusBadRequest)
		return
	}

	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		relPath = "/"
	}
	// Sanitize: prevent path traversal
	cleaned := filepath.Clean("/" + relPath)
	absPath := filepath.Join(mountPath, cleaned)
	if !strings.HasPrefix(absPath, mountPath) {
		httpError(w, fmt.Errorf("invalid path"), http.StatusBadRequest)
		return
	}

	info, err := os.Stat(absPath)
	if err != nil {
		httpError(w, fmt.Errorf("path not found"), http.StatusNotFound)
		return
	}
	if !info.IsDir() {
		httpError(w, fmt.Errorf("not a directory"), http.StatusBadRequest)
		return
	}

	entries, err := os.ReadDir(absPath)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}

	type fileEntry struct {
		Name    string `json:"name"`
		IsDir   bool   `json:"is_dir"`
		Size    int64  `json:"size"`
		ModTime int64  `json:"mod_time"`
	}
	result := make([]fileEntry, 0, len(entries))
	for _, e := range entries {
		fi, _ := e.Info()
		size := int64(0)
		modTime := int64(0)
		if fi != nil {
			size = fi.Size()
			modTime = fi.ModTime().Unix()
		}
		result = append(result, fileEntry{
			Name:    e.Name(),
			IsDir:   e.IsDir(),
			Size:    size,
			ModTime: modTime,
		})
	}
	jsonResp(w, map[string]interface{}{"path": cleaned, "entries": result})
}

func (s *Server) handleSnapshotRestoreFile(w http.ResponseWriter, r *http.Request, poolID, snapName string) {
	var req struct {
		Path string `json:"path"` // relative path within snapshot, e.g. "/testdata/file1.bin"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		httpError(w, fmt.Errorf("path required"), http.StatusBadRequest)
		return
	}

	// Get snapshot mount path
	snaps, err := s.engine.ListSnapshots(r.Context(), poolID)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	var snapMount string
	for _, snap := range snaps {
		if snap.Name == snapName {
			snapMount = snap.MountPath
			break
		}
	}
	if snapMount == "" {
		httpError(w, fmt.Errorf("snapshot not mounted"), http.StatusBadRequest)
		return
	}

	// Get pool mount path
	pool, err := s.engine.GetPool(r.Context(), poolID)
	if err != nil {
		httpError(w, err, http.StatusNotFound)
		return
	}

	cleaned := filepath.Clean("/" + req.Path)
	src := filepath.Join(snapMount, cleaned)
	dst := filepath.Join(pool.MountPoint, cleaned)

	if !strings.HasPrefix(src, snapMount) || !strings.HasPrefix(dst, pool.MountPoint) {
		httpError(w, fmt.Errorf("invalid path"), http.StatusBadRequest)
		return
	}

	// Check source exists
	if _, err := os.Stat(src); err != nil {
		httpError(w, fmt.Errorf("source not found in snapshot"), http.StatusNotFound)
		return
	}

	// Ensure parent dir exists in destination
	os.MkdirAll(filepath.Dir(dst), 0755)

	// Use cp -a to preserve permissions and handle both files and dirs
	out, err := exec.Command("cp", "-a", src, dst).CombinedOutput()
	if err != nil {
		httpError(w, fmt.Errorf("restore failed: %s", string(out)), http.StatusInternalServerError)
		return
	}

	s.logInfo("restored %q from snapshot %q", cleaned, snapName)
	jsonResp(w, map[string]string{"status": "restored", "path": cleaned})
}

// --- Phase 6: Pairing ---

func (s *Server) handlePairInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || s.pairing == nil {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name string `json:"name"`
		Host string `json:"host"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	code, err := s.pairing.InitPairing(req.Name, req.Host)
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	jsonResp(w, map[string]string{"code": code})
}

func (s *Server) handlePairExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || s.pairing == nil {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Code      string `json:"code"`
		Name      string `json:"name"`
		Host      string `json:"host"`
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	pubKey, err := s.pairing.Exchange(req.Code, req.Name, req.Host, req.PublicKey)
	if err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	host := r.Host
	if idx := strings.Index(host, ":"); idx > 0 {
		host = host[:idx]
	}
	jsonResp(w, map[string]string{"public_key": pubKey, "name": host, "host": host})
}

func (s *Server) handlePairJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || s.pairing == nil {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	localHost := r.Host
	if idx := strings.Index(localHost, ":"); idx > 0 {
		localHost = localHost[:idx]
	}
	// Use private IP if available
	if addrs, err := exec.Command("hostname", "-I").Output(); err == nil {
		if fields := strings.Fields(string(addrs)); len(fields) > 0 {
			localHost = fields[0]
		}
	}
	localName := localHost
	localAddr := localHost + ":8080"
	if err := s.pairing.JoinRemote(req.Code, localName, localAddr); err != nil {
		httpError(w, err, http.StatusBadRequest)
		return
	}
	jsonResp(w, map[string]bool{"ok": true})
}

func (s *Server) sshQuery(ctx context.Context, node *engine.PairedNode, apiPath string) ([]byte, error) {
	sshKey := replication.PrivateKeyPath()
	cmd := exec.CommandContext(ctx, "ssh", "-i", sshKey, "-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=5", fmt.Sprintf("-p%d", node.Port),
		fmt.Sprintf("root@%s", node.Host),
		fmt.Sprintf("curl -s http://localhost:8080%s", apiPath))
	return cmd.Output()
}

func (s *Server) handleInternalPools(w http.ResponseWriter, r *http.Request) {
	pools, err := s.engine.ListPools(r.Context())
	if err != nil {
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	if pools == nil {
		pools = []engine.PoolSummary{}
	}
	jsonResp(w, pools)
}

func (s *Server) handlePairNodes(w http.ResponseWriter, r *http.Request) {
	if s.pairing == nil {
		jsonResp(w, []engine.PairedNode{})
		return
	}
	nodes := s.pairing.Nodes()
	if nodes == nil {
		nodes = []engine.PairedNode{}
	}
	jsonResp(w, nodes)
}

func (s *Server) handlePairNodeDelete(w http.ResponseWriter, r *http.Request) {
	if s.pairing == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/pair/nodes/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if action == "pools" && r.Method == http.MethodGet {
		node := s.pairing.FindNode(id)
		if node == nil {
			httpError(w, fmt.Errorf("node not found"), http.StatusNotFound)
			return
		}
		out, err := s.sshQuery(r.Context(), node, "/api/internal/pools")
		if err != nil {
			httpError(w, err, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
		return
	}

	if action == "info" && r.Method == http.MethodGet {
		node := s.pairing.FindNode(id)
		if node == nil {
			httpError(w, fmt.Errorf("node not found"), http.StatusNotFound)
			return
		}
		out, err := s.sshQuery(r.Context(), node, "/api/internal/node-info")
		if err != nil {
			httpError(w, err, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(out)
		return
	}

	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.pairing.RemoveNode(id); err != nil {
		httpError(w, err, http.StatusNotFound)
		return
	}
	jsonResp(w, map[string]string{"status": "unpaired"})
}

// --- Phase 6: Sync ---

func (s *Server) handleSyncJobs(w http.ResponseWriter, r *http.Request) {
	if s.sync == nil {
		jsonResp(w, []engine.SyncJob{})
		return
	}
	switch r.Method {
	case http.MethodGet:
		jobs := s.sync.Jobs()
		if jobs == nil {
			jobs = []engine.SyncJob{}
		}
		jsonResp(w, jobs)
	case http.MethodPost:
		var job engine.SyncJob
		if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		if err := s.sync.CreateJob(job); err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		s.logInfo("sync job %q created", job.Name)
		jsonResp(w, map[string]string{"status": "created"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSyncJob(w http.ResponseWriter, r *http.Request) {
	if s.sync == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/sync/jobs/")
	parts := strings.SplitN(path, "/", 2)
	jobID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "run" && r.Method == http.MethodPost:
		var req struct {
			PoolMount string `json:"pool_mount"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		// Auto-resolve pool mount from job's local_pool if not provided
		if req.PoolMount == "" {
			if job := s.sync.FindJob(jobID); job != nil && job.LocalPool != "" {
				if st, err := s.engine.GetPoolStatus(r.Context(), job.LocalPool); err == nil {
					req.PoolMount = st.Pool.MountPoint
				}
			}
		}
		run := s.sync.RunJob(jobID, req.PoolMount)
		jsonResp(w, run)
	case action == "progress" && r.Method == http.MethodGet:
		jsonResp(w, s.sync.GetProgress(jobID))
	case action == "history" && r.Method == http.MethodGet:
		history := s.sync.History(jobID)
		if history == nil {
			history = []engine.SyncRun{}
		}
		jsonResp(w, history)
	case action == "" && r.Method == http.MethodPut:
		var job engine.SyncJob
		if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
			httpError(w, err, http.StatusBadRequest)
			return
		}
		if err := s.sync.UpdateJob(jobID, job); err != nil {
			httpError(w, err, http.StatusInternalServerError)
			return
		}
		jsonResp(w, map[string]string{"status": "updated"})
	case action == "" && r.Method == http.MethodDelete:
		if err := s.sync.DeleteJob(jobID); err != nil {
			httpError(w, err, http.StatusNotFound)
			return
		}
		jsonResp(w, map[string]string{"status": "deleted"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// Pool start/stop/autostart handlers

func (s *Server) handleStartPool(w http.ResponseWriter, r *http.Request, poolNameOrID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	force := r.URL.Query().Get("force") == "true"
	result, err := s.engine.StartPool(r.Context(), poolNameOrID, force)
	if err != nil {
		s.logError("pool '%s' start failed: %v", poolNameOrID, err)
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
		s.logError("pool '%s' stop failed: %v", poolNameOrID, err)
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

func (s *Server) handleAssemble(w http.ResponseWriter, r *http.Request, poolNameOrID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.engine.AssembleArrays(r.Context(), poolNameOrID); err != nil {
		s.logError("assemble arrays for '%s': %v", poolNameOrID, err)
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	s.logInfo("arrays assembled for pool '%s'", poolNameOrID)
	jsonResp(w, map[string]string{"status": "ok", "message": "RAID arrays assembled"})
}

func (s *Server) handleActivateLVM(w http.ResponseWriter, r *http.Request, poolNameOrID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.engine.ActivateLVM(r.Context(), poolNameOrID); err != nil {
		s.logError("activate LVM for '%s': %v", poolNameOrID, err)
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	s.logInfo("LVM activated for pool '%s'", poolNameOrID)
	jsonResp(w, map[string]string{"status": "ok", "message": "LVM volume group activated"})
}

func (s *Server) handleMount(w http.ResponseWriter, r *http.Request, poolNameOrID string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.engine.MountPool(r.Context(), poolNameOrID); err != nil {
		s.logError("mount pool '%s': %v", poolNameOrID, err)
		httpError(w, err, http.StatusInternalServerError)
		return
	}
	s.logInfo("pool '%s' mounted", poolNameOrID)
	jsonResp(w, map[string]string{"status": "ok", "message": "Filesystem mounted"})
}
