package sharing

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	exportsPath = "/etc/exports"
	beginMarker = "# BEGIN POOLFORGE"
	endMarker   = "# END POOLFORGE"
)

type NFSBackend struct{}

func (b *NFSBackend) WriteExports(shares []Share) error {
	var block strings.Builder
	block.WriteString(beginMarker + "\n")
	for _, s := range shares {
		clients := s.NFSClients
		if clients == "" {
			clients = "*"
		}
		opts := "rw"
		if s.ReadOnly {
			opts = "ro"
		}
		fmt.Fprintf(&block, "%s %s(%s,sync,no_subtree_check)\n", s.Path, clients, opts)
	}
	block.WriteString(endMarker + "\n")

	existing, _ := os.ReadFile(exportsPath)
	content := string(existing)

	startIdx := strings.Index(content, beginMarker)
	endIdx := strings.Index(content, endMarker)
	if startIdx >= 0 && endIdx >= 0 {
		content = content[:startIdx] + block.String() + content[endIdx+len(endMarker)+1:]
	} else {
		content = strings.TrimRight(content, "\n") + "\n" + block.String()
	}

	if err := os.WriteFile(exportsPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("write exports: %w", err)
	}
	exec.Command("exportfs", "-ra").Run()
	return nil
}

func (b *NFSBackend) ManageService(needed bool) {
	if needed {
		exec.Command("systemctl", "start", "nfs-kernel-server").Run()
	} else {
		exec.Command("systemctl", "stop", "nfs-kernel-server").Run()
	}
}

func (b *NFSBackend) IsRunning() bool {
	return exec.Command("systemctl", "is-active", "--quiet", "nfs-kernel-server").Run() == nil
}
