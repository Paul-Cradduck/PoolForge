package sharing

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	smbConfPath      = "/etc/samba/poolforge.conf"
	smbMainConf      = "/etc/samba/smb.conf"
	smbIncludeLine   = "include = /etc/samba/poolforge.conf"
	defaultWorkgroup = "WORKGROUP"
	defaultServerStr = "PoolForge NAS"
)

type SMBBackend struct{}

func (b *SMBBackend) WriteConfig(shares []Share) error {
	workgroup := readConfValue("POOLFORGE_SMB_WORKGROUP", defaultWorkgroup)
	serverStr := readConfValue("POOLFORGE_SMB_SERVER_NAME", defaultServerStr)
	minProto := readConfValue("POOLFORGE_SMB_MIN_PROTOCOL", "")
	maxConn := readConfValue("POOLFORGE_SMB_MAX_CONNECTIONS", "")

	var buf strings.Builder
	fmt.Fprintf(&buf, "[global]\n   workgroup = %s\n   server string = %s\n", workgroup, serverStr)
	if minProto != "" {
		fmt.Fprintf(&buf, "   min protocol = %s\n", minProto)
	}
	if maxConn != "" && maxConn != "0" {
		fmt.Fprintf(&buf, "   max connections = %s\n", maxConn)
	}
	buf.WriteString("\n")

	for _, s := range shares {
		guestOK := "no"
		if s.SMBPublic {
			guestOK = "yes"
		}
		browseable := "yes"
		if !s.SMBBrowsable {
			browseable = "no"
		}
		readOnly := "no"
		if s.ReadOnly {
			readOnly = "yes"
		}
		fmt.Fprintf(&buf, "[%s]\n", s.Name)
		fmt.Fprintf(&buf, "   path = %s\n", s.Path)
		fmt.Fprintf(&buf, "   browseable = %s\n", browseable)
		fmt.Fprintf(&buf, "   read only = %s\n", readOnly)
		fmt.Fprintf(&buf, "   guest ok = %s\n", guestOK)
		if !s.SMBPublic {
			fmt.Fprintf(&buf, "   valid users = @poolforge\n")
		}
		buf.WriteString("\n")
	}

	os.MkdirAll(filepath.Dir(smbConfPath), 0755)
	if err := os.WriteFile(smbConfPath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("write smb config: %w", err)
	}
	return b.ensureInclude()
}

func (b *SMBBackend) ensureInclude() error {
	data, err := os.ReadFile(smbMainConf)
	if err != nil {
		return nil
	}
	if strings.Contains(string(data), smbIncludeLine) {
		return nil
	}
	f, err := os.OpenFile(smbMainConf, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n%s\n", smbIncludeLine)
	return err
}

func (b *SMBBackend) ManageService(needed bool) {
	if needed {
		exec.Command("systemctl", "start", "smbd").Run()
		exec.Command("systemctl", "start", "nmbd").Run()
	} else {
		exec.Command("systemctl", "stop", "smbd").Run()
		exec.Command("systemctl", "stop", "nmbd").Run()
	}
}

func (b *SMBBackend) IsRunning() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "smbd").Run() == nil
}
