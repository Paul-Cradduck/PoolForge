package storage

// DiskManager handles disk partitioning via sgdisk.
type DiskManager interface {
	GetDiskInfo(device string) (*DiskInfoResult, error)
	CreateGPTPartitionTable(device string) error
	CreatePartition(device string, start, size uint64) (*Partition, error)
	ListPartitions(device string) ([]Partition, error)
}

type DiskInfoResult struct {
	Device        string
	CapacityBytes uint64
}

type Partition struct {
	Number int
	Device string
	Start  uint64
	Size   uint64
}

// RAIDManager handles mdadm operations.
type RAIDManager interface {
	CreateArray(opts RAIDCreateOpts) (*RAIDArrayInfo, error)
	GetArrayDetail(device string) (*RAIDArrayDetail, error)
	AssembleArray(device string, members []string) error
	StopArray(device string) error
}

type RAIDCreateOpts struct {
	Name            string
	Level           int
	Members         []string
	MetadataVersion string
}

type RAIDArrayInfo struct {
	Device  string
	Level   int
	Members []string
	State   string
}

type RAIDArrayDetail struct {
	Device        string
	Level         int
	State         string
	Members       []MemberInfo
	CapacityBytes uint64
}

type MemberInfo struct {
	Device string
	State  string
}

// LVMManager handles LVM operations.
type LVMManager interface {
	CreatePhysicalVolume(device string) error
	CreateVolumeGroup(name string, pvDevices []string) error
	CreateLogicalVolume(vgName string, lvName string, sizePercent int) error
	GetVolumeGroupInfo(name string) (*VGInfo, error)
}

type VGInfo struct {
	Name      string
	SizeBytes uint64
	FreeBytes uint64
	PVCount   int
	LVCount   int
}

// FilesystemManager handles ext4 operations.
type FilesystemManager interface {
	CreateFilesystem(device string) error
	MountFilesystem(device string, mountPoint string) error
	UnmountFilesystem(mountPoint string) error
	GetUsage(mountPoint string) (*FSUsage, error)
}

type FSUsage struct {
	TotalBytes uint64
	UsedBytes  uint64
	FreeBytes  uint64
}
