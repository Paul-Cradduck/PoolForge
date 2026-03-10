package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/poolforge/poolforge/internal/api"
	"github.com/poolforge/poolforge/internal/engine"
	"github.com/poolforge/poolforge/internal/metadata"
	"github.com/poolforge/poolforge/internal/safety"
	"github.com/poolforge/poolforge/internal/storage"
	"github.com/spf13/cobra"
)

func main() {
	meta := metadata.NewJSONStore("")
	eng := engine.NewEngine(
		storage.NewDiskManager(),
		storage.NewRAIDManager(),
		storage.NewLVMManager(),
		storage.NewFilesystemManager(),
		meta,
	)

	root := &cobra.Command{Use: "poolforge", Short: "Hybrid RAID storage manager"}

	pool := &cobra.Command{Use: "pool", Short: "Pool management commands"}
	root.AddCommand(pool)

	// pool create
	var createDisks string
	var createParity string
	var createName string
	var createExternal bool
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new storage pool",
		RunE: func(cmd *cobra.Command, args []string) error {
			pm, err := engine.ParseParityMode(createParity)
			if err != nil {
				return err
			}
			disks := strings.Split(createDisks, ",")
			p, err := eng.CreatePool(context.Background(), engine.CreatePoolRequest{
				Name: createName, Disks: disks, ParityMode: pm, External: createExternal,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Pool created: %s\n", p.Name)
			fmt.Printf("  ID:          %s\n", p.ID)
			fmt.Printf("  Parity:      %s\n", p.ParityMode)
			fmt.Printf("  Disks:       %d\n", len(p.Disks))
			fmt.Printf("  Tiers:       %d\n", len(p.CapacityTiers))
			fmt.Printf("  Arrays:      %d\n", len(p.RAIDArrays))
			fmt.Printf("  Mount:       %s\n", p.MountPoint)
			return nil
		},
	}
	createCmd.Flags().StringVar(&createDisks, "disks", "", "Comma-separated disk devices")
	createCmd.Flags().StringVar(&createParity, "parity", "parity1", "Parity mode: parity1 or parity2")
	createCmd.Flags().StringVar(&createName, "name", "", "Pool name")
	createCmd.MarkFlagRequired("disks")
	createCmd.MarkFlagRequired("name")
	pool.AddCommand(createCmd)

	// pool list
	pool.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all pools",
		RunE: func(cmd *cobra.Command, args []string) error {
			pools, err := eng.ListPools(context.Background())
			if err != nil {
				return err
			}
			if len(pools) == 0 {
				fmt.Println("No pools found.")
				return nil
			}
			fmt.Printf("%-20s %-10s %-15s %-15s %-6s\n", "NAME", "STATE", "TOTAL", "USED", "DISKS")
			for _, p := range pools {
				fmt.Printf("%-20s %-10s %-15s %-15s %-6d\n",
					p.Name, p.State,
					formatBytes(p.TotalCapacityBytes),
					formatBytes(p.UsedCapacityBytes),
					p.DiskCount)
			}
			return nil
		},
	})

	// pool status
	pool.AddCommand(&cobra.Command{
		Use:   "status [pool-name]",
		Short: "Show pool status",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Find pool by name
			pools, err := eng.ListPools(context.Background())
			if err != nil {
				return err
			}
			var poolID string
			for _, p := range pools {
				if p.Name == args[0] {
					poolID = p.ID
					break
				}
			}
			if poolID == "" {
				return fmt.Errorf("pool %q not found", args[0])
			}
			status, err := eng.GetPoolStatus(context.Background(), poolID)
			if err != nil {
				return err
			}
			fmt.Printf("Pool: %s  State: %s\n", status.Pool.Name, status.Pool.State)
			fmt.Printf("  VG: %s  LV: %s  Mount: %s\n\n", status.Pool.VolumeGroup, status.Pool.LogicalVolume, status.Pool.MountPoint)
			fmt.Println("Arrays:")
			for _, a := range status.ArrayStatuses {
				fmt.Printf("  %s  RAID%d  Tier%d  %s  Members: %s\n",
					a.Device, a.RAIDLevel, a.TierIndex, a.State, strings.Join(a.Members, ", "))
			}
			fmt.Println("\nDisks:")
			for _, d := range status.DiskStatuses {
				fmt.Printf("  %s  %s  Arrays: %s\n",
					d.Device, d.State, strings.Join(d.ContributingArrays, ", "))
			}
			return nil
		},
	})

	// pool add-disk
	var addDiskDev string
	addDiskCmd := &cobra.Command{
		Use:   "add-disk [pool-name]",
		Short: "Add a disk to an existing pool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			poolID, err := resolvePoolName(eng, args[0])
			if err != nil {
				return err
			}
			return eng.AddDisk(context.Background(), poolID, addDiskDev)
		},
	}
	addDiskCmd.Flags().StringVar(&addDiskDev, "disk", "", "Disk device to add")
	addDiskCmd.MarkFlagRequired("disk")
	pool.AddCommand(addDiskCmd)

	// pool replace-disk
	var replaceOld, replaceNew string
	replaceDiskCmd := &cobra.Command{
		Use:   "replace-disk [pool-name]",
		Short: "Replace a failed disk",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			poolID, err := resolvePoolName(eng, args[0])
			if err != nil {
				return err
			}
			return eng.ReplaceDisk(context.Background(), poolID, replaceOld, replaceNew)
		},
	}
	replaceDiskCmd.Flags().StringVar(&replaceOld, "old", "", "Failed disk device")
	replaceDiskCmd.Flags().StringVar(&replaceNew, "new", "", "Replacement disk device")
	replaceDiskCmd.MarkFlagRequired("old")
	replaceDiskCmd.MarkFlagRequired("new")
	pool.AddCommand(replaceDiskCmd)

	// pool remove-disk
	var removeDiskDev string
	removeDiskCmd := &cobra.Command{
		Use:   "remove-disk [pool-name]",
		Short: "Remove a disk from a pool",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			poolID, err := resolvePoolName(eng, args[0])
			if err != nil {
				return err
			}
			return eng.RemoveDisk(context.Background(), poolID, removeDiskDev)
		},
	}
	removeDiskCmd.Flags().StringVar(&removeDiskDev, "disk", "", "Disk device to remove")
	removeDiskCmd.MarkFlagRequired("disk")
	pool.AddCommand(removeDiskCmd)

	// pool import
	pool.AddCommand(&cobra.Command{
		Use:   "import",
		Short: "Import a pool from disks moved from another system",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := eng.ImportPool()
			if err != nil {
				return err
			}
			fmt.Printf("Pool imported: %s\n", result.PoolName)
			fmt.Printf("  ID:              %s\n", result.PoolID)
			fmt.Printf("  Arrays found:    %d\n", result.ArraysFound)
			fmt.Printf("  Disks remapped:  %d\n", result.DisksRemapped)
			fmt.Printf("  Arrays fixed:    %d\n", result.ArraysFixed)
			fmt.Printf("  Mount point:     %s\n", result.MountPoint)
			return nil
		},
	})

	// pool delete
	pool.AddCommand(&cobra.Command{
		Use:   "delete [pool-name]",
		Short: "Delete a pool and destroy all data",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			poolID, err := resolvePoolName(eng, args[0])
			if err != nil {
				return err
			}
			if err := eng.DeletePool(context.Background(), poolID); err != nil {
				return err
			}
			fmt.Printf("Pool %q deleted.\n", args[0])
			return nil
		},
	})

	// pool fail-disk (simulate failure for testing)
	var failDiskDev string
	failDiskCmd := &cobra.Command{
		Use:   "fail-disk [pool-name]",
		Short: "Mark a disk as failed (for testing)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			poolID, err := resolvePoolName(eng, args[0])
			if err != nil {
				return err
			}
			if err := eng.HandleDiskFailure(context.Background(), poolID, failDiskDev); err != nil {
				return err
			}
			fmt.Printf("Disk %s marked as failed in pool %q\n", failDiskDev, args[0])
			return nil
		},
	}
	failDiskCmd.Flags().StringVar(&failDiskDev, "disk", "", "Disk device to mark failed")
	failDiskCmd.MarkFlagRequired("disk")
	pool.AddCommand(failDiskCmd)

	// pool start
	var startForce bool
	startCmd := &cobra.Command{
		Use:   "start [pool-name]",
		Short: "Start a stopped pool (assemble arrays, mount filesystem)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Starting pool '%s'...\n", args[0])
			result, err := eng.StartPool(context.Background(), args[0], startForce)
			if err != nil {
				return err
			}
			if len(result.Warnings) > 0 && len(result.ArrayResults) == 0 {
				for _, w := range result.Warnings {
					fmt.Printf("  Warning: %s\n", w)
				}
				return fmt.Errorf("start aborted — use --force to proceed")
			}
			for _, ar := range result.ArrayResults {
				status := string(ar.State)
				extra := ""
				for _, p := range ar.ReAddedParts {
					extra += fmt.Sprintf(" → re-added %s", p)
				}
				for _, p := range ar.FullRebuilds {
					extra += fmt.Sprintf(" → rebuilding %s", p)
				}
				fmt.Printf("  %s (tier %d): %s%s\n", ar.Device, ar.TierIndex, status, extra)
			}
			fmt.Printf("\nPool '%s' is now RUNNING.\n  Mount: %s\n", args[0], result.MountPoint)
			return nil
		},
	}
	startCmd.Flags().BoolVar(&startForce, "force", false, "Skip drive count confirmation")
	pool.AddCommand(startCmd)

	// pool stop
	pool.AddCommand(&cobra.Command{
		Use:   "stop [pool-name]",
		Short: "Stop a running pool (unmount, stop arrays)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Stopping pool '%s'...\n", args[0])
			if err := eng.StopPool(context.Background(), args[0]); err != nil {
				return err
			}
			fmt.Println("\nIt is now SAFE to power down the external enclosure.")
			return nil
		},
	})

	// pool set-autostart
	pool.AddCommand(&cobra.Command{
		Use:   "set-autostart [pool-name] [true|false]",
		Short: "Configure whether a pool auto-starts at boot",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var autoStart bool
			switch args[1] {
			case "true":
				autoStart = true
			case "false":
				autoStart = false
			default:
				return fmt.Errorf("auto-start must be true or false, got %q", args[1])
			}
			if err := eng.SetAutoStart(context.Background(), args[0], autoStart); err != nil {
				return err
			}
			// Regenerate boot config
			safety.GenerateBootConfigFromMetadata(meta)
			if autoStart {
				fmt.Printf("Auto-start enabled for pool '%s'\n", args[0])
			} else {
				fmt.Printf("Auto-start disabled for pool '%s'. Manual start required.\n", args[0])
			}
			return nil
		},
	})

	// pool create --external flag
	createCmd.Flags().BoolVar(&createExternal, "external", false, "Mark pool as external enclosure (manual start required)")

	// serve — web portal + safety daemon
	var serveAddr, serveUser, servePass, webhookURL string
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the web management portal with safety daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Start safety daemon
			daemon := safety.NewDaemon(safety.DaemonConfig{
				Engine:        eng,
				MetadataStore: meta,
				MetadataPath:  "/var/lib/poolforge/metadata.json",
				AlertConfig:   safety.AlertConfig{WebhookURL: webhookURL},
				SMARTInterval: 5 * 60 * 1000000000,  // 5 min
				ScrubInterval: 7 * 24 * 3600000000000, // 7 days
				RAIDManager:   storage.NewRAIDManager(),
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go daemon.Run(ctx)

			srv := api.NewWithAuth(eng, serveUser, servePass)
			srv.SetAlerter(daemon.Alerter())
			srv.SetLogs(daemon.Logs())
			srv.SetDaemon(daemon)
			fmt.Printf("PoolForge web portal: http://%s\n", serveAddr)
			fmt.Println("Safety daemon: SMART monitoring, scrub scheduling, metadata backup")
			return http.ListenAndServe(serveAddr, srv)
		},
	}
	serveCmd.Flags().StringVar(&serveAddr, "addr", "0.0.0.0:8080", "Listen address")
	serveCmd.Flags().StringVar(&serveUser, "user", "", "Basic auth username")
	serveCmd.Flags().StringVar(&servePass, "pass", "", "Basic auth password")
	serveCmd.Flags().StringVar(&webhookURL, "webhook", "", "Alert webhook URL")
	root.AddCommand(serveCmd)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func resolvePoolName(eng engine.EngineService, name string) (string, error) {
	pools, err := eng.ListPools(context.Background())
	if err != nil {
		return "", err
	}
	for _, p := range pools {
		if p.Name == name {
			return p.ID, nil
		}
	}
	return "", fmt.Errorf("pool %q not found", name)
}

func formatBytes(b uint64) string {
	if b == 0 {
		return "-"
	}
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
