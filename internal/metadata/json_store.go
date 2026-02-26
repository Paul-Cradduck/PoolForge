package metadata

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
)

const DefaultPath = "/var/lib/poolforge/metadata.json"

type JSONStore struct {
	path string
	mu   sync.Mutex
}

func NewJSONStore(path string) *JSONStore {
	if path == "" {
		path = DefaultPath
	}
	return &JSONStore{path: path}
}

// On-disk schema
type schema struct {
	Version int                    `json:"version"`
	Pools   map[string]*poolRecord `json:"pools"`
}

type poolRecord struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	ParityMode    string        `json:"parity_mode"`
	State         string        `json:"state"`
	Disks         []diskRecord  `json:"disks"`
	CapacityTiers []tierRecord  `json:"capacity_tiers"`
	RAIDArrays    []arrayRecord `json:"raid_arrays"`
	VolumeGroup   string        `json:"volume_group"`
	LogicalVolume string        `json:"logical_volume"`
	MountPoint    string        `json:"mount_point"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type diskRecord struct {
	Device        string        `json:"device"`
	CapacityBytes uint64        `json:"capacity_bytes"`
	State         string        `json:"state"`
	Slices        []sliceRecord `json:"slices"`
	FailedAt      *time.Time    `json:"failed_at,omitempty"`
}

type sliceRecord struct {
	TierIndex       int    `json:"tier_index"`
	PartitionNumber int    `json:"partition_number"`
	PartitionDevice string `json:"partition_device"`
	SizeBytes       uint64 `json:"size_bytes"`
}

type tierRecord struct {
	Index             int    `json:"index"`
	SliceSizeBytes    uint64 `json:"slice_size_bytes"`
	EligibleDiskCount int    `json:"eligible_disk_count"`
	RAIDArray         string `json:"raid_array"`
}

type arrayRecord struct {
	Device        string   `json:"device"`
	RAIDLevel     int      `json:"raid_level"`
	TierIndex     int      `json:"tier_index"`
	State         string   `json:"state"`
	Members       []string `json:"members"`
	CapacityBytes uint64   `json:"capacity_bytes"`
}

func (s *JSONStore) SavePool(pool *engine.Pool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		data = &schema{Version: 1, Pools: make(map[string]*poolRecord)}
	}
	data.Pools[pool.ID] = toRecord(pool)
	return s.save(data)
}

func (s *JSONStore) LoadPool(poolID string) (*engine.Pool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return nil, err
	}
	rec, ok := data.Pools[poolID]
	if !ok {
		return nil, fmt.Errorf("pool %q not found", poolID)
	}
	return fromRecord(rec), nil
}

func (s *JSONStore) ListPools() ([]engine.PoolSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return nil, nil
	}
	var summaries []engine.PoolSummary
	for _, rec := range data.Pools {
		summaries = append(summaries, engine.PoolSummary{
			ID:        rec.ID,
			Name:      rec.Name,
			State:     engine.PoolState(rec.State),
			DiskCount: len(rec.Disks),
		})
	}
	return summaries, nil
}

func (s *JSONStore) DeletePool(poolID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return err
	}
	delete(data.Pools, poolID)
	return s.save(data)
}

func (s *JSONStore) load() (*schema, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var data schema
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("corrupt metadata: %w", err)
	}
	return &data, nil
}

func (s *JSONStore) save(data *schema) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, s.path)
}

func toRecord(p *engine.Pool) *poolRecord {
	rec := &poolRecord{
		ID: p.ID, Name: p.Name, ParityMode: p.ParityMode.String(),
		State: string(p.State), VolumeGroup: p.VolumeGroup,
		LogicalVolume: p.LogicalVolume, MountPoint: p.MountPoint,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	for _, d := range p.Disks {
		dr := diskRecord{Device: d.Device, CapacityBytes: d.CapacityBytes, State: string(d.State), FailedAt: d.FailedAt}
		for _, sl := range d.Slices {
			dr.Slices = append(dr.Slices, sliceRecord{
				TierIndex: sl.TierIndex, PartitionNumber: sl.PartitionNumber,
				PartitionDevice: sl.PartitionDevice, SizeBytes: sl.SizeBytes,
			})
		}
		rec.Disks = append(rec.Disks, dr)
	}
	for _, t := range p.CapacityTiers {
		rec.CapacityTiers = append(rec.CapacityTiers, tierRecord{
			Index: t.Index, SliceSizeBytes: t.SliceSizeBytes,
			EligibleDiskCount: t.EligibleDiskCount, RAIDArray: t.RAIDArray,
		})
	}
	for _, a := range p.RAIDArrays {
		rec.RAIDArrays = append(rec.RAIDArrays, arrayRecord{
			Device: a.Device, RAIDLevel: a.RAIDLevel, TierIndex: a.TierIndex,
			State: string(a.State), Members: a.Members, CapacityBytes: a.CapacityBytes,
		})
	}
	return rec
}

func fromRecord(rec *poolRecord) *engine.Pool {
	pm := engine.Parity1
	if rec.ParityMode == "parity2" {
		pm = engine.Parity2
	}
	p := &engine.Pool{
		ID: rec.ID, Name: rec.Name, ParityMode: pm,
		State: engine.PoolState(rec.State), VolumeGroup: rec.VolumeGroup,
		LogicalVolume: rec.LogicalVolume, MountPoint: rec.MountPoint,
		CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt,
	}
	for _, dr := range rec.Disks {
		d := engine.DiskInfo{Device: dr.Device, CapacityBytes: dr.CapacityBytes, State: engine.DiskState(dr.State), FailedAt: dr.FailedAt}
		for _, sr := range dr.Slices {
			d.Slices = append(d.Slices, engine.SliceInfo{
				TierIndex: sr.TierIndex, PartitionNumber: sr.PartitionNumber,
				PartitionDevice: sr.PartitionDevice, SizeBytes: sr.SizeBytes,
			})
		}
		p.Disks = append(p.Disks, d)
	}
	for _, tr := range rec.CapacityTiers {
		p.CapacityTiers = append(p.CapacityTiers, engine.CapacityTier{
			Index: tr.Index, SliceSizeBytes: tr.SliceSizeBytes,
			EligibleDiskCount: tr.EligibleDiskCount, RAIDArray: tr.RAIDArray,
		})
	}
	for _, ar := range rec.RAIDArrays {
		p.RAIDArrays = append(p.RAIDArrays, engine.RAIDArray{
			Device: ar.Device, RAIDLevel: ar.RAIDLevel, TierIndex: ar.TierIndex,
			State: engine.ArrayState(ar.State), Members: ar.Members, CapacityBytes: ar.CapacityBytes,
		})
	}
	return p
}
