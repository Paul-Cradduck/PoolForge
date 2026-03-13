package replication

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
)

const (
	syncFile    = "/var/lib/poolforge/sync.json"
	historyFile = "/var/lib/poolforge/sync_history.json"
	maxHistory  = 100
)

type SyncManager struct {
	mu       sync.Mutex
	jobs     []engine.SyncJob
	history  []engine.SyncRun
	pairing  *PairingManager
	progress map[string]*engine.SyncProgress
}

func NewSyncManager(pm *PairingManager) *SyncManager {
	sm := &SyncManager{pairing: pm, progress: make(map[string]*engine.SyncProgress)}
	sm.load()
	return sm
}

func (sm *SyncManager) CreateJob(job engine.SyncJob) error {
	job.ID = generateNodeID()
	job.Enabled = true
	sm.mu.Lock()
	sm.jobs = append(sm.jobs, job)
	sm.mu.Unlock()
	sm.saveJobs()
	return nil
}

func (sm *SyncManager) Jobs() []engine.SyncJob {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return append([]engine.SyncJob{}, sm.jobs...)
}

func (sm *SyncManager) FindJob(id string) *engine.SyncJob {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, j := range sm.jobs {
		if j.ID == id {
			return &j
		}
	}
	return nil
}

func (sm *SyncManager) UpdateJob(id string, updated engine.SyncJob) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for i, j := range sm.jobs {
		if j.ID == id {
			updated.ID = id
			sm.jobs[i] = updated
			sm.saveJobs()
			return nil
		}
	}
	return fmt.Errorf("job %q not found", id)
}

func (sm *SyncManager) DeleteJob(id string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for i, j := range sm.jobs {
		if j.ID == id {
			sm.jobs = append(sm.jobs[:i], sm.jobs[i+1:]...)
			sm.saveJobs()
			return nil
		}
	}
	return fmt.Errorf("job %q not found", id)
}

func (sm *SyncManager) History(jobID string) []engine.SyncRun {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var runs []engine.SyncRun
	for _, r := range sm.history {
		if r.JobID == jobID {
			runs = append(runs, r)
		}
	}
	return runs
}

// RunJob executes a sync job immediately (async — returns immediately).
func (sm *SyncManager) RunJob(jobID string, poolMount string) *engine.SyncRun {
	sm.mu.Lock()
	var job *engine.SyncJob
	for _, j := range sm.jobs {
		if j.ID == jobID {
			job = &j
			break
		}
	}
	// Check if already running
	if p, ok := sm.progress[jobID]; ok && p.Running {
		sm.mu.Unlock()
		return &engine.SyncRun{JobID: jobID, Status: "running"}
	}
	sm.mu.Unlock()
	if job == nil {
		return &engine.SyncRun{JobID: jobID, Status: "failed", Error: "job not found"}
	}

	node := sm.pairing.FindNode(job.RemoteNode)
	if node == nil {
		return &engine.SyncRun{JobID: jobID, Status: "failed", Error: "remote node not found"}
	}

	prog := &engine.SyncProgress{JobID: jobID, Running: true, StartedAt: time.Now().Unix()}
	sm.mu.Lock()
	sm.progress[jobID] = prog
	sm.mu.Unlock()

	sshCmd := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no -p %d", PrivateKeyPath(), node.Port)
	localBase := poolMount + "/"
	remoteMount := poolMount
	if job.RemotePool != "" {
		remoteMount = job.RemotePool
	}
	remoteBase := fmt.Sprintf("root@%s:%s/", node.Host, remoteMount)

	go func() {
		run := engine.SyncRun{JobID: jobID, StartedAt: prog.StartedAt, Status: "running"}
		var totalSent, totalRecv uint64
		var totalFiles int
		var lastErr error

		switch job.Mode {
		case "push":
			sent, files, err := rsyncWithProgress(sshCmd, localBase, remoteBase, prog)
			totalSent, totalFiles, lastErr = sent, files, err
		case "pull":
			recv, files, err := rsyncWithProgress(sshCmd, remoteBase, localBase, prog)
			totalRecv, totalFiles, lastErr = recv, files, err
		case "bidirectional":
			recv, f1, err := rsyncWithProgress(sshCmd, remoteBase, localBase, prog, "--update")
			if err != nil {
				lastErr = err
			}
			sent, f2, err := rsyncWithProgress(sshCmd, localBase, remoteBase, prog, "--update")
			if err != nil {
				lastErr = err
			}
			totalSent, totalRecv, totalFiles = sent, recv, f1+f2
		}

		run.FinishedAt = time.Now().Unix()
		run.BytesSent = totalSent
		run.BytesRecv = totalRecv
		run.FilesChanged = totalFiles
		if lastErr != nil {
			run.Status = "failed"
			run.Error = lastErr.Error()
		} else {
			run.Status = "success"
		}

		sm.mu.Lock()
		sm.history = append(sm.history, run)
		if len(sm.history) > maxHistory {
			sm.history = sm.history[len(sm.history)-maxHistory:]
		}
		prog.Running = false
		prog.Percent = 100
		prog.BytesDone = totalSent + totalRecv
		sm.mu.Unlock()
		sm.saveHistory()
	}()

	return &engine.SyncRun{JobID: jobID, Status: "running", StartedAt: prog.StartedAt}
}

// GetProgress returns current sync progress for a job.
func (sm *SyncManager) GetProgress(jobID string) *engine.SyncProgress {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if p, ok := sm.progress[jobID]; ok {
		cp := *p
		return &cp
	}
	return &engine.SyncProgress{JobID: jobID}
}

// RunScheduled runs all enabled jobs that are due.
func (sm *SyncManager) RunScheduled(poolMounts map[string]string) {
	for _, job := range sm.Jobs() {
		if !job.Enabled || job.Schedule == "" {
			continue
		}
		mount, ok := poolMounts[job.LocalPool]
		if !ok {
			continue
		}
		// Check if due based on last run + schedule interval
		history := sm.History(job.ID)
		if len(history) > 0 {
			last := history[len(history)-1]
			dur, err := time.ParseDuration(job.Schedule)
			if err != nil {
				continue
			}
			if time.Since(time.Unix(last.FinishedAt, 0)) < dur {
				continue
			}
		}
		go sm.RunJob(job.ID, mount)
	}
}

var progressRe = regexp.MustCompile(`(\d[\d,]*)\s+(\d+)%\s+([\d.]+[kMGT]?B/s)`)

func rsyncWithProgress(sshCmd, src, dst string, prog *engine.SyncProgress, extraArgs ...string) (uint64, int, error) {
	args := []string{"-avz", "--delete", "--info=progress2", "-e", sshCmd}
	args = append(args, extraArgs...)
	args = append(args, src, dst)
	cmd := exec.Command("rsync", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, 0, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return 0, 0, err
	}

	var totalBytes uint64
	var totalFiles int
	scanner := bufio.NewScanner(stdout)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		line := scanner.Text()
		if m := progressRe.FindStringSubmatch(line); m != nil {
			if n, err := strconv.ParseUint(strings.ReplaceAll(m[1], ",", ""), 10, 64); err == nil {
				prog.BytesDone = n
			}
			if pct, err := strconv.ParseFloat(m[2], 64); err == nil {
				prog.Percent = pct
			}
			prog.SpeedBps = parseSpeed(m[3])
		}
		if strings.Contains(line, "Total transferred file size:") {
			for _, p := range strings.Fields(line) {
				if n, err := strconv.ParseUint(strings.ReplaceAll(p, ",", ""), 10, 64); err == nil && n > 0 {
					totalBytes = n
					prog.BytesTotal = n
					break
				}
			}
		}
		if strings.Contains(line, "Number of regular files transferred:") {
			for _, p := range strings.Fields(line) {
				if n, err := strconv.Atoi(strings.ReplaceAll(p, ",", "")); err == nil && n > 0 {
					totalFiles = n
					prog.FilesDone = n
					break
				}
			}
		}
		if strings.Contains(line, "Number of files:") && !strings.Contains(line, "transferred") {
			for _, p := range strings.Fields(line) {
				if n, err := strconv.Atoi(strings.ReplaceAll(p, ",", "")); err == nil && n > 0 {
					prog.FilesTotal = n
					break
				}
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		return 0, 0, fmt.Errorf("rsync: %w", err)
	}
	return totalBytes, totalFiles, nil
}

func parseSpeed(s string) uint64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "B/s")
	mult := uint64(1)
	if strings.HasSuffix(s, "k") {
		mult, s = 1024, s[:len(s)-1]
	} else if strings.HasSuffix(s, "M") {
		mult, s = 1024*1024, s[:len(s)-1]
	} else if strings.HasSuffix(s, "G") {
		mult, s = 1024*1024*1024, s[:len(s)-1]
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return uint64(f * float64(mult))
	}
	return 0
}

func (sm *SyncManager) saveJobs() {
	os.MkdirAll(filepath.Dir(syncFile), 0755)
	data, _ := json.MarshalIndent(sm.jobs, "", "  ")
	os.WriteFile(syncFile, data, 0600)
}

func (sm *SyncManager) saveHistory() {
	data, _ := json.MarshalIndent(sm.history, "", "  ")
	os.WriteFile(historyFile, data, 0600)
}

func (sm *SyncManager) load() {
	if data, err := os.ReadFile(syncFile); err == nil {
		json.Unmarshal(data, &sm.jobs)
	}
	if data, err := os.ReadFile(historyFile); err == nil {
		json.Unmarshal(data, &sm.history)
	}
}
