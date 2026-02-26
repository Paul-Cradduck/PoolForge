package monitoring

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
)

const diskLogPath = "/var/lib/poolforge/metrics.log"

// DiskLog appends 1-minute averaged samples to disk and prunes entries older than 30 days.
type DiskLog struct {
	mu   sync.Mutex
	stop chan struct{}
}

func NewDiskLog() *DiskLog {
	return &DiskLog{stop: make(chan struct{})}
}

func (d *DiskLog) Start(c *Collector) {
	d.prune()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-ticker.C:
			snap := c.Latest()
			if snap.Timestamp > 0 {
				d.append(snap)
			}
		}
	}
}

func (d *DiskLog) Stop() {
	select {
	case <-d.stop:
	default:
		close(d.stop)
	}
}

func (d *DiskLog) append(snap engine.MetricsSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	os.MkdirAll(filepath.Dir(diskLogPath), 0755)
	f, err := os.OpenFile(diskLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	line, _ := json.Marshal(snap)
	fmt.Fprintf(f, "%s\n", line)
}

func (d *DiskLog) Query(since time.Time) []engine.MetricsSnapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	f, err := os.Open(diskLogPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	var result []engine.MetricsSnapshot
	scanner := bufio.NewScanner(f)
	sinceUnix := since.Unix()
	for scanner.Scan() {
		var snap engine.MetricsSnapshot
		if json.Unmarshal(scanner.Bytes(), &snap) == nil && snap.Timestamp >= sinceUnix {
			result = append(result, snap)
		}
	}
	return result
}

func (d *DiskLog) prune() {
	d.mu.Lock()
	defer d.mu.Unlock()
	f, err := os.Open(diskLogPath)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
	var keep [][]byte
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var snap engine.MetricsSnapshot
		line := scanner.Bytes()
		if json.Unmarshal(line, &snap) == nil && snap.Timestamp >= cutoff {
			cp := make([]byte, len(line))
			copy(cp, line)
			keep = append(keep, cp)
		}
	}
	f.Close()
	out, err := os.Create(diskLogPath)
	if err != nil {
		return
	}
	defer out.Close()
	for _, line := range keep {
		fmt.Fprintf(out, "%s\n", line)
	}
}
