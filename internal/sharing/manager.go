package sharing

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// Share mirrors engine.Share to avoid import cycle.
type Share struct {
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	Protocols    []string `json:"protocols"`
	NFSClients   string   `json:"nfs_clients"`
	SMBPublic    bool     `json:"smb_public"`
	SMBBrowsable bool     `json:"smb_browsable"`
	ReadOnly     bool     `json:"read_only"`
}

// NASUser mirrors engine.NASUser to avoid import cycle.
type NASUser struct {
	Name         string `json:"name"`
	UID          int    `json:"uid"`
	PoolID       string `json:"pool_id"`
	GlobalAccess bool   `json:"global_access"`
}

// ShareManager manages NAS shares and users for pools.
type ShareManager struct {
	smb *SMBBackend
	nfs *NFSBackend
}

func NewShareManager() *ShareManager {
	return &ShareManager{smb: &SMBBackend{}, nfs: &NFSBackend{}}
}

func (m *ShareManager) CreateShare(mountPoint string, share *Share) error {
	share.Path = filepath.Join(mountPoint, share.Name)
	if err := os.MkdirAll(share.Path, 0775); err != nil {
		return fmt.Errorf("create share dir: %w", err)
	}
	exec.Command("chgrp", "poolforge", share.Path).Run()
	exec.Command("chmod", "2775", share.Path).Run()
	return nil
}

func (m *ShareManager) DeleteShareDir(path string) error {
	return os.RemoveAll(path)
}

// ApplyConfig regenerates SMB and NFS configs from the given shares and manages services.
func (m *ShareManager) ApplyConfig(shares []Share) error {
	var smbShares, nfsShares []Share
	for _, s := range shares {
		for _, p := range s.Protocols {
			switch p {
			case "smb":
				smbShares = append(smbShares, s)
			case "nfs":
				nfsShares = append(nfsShares, s)
			}
		}
	}
	if err := m.smb.WriteConfig(smbShares); err != nil {
		return err
	}
	if err := m.nfs.WriteExports(nfsShares); err != nil {
		return err
	}
	m.smb.ManageService(len(smbShares) > 0)
	m.nfs.ManageService(len(nfsShares) > 0)
	return nil
}

// SMBRunning returns true if smbd is active.
func (m *ShareManager) SMBRunning() bool { return m.smb.IsRunning() }

// NFSRunning returns true if nfs-server is active.
func (m *ShareManager) NFSRunning() bool { return m.nfs.IsRunning() }

// ToggleSMB starts or stops the SMB service.
func (m *ShareManager) ToggleSMB(on bool) { m.smb.ManageService(on) }

// ToggleNFS starts or stops the NFS service.
func (m *ShareManager) ToggleNFS(on bool) { m.nfs.ManageService(on) }

// GetShareSize returns the size in bytes of a share directory.
func GetShareSize(path string) (uint64, error) {
	out, err := exec.Command("du", "-sb", path).Output()
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected du output")
	}
	return strconv.ParseUint(fields[0], 10, 64)
}

// CreateUser creates a POSIX user in the poolforge group and sets their Samba password.
func CreateUser(name, password, poolID string, globalAccess bool) (*NASUser, error) {
	exec.Command("groupadd", "-f", "poolforge").Run()
	if err := exec.Command("useradd", "-M", "-s", "/usr/sbin/nologin", "-G", "poolforge", name).Run(); err != nil {
		if _, lookupErr := user.Lookup(name); lookupErr != nil {
			return nil, fmt.Errorf("create user %s: %w", name, err)
		}
	}
	cmd := exec.Command("smbpasswd", "-a", "-s", name)
	cmd.Stdin = strings.NewReader(password + "\n" + password + "\n")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("set samba password: %w", err)
	}
	exec.Command("smbpasswd", "-e", name).Run()

	u, err := user.Lookup(name)
	if err != nil {
		return nil, err
	}
	uid, _ := strconv.Atoi(u.Uid)
	return &NASUser{Name: name, UID: uid, PoolID: poolID, GlobalAccess: globalAccess}, nil
}

// DeleteUser removes a POSIX user and their Samba password.
func DeleteUser(name string) error {
	exec.Command("smbpasswd", "-x", name).Run()
	exec.Command("userdel", name).Run()
	return nil
}
