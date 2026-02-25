package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/poolforge/poolforge/internal/engine"
	"github.com/poolforge/poolforge/internal/metadata"
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
				Name: createName, Disks: disks, ParityMode: pm,
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
	createCmd.Flags().StringVar(&createParity, "parity", "shr1", "Parity mode: shr1 or shr2")
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

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
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
