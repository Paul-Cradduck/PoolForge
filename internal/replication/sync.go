package replication

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	mu      sync.Mutex
	jobs    []engine.SyncJob
	history []engine.SyncRun
	pairing *PairingManager
}

func NewSyncManager(pm *PairingManager) *SyncManager {
	sm := &SyncManager{pairing: pm}
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

// RunJob executes a sync job immediately.
func (sm *SyncManager) RunJob(jobID string, poolMount string) *engine.SyncRun {
	sm.mu.Lock()
	var job *engine.SyncJob
	for _, j := range sm.jobs {
		if j.ID == jobID {
			job = &j
			break
		}
	}
	sm.mu.Unlock()
	if job == nil {
		return &engine.SyncRun{JobID: jobID, Status: "failed", Error: "job not found"}
	}

	node := sm.pairing.FindNode(job.RemoteNode)
	if node == nil {
		return &engine.SyncRun{JobID: jobID, Status: "failed", Error: "remote node not found"}
	}

	run := engine.SyncRun{JobID: jobID, StartedAt: time.Now().Unix(), Status: "running"}

	sshCmd := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no -p %d", PrivateKeyPath(), node.Port)
	localBase := poolMount + "/"
	remoteBase := fmt.Sprintf("root@%s:%s/", node.Host, poolMount)

	var totalSent, totalRecv uint64
	var totalFiles int
	var lastErr error

	switch job.Mode {
	case "push":
		sent, files, err := rsyncExec(sshCmd, localBase, remoteBase)
		totalSent, totalFiles, lastErr = sent, files, err
	case "pull":
		recv, files, err := rsyncExec(sshCmd, remoteBase, localBase)
		totalRecv, totalFiles, lastErr = recv, files, err
	case "bidirectional":
		recv, f1, err := rsyncExec(sshCmd+" --update", remoteBase, localBase)
		if err != nil {
			lastErr = err
		}
		sent, f2, err := rsyncExec(sshCmd+" --update", localBase, remoteBase)
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
	sm.mu.Unlock()
	sm.saveHistory()
	return &run
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

func rsyncExec(sshCmd, src, dst string) (uint64, int, error) {
	args := []string{"-avz", "--delete", "-e", sshCmd, src, dst}
	out, err := exec.Command("rsync", args...).CombinedOutput()
	if err != nil {
		return 0, 0, fmt.Errorf("rsync: %s: %w", strings.TrimSpace(string(out)), err)
	}
	// Parse rsync output for stats
	var bytes uint64
	var files int
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Total transferred file size:") {
			parts := strings.Fields(line)
			for _, p := range parts {
				if n, err := strconv.ParseUint(strings.ReplaceAll(p, ",", ""), 10, 64); err == nil && n > 0 {
					bytes = n
					break
				}
			}
		}
		if strings.Contains(line, "Number of regular files transferred:") {
			parts := strings.Fields(line)
			for _, p := range parts {
				if n, err := strconv.Atoi(strings.ReplaceAll(p, ",", "")); err == nil && n > 0 {
					files = n
					break
				}
			}
		}
	}
	return bytes, files, nil
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
