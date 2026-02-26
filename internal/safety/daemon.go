package safety

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
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
}

type Daemon struct {
	cfg     DaemonConfig
	alerter *Alerter
	scrub   *ScrubScheduler
	stop    chan struct{}
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
		stop:    make(chan struct{}),
	}
}

func (d *Daemon) Alerter() *Alerter { return d.alerter }

func (d *Daemon) Run(ctx context.Context) {
	log.Println("[safety] daemon started")

	// Graceful shutdown handler
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Generate mdadm.conf on startup
	d.updateBootConfig()

	// Start scrub scheduler
	arrays := d.getArrayDevices()
	d.scrub = NewScrubScheduler(d.cfg.ScrubInterval, func(dev string, err error) {
		d.alerter.Send(Alert{Level: AlertWarning, Message: fmt.Sprintf("scrub error on %s: %v", dev, err)})
	})
	d.scrub.Start(arrays)

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
			if err != nil {
				continue
			}
			if !status.Healthy {
				d.alerter.Send(Alert{
					Level: AlertCritical, Pool: pool.Name, Device: disk.Device,
					Message: fmt.Sprintf("SMART health FAILED: %v", status.Errors),
				})
			} else if len(status.Errors) > 0 {
				d.alerter.Send(Alert{
					Level: AlertWarning, Pool: pool.Name, Device: disk.Device,
					Message: fmt.Sprintf("SMART warning: %v", status.Errors),
				})
			}
		}
	}
}

func (d *Daemon) backupMetadata() {
	pools, _ := d.cfg.MetadataStore.ListPools()
	for _, ps := range pools {
		pool, err := d.cfg.MetadataStore.LoadPool(ps.ID)
		if err != nil {
			continue
		}
		if pool.MountPoint != "" {
			BackupMetadataToMount(d.cfg.MetadataPath, pool.MountPoint)
		}
	}
	d.updateBootConfig()
}

func (d *Daemon) updateBootConfig() {
	arrays := d.getArrayDevices()
	if len(arrays) > 0 {
		if err := GenerateMdadmConf(arrays); err != nil {
			log.Printf("[safety] mdadm.conf update failed: %v", err)
		}
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
