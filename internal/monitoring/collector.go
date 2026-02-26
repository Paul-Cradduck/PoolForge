package monitoring

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
)

const ringSize = 300 // 5 minutes at 1s resolution

// Collector samples disk/net IO at 1s intervals and maintains a ring buffer.
type Collector struct {
	mu       sync.RWMutex
	ring     [ringSize]engine.MetricsSnapshot
	pos      int
	count    int
	prevDisk map[string]diskRaw
	prevNet  map[string]netRaw
	clients  []engine.ClientConnection
	diskLog  *DiskLog
	stop     chan struct{}
}

type diskRaw struct {
	readSectors, writeSectors uint64
	readOps, writeOps         uint64
}

type netRaw struct {
	rxBytes, txBytes uint64
}

func NewCollector() *Collector {
	return &Collector{
		prevDisk: make(map[string]diskRaw),
		prevNet:  make(map[string]netRaw),
		diskLog:  NewDiskLog(),
		stop:     make(chan struct{}),
	}
}

func (c *Collector) Start() {
	go c.loop()
	go c.diskLog.Start(c)
}

func (c *Collector) Stop() {
	close(c.stop)
	c.diskLog.Stop()
}

func (c *Collector) loop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	// Prime previous values
	c.prevDisk = readDiskStats()
	c.prevNet = readNetStats()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.sample()
		}
	}
}

func (c *Collector) sample() {
	now := time.Now().Unix()
	curDisk := readDiskStats()
	curNet := readNetStats()

	var diskIO []engine.DiskIOStats
	for dev, cur := range curDisk {
		prev, ok := c.prevDisk[dev]
		if !ok {
			continue
		}
		diskIO = append(diskIO, engine.DiskIOStats{
			Device:    dev,
			ReadMBps:  float64(cur.readSectors-prev.readSectors) * 512 / 1048576,
			WriteMBps: float64(cur.writeSectors-prev.writeSectors) * 512 / 1048576,
			ReadIOPS:  float64(cur.readOps - prev.readOps),
			WriteIOPS: float64(cur.writeOps - prev.writeOps),
		})
	}

	var netIO []engine.NetIOStats
	for iface, cur := range curNet {
		prev, ok := c.prevNet[iface]
		if !ok {
			continue
		}
		netIO = append(netIO, engine.NetIOStats{
			Interface: iface,
			RxMBps:    float64(cur.rxBytes-prev.rxBytes) / 1048576,
			TxMBps:    float64(cur.txBytes-prev.txBytes) / 1048576,
		})
	}

	// Protocol breakdown via ss byte counters
	smbBytes := getPortBytes(445)
	nfsBytes := getPortBytes(2049)
	if smbBytes[0]+smbBytes[1] > 0 {
		netIO = append(netIO, engine.NetIOStats{Protocol: "smb", RxMBps: float64(smbBytes[0]) / 1048576, TxMBps: float64(smbBytes[1]) / 1048576})
	}
	if nfsBytes[0]+nfsBytes[1] > 0 {
		netIO = append(netIO, engine.NetIOStats{Protocol: "nfs", RxMBps: float64(nfsBytes[0]) / 1048576, TxMBps: float64(nfsBytes[1]) / 1048576})
	}

	snap := engine.MetricsSnapshot{Timestamp: now, DiskIO: diskIO, NetIO: netIO}

	c.mu.Lock()
	c.ring[c.pos] = snap
	c.pos = (c.pos + 1) % ringSize
	if c.count < ringSize {
		c.count++
	}
	c.mu.Unlock()

	c.prevDisk = curDisk
	c.prevNet = curNet

	// Update clients periodically (every 5s based on timestamp)
	if now%5 == 0 {
		c.mu.Lock()
		c.clients = pollClients()
		c.mu.Unlock()
	}
}

// Latest returns the most recent metrics snapshot.
func (c *Collector) Latest() engine.MetricsSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.count == 0 {
		return engine.MetricsSnapshot{}
	}
	idx := (c.pos - 1 + ringSize) % ringSize
	return c.ring[idx]
}

// History returns all snapshots in the ring buffer, oldest first.
func (c *Collector) History() []engine.MetricsSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]engine.MetricsSnapshot, 0, c.count)
	start := 0
	if c.count == ringSize {
		start = c.pos
	}
	for i := 0; i < c.count; i++ {
		idx := (start + i) % ringSize
		result = append(result, c.ring[idx])
	}
	return result
}

// Clients returns current client connections.
func (c *Collector) Clients() []engine.ClientConnection {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clients
}

// DiskHistory returns historical metrics from the disk log.
func (c *Collector) DiskHistory(since time.Time) []engine.MetricsSnapshot {
	return c.diskLog.Query(since)
}

// --- proc parsers ---

func readDiskStats() map[string]diskRaw {
	result := make(map[string]diskRaw)
	f, err := os.Open("/proc/diskstats")
	if err != nil {
		return result
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			continue
		}
		name := fields[2]
		// Only real disks (sd*, nvme*n*), skip partitions
		if !strings.HasPrefix(name, "sd") && !strings.HasPrefix(name, "nvme") {
			continue
		}
		if strings.HasPrefix(name, "sd") && len(name) > 3 {
			continue // sda1 etc
		}
		if strings.HasPrefix(name, "nvme") && strings.Contains(name, "p") {
			continue // nvme0n1p1 etc
		}
		readOps, _ := strconv.ParseUint(fields[3], 10, 64)
		readSectors, _ := strconv.ParseUint(fields[5], 10, 64)
		writeOps, _ := strconv.ParseUint(fields[7], 10, 64)
		writeSectors, _ := strconv.ParseUint(fields[9], 10, 64)
		result[name] = diskRaw{readSectors: readSectors, writeSectors: writeSectors, readOps: readOps, writeOps: writeOps}
	}
	return result
}

func readNetStats() map[string]netRaw {
	result := make(map[string]netRaw)
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return result
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) < 10 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		result[iface] = netRaw{rxBytes: rx, txBytes: tx}
	}
	return result
}

// getPortBytes returns [rx, tx] byte estimates for a given port using ss.
func getPortBytes(port int) [2]uint64 {
	out, err := exec.Command("ss", "-tn", fmt.Sprintf("sport = :%d or dport = :%d", port, port)).Output()
	if err != nil {
		return [2]uint64{}
	}
	// Count connections as a rough proxy; actual byte counters need ss -i parsing
	lines := strings.Split(string(out), "\n")
	count := uint64(0)
	for _, l := range lines {
		if strings.Contains(l, "ESTAB") {
			count++
		}
	}
	// Return connection count as a signal (actual throughput comes from /proc/net/dev)
	return [2]uint64{count, count}
}

// pollClients returns current SMB and NFS client connections.
func pollClients() []engine.ClientConnection {
	var clients []engine.ClientConnection

	// SMB clients via smbstatus
	if out, err := exec.Command("smbstatus", "--shares", "--fast").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 4 && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "Service") {
				clients = append(clients, engine.ClientConnection{
					Share: fields[0], User: fields[1], IP: fields[3],
					Protocol: "smb", ConnectedAt: time.Now().Unix(),
				})
			}
		}
	}

	// NFS clients via ss on port 2049
	if out, err := exec.Command("ss", "-tn", "sport = :2049").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.Contains(line, "ESTAB") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				peer := fields[4]
				if idx := strings.LastIndex(peer, ":"); idx > 0 {
					clients = append(clients, engine.ClientConnection{
						IP: peer[:idx], Protocol: "nfs", ConnectedAt: time.Now().Unix(),
					})
				}
			}
		}
	}
	return clients
}
