package safety

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
)

type DaemonConfig struct {
	Engine        engine.EngineService
	MetadataStore engine.MetadataStore
	MetadataPath  string
	AlertConfig   AlertConfig
	SMARTInterval time.Duration
	ScrubInterval time.Duration
	RAIDManager   interface{ GetArrayUUID(string) (string, error) } // Phase 5: for UUID population
}

type Daemon struct {
	cfg     DaemonConfig
	alerter *Alerter
	scrub   *ScrubScheduler
	logs    *LogBuffer
	stop    chan struct{}

	// Status tracking
	mu              sync.Mutex
	startedAt       time.Time
	lastSMART       time.Time
	lastBackup      time.Time
	lastBootConfig  time.Time
	lastScrubStart  time.Time
	smartDiskCount  int
	smartErrors     int
}

type DaemonStatus struct {
	Running        bool      `json:"running"`
	Uptime         string    `json:"uptime"`
	StartedAt      time.Time `json:"startedAt"`
	SMART          FeatureStatus `json:"smart"`
	Scrub          FeatureStatus `json:"scrub"`
	MetadataBackup FeatureStatus `json:"metadataBackup"`
	BootConfig     FeatureStatus `json:"bootConfig"`
	GracefulShutdown FeatureStatus `json:"gracefulShutdown"`
}

type FeatureStatus struct {
	Enabled  bool      `json:"enabled"`
	Interval string    `json:"interval"`
	LastRun  time.Time `json:"lastRun,omitempty"`
	Detail   string    `json:"detail,omitempty"`
}

func (d *Daemon) Status() DaemonStatus {
	d.mu.Lock()
	defer d.mu.Unlock()
	return DaemonStatus{
		Running:   !d.startedAt.IsZero(),
		Uptime:    time.Since(d.startedAt).Truncate(time.Second).String(),
		StartedAt: d.startedAt,
		SMART: FeatureStatus{
			Enabled: true, Interval: d.cfg.SMARTInterval.String(),
			LastRun: d.lastSMART,
			Detail:  fmt.Sprintf("%d disks checked, %d errors", d.smartDiskCount, d.smartErrors),
		},
		Scrub: FeatureStatus{
			Enabled: true, Interval: d.cfg.ScrubInterval.String(),
			LastRun: d.lastScrubStart,
		},
		MetadataBackup: FeatureStatus{
			Enabled: true, Interval: "1h",
			LastRun: d.lastBackup,
		},
		BootConfig: FeatureStatus{
			Enabled: true, Interval: "on backup",
			LastRun: d.lastBootConfig,
		},
		GracefulShutdown: FeatureStatus{
			Enabled: true, Interval: "SIGINT/SIGTERM",
			Detail:  "metadata backup + scrub stop on shutdown",
		},
	}
}

func NewDaemon(cfg DaemonConfig) *Daemon {
	if cfg.SMARTInterval == 0 {
		cfg.SMARTInterval = 5 * time.Minute
	}
	if cfg.ScrubInterval == 0 {
		cfg.ScrubInterval = 7 * 24 * time.Hour // weekly
	}
	return &Daemon{
		cfg:     cfg,
		alerter: NewAlerter(cfg.AlertConfig),
		logs:    NewPersistentLogBuffer(500, "/var/lib/poolforge/logs.json"),
		stop:    make(chan struct{}),
	}
}

func (d *Daemon) Alerter() *Alerter   { return d.alerter }
func (d *Daemon) Logs() *LogBuffer    { return d.logs }

func (d *Daemon) Run(ctx context.Context) {
	d.mu.Lock()
	d.startedAt = time.Now()
	d.mu.Unlock()
	d.logs.Info("safety daemon started")
	log.Println("[safety] daemon started")

	// Graceful shutdown handler
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Phase 5: Boot pools (migration + per-pool auto-start)
	d.bootPools()

	// Generate mdadm.conf on startup
	d.updateBootConfig()

	// Start scrub scheduler
	arrays := d.getArrayDevices()
	d.scrub = NewScrubScheduler(d.cfg.ScrubInterval, func(dev string, err error) {
		d.alerter.Send(Alert{Level: AlertWarning, Message: fmt.Sprintf("scrub error on %s: %v", dev, err)})
	})
	d.scrub.Start(arrays)
	d.mu.Lock()
	d.lastScrubStart = time.Now()
	d.mu.Unlock()

	// SMART check ticker
	smartTicker := time.NewTicker(d.cfg.SMARTInterval)
	defer smartTicker.Stop()

	// Metadata backup ticker (hourly)
	backupTicker := time.NewTicker(1 * time.Hour)
	defer backupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			d.shutdown()
			return
		case sig := <-sigCh:
			d.logs.Info("received %s, shutting down gracefully", sig)
			log.Printf("[safety] received %s, shutting down gracefully", sig)
			d.shutdown()
			return
		case <-d.stop:
			return
		case <-smartTicker.C:
			d.checkSMART()
		case <-backupTicker.C:
			d.backupMetadata()
		}
	}
}

func (d *Daemon) Stop() { close(d.stop) }

func (d *Daemon) checkSMART() {
	diskCount, errCount := 0, 0
	pools, _ := d.cfg.MetadataStore.ListPools()
	for _, ps := range pools {
		pool, err := d.cfg.MetadataStore.LoadPool(ps.ID)
		if err != nil {
			continue
		}
		for _, disk := range pool.Disks {
			if disk.State == engine.DiskFailed {
				continue
			}
			status, err := CheckSMART(disk.Device)
			diskCount++
			if err != nil {
				d.logs.Warn("SMART check failed for %s: %v", disk.Device, err)
				continue
			}
			if !status.Healthy {
				errCount++
				d.logs.Error("SMART FAILED on %s: %v", disk.Device, status.Errors)
				d.alerter.Send(Alert{
					Level: AlertCritical, Pool: pool.Name, Device: disk.Device,
					Message: fmt.Sprintf("SMART health FAILED: %v", status.Errors),
				})
			} else if len(status.Errors) > 0 {
				d.logs.Warn("SMART warning on %s: %v", disk.Device, status.Errors)
				d.alerter.Send(Alert{
					Level: AlertWarning, Pool: pool.Name, Device: disk.Device,
					Message: fmt.Sprintf("SMART warning: %v", status.Errors),
				})
			} else {
				d.logs.Info("SMART OK: %s (temp=%d°C, hours=%d)", disk.Device, status.Temperature, status.PowerOnHrs)
			}
		}
	}
	d.mu.Lock()
	d.lastSMART = time.Now()
	d.smartDiskCount = diskCount
	d.smartErrors = errCount
	d.mu.Unlock()
}

func (d *Daemon) backupMetadata() {
	pools, _ := d.cfg.MetadataStore.ListPools()
	for _, ps := range pools {
		pool, err := d.cfg.MetadataStore.LoadPool(ps.ID)
		if err != nil {
			continue
		}
		if pool.MountPoint != "" {
			if err := BackupMetadataToMount(d.cfg.MetadataPath, pool.MountPoint); err == nil {
				d.logs.Info("metadata backed up to %s", pool.MountPoint)
			}
		}
	}
	d.updateBootConfig()
	d.mu.Lock()
	d.lastBackup = time.Now()
	d.mu.Unlock()
}

func (d *Daemon) updateBootConfig() {
	if err := GenerateBootConfigFromMetadata(d.cfg.MetadataStore); err != nil {
		d.logs.Warn("mdadm.conf update failed: %v", err)
	} else {
		d.logs.Info("mdadm.conf updated (pool-aware)")
		d.mu.Lock()
		d.lastBootConfig = time.Now()
		d.mu.Unlock()
	}
}

func (d *Daemon) getArrayDevices() []string {
	pools, _ := d.cfg.MetadataStore.ListPools()
	var arrays []string
	for _, ps := range pools {
		pool, _ := d.cfg.MetadataStore.LoadPool(ps.ID)
		if pool == nil {
			continue
		}
		for _, a := range pool.RAIDArrays {
			arrays = append(arrays, a.Device)
		}
	}
	return arrays
}

func (d *Daemon) shutdown() {
	log.Println("[safety] graceful shutdown: stopping scrubs")
	if d.scrub != nil {
		d.scrub.Stop()
	}
	// Sync filesystems
	pools, _ := d.cfg.MetadataStore.ListPools()
	for _, ps := range pools {
		pool, _ := d.cfg.MetadataStore.LoadPool(ps.ID)
		if pool != nil && pool.MountPoint != "" {
			BackupMetadataToMount(d.cfg.MetadataPath, pool.MountPoint)
			log.Printf("[safety] backed up metadata to %s", pool.MountPoint)
		}
	}
	log.Println("[safety] shutdown complete")
}

// Phase 5: Boot pools with migration detection and per-pool auto-start.
func (d *Daemon) bootPools() {
	pools, err := d.cfg.MetadataStore.ListPools()
	if err != nil || len(pools) == 0 {
		return
	}

	// Detect first run after upgrade
	needsMigration := false
	for _, ps := range pools {
		pool, err := d.cfg.MetadataStore.LoadPool(ps.ID)
		if err != nil {
			continue
		}
		if pool.OperationalStatus == "" {
			needsMigration = true
			break
		}
	}
	if needsMigration {
		d.migrateToPhase5(pools)
	}

	// Per-pool auto-start
	for _, ps := range pools {
		pool, err := d.cfg.MetadataStore.LoadPool(ps.ID)
		if err != nil {
			d.logs.Error("failed to load pool %s: %v", ps.Name, err)
			continue
		}
		if pool.RequiresManualStart {
			pool.OperationalStatus = engine.PoolOffline
			d.cfg.MetadataStore.SavePool(pool)
			d.logs.Info("pool %s: skipped (manual start required)", pool.Name)
		} else {
			// Verify the full stack is actually up: arrays + LVM + mount
			mounted := false
			if pool.MountPoint != "" {
				if _, err := os.Stat(pool.MountPoint); err == nil {
					// Check if actually mounted (not just directory exists)
					data, _ := os.ReadFile("/proc/mounts")
					mounted = strings.Contains(string(data), pool.MountPoint)
				}
			}
			if mounted {
				pool.OperationalStatus = engine.PoolRunning
			} else {
				pool.OperationalStatus = engine.PoolOffline
			}
			d.cfg.MetadataStore.SavePool(pool)
			d.logs.Info("pool %s: status=%s", pool.Name, pool.OperationalStatus)
		}
	}
}

// migrateToPhase5 performs one-time migration for pools created by Phase 1-4 builds.
func (d *Daemon) migrateToPhase5(pools []engine.PoolSummary) {
	d.logs.Info("PoolForge upgraded to Phase 5. Performing one-time metadata migration.")

	for _, ps := range pools {
		pool, err := d.cfg.MetadataStore.LoadPool(ps.ID)
		if err != nil {
			d.logs.Error("migration: failed to load pool %s: %v", ps.Name, err)
			continue
		}

		pool.IsExternal = false
		pool.RequiresManualStart = false
		pool.OperationalStatus = engine.PoolRunning

		// Populate Array UUIDs from live mdadm --detail
		if d.cfg.RAIDManager != nil {
			for i, arr := range pool.RAIDArrays {
				if arr.UUID == "" {
					uuid, err := d.cfg.RAIDManager.GetArrayUUID(arr.Device)
					if err != nil {
						d.logs.Warn("migration: could not get UUID for %s: %v", arr.Device, err)
					} else {
						pool.RAIDArrays[i].UUID = uuid
					}
				}
			}
		}

		if err := d.cfg.MetadataStore.SavePool(pool); err != nil {
			d.logs.Error("migration: failed to save pool %s: %v", ps.Name, err)
		} else {
			d.logs.Info("migration: pool %s migrated to Phase 5", pool.Name)
		}
	}

	// Regenerate Boot_Config
	if err := GenerateBootConfigFromMetadata(d.cfg.MetadataStore); err != nil {
		d.logs.Error("migration: failed to regenerate Boot_Config: %v", err)
	}

	d.logs.Info("PoolForge upgraded to Phase 5. Metadata migrated. Boot config updated.")
}
