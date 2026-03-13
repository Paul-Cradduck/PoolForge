package replication

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const keyDir = "/var/lib/poolforge/ssh"

// EnsureKeys generates an SSH keypair if one doesn't exist.
func EnsureKeys() error {
	privPath := filepath.Join(keyDir, "id_ed25519")
	if _, err := os.Stat(privPath); err == nil {
		return nil
	}
	os.MkdirAll(keyDir, 0700)
	return exec.Command("ssh-keygen", "-t", "ed25519", "-f", privPath, "-N", "", "-q").Run()
}

// PublicKey returns the public key string.
func PublicKey() (string, error) {
	data, err := os.ReadFile(filepath.Join(keyDir, "id_ed25519.pub"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// PrivateKeyPath returns the path to the private key.
func PrivateKeyPath() string {
	return filepath.Join(keyDir, "id_ed25519")
}

// AuthorizeKey adds a public key to root's authorized_keys file.
func AuthorizeKey(pubKey string) error {
	// Add to root's authorized_keys for rsync access
	rootSSH := "/root/.ssh"
	os.MkdirAll(rootSSH, 0700)
	authFile := filepath.Join(rootSSH, "authorized_keys")
	existing, _ := os.ReadFile(authFile)
	if strings.Contains(string(existing), pubKey) {
		return nil
	}
	f, err := os.OpenFile(authFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s\n", pubKey)
	return err
}

// RemoveKey removes a public key from root's authorized_keys.
func RemoveKey(pubKey string) error {
	authFile := "/root/.ssh/authorized_keys"
	data, err := os.ReadFile(authFile)
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" && line != pubKey {
			lines = append(lines, line)
		}
	}
	return os.WriteFile(authFile, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}
