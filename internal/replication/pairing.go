package replication

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/poolforge/poolforge/internal/engine"
)

const (
	pairingFile = "/var/lib/poolforge/pairing.json"
	codeExpiry  = 10 * time.Minute
)

type PairingManager struct {
	mu          sync.Mutex
	pendingCode string
	codeExpires time.Time
	nodes       []engine.PairedNode
}

func NewPairingManager() *PairingManager {
	pm := &PairingManager{}
	pm.load()
	return pm
}

// InitPairing generates a one-time pairing code.
func (pm *PairingManager) InitPairing(localName, localHost string) (string, error) {
	if err := EnsureKeys(); err != nil {
		return "", fmt.Errorf("generate keys: %w", err)
	}
	code, _ := rand.Int(rand.Reader, big.NewInt(999999))
	pm.mu.Lock()
	pm.pendingCode = fmt.Sprintf("%06d", code.Int64())
	pm.codeExpires = time.Now().Add(codeExpiry)
	pm.mu.Unlock()
	return fmt.Sprintf("%s@%s", pm.pendingCode, localHost), nil
}

// Exchange handles the key exchange from a remote node.
func (pm *PairingManager) Exchange(code, remoteName, remoteHost, remotePubKey string) (string, error) {
	// Strip port from host if present
	if idx := strings.LastIndex(remoteHost, ":"); idx > 0 {
		remoteHost = remoteHost[:idx]
	}
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.pendingCode == "" || time.Now().After(pm.codeExpires) {
		return "", fmt.Errorf("no pending pairing or code expired")
	}
	if code != pm.pendingCode {
		return "", fmt.Errorf("invalid pairing code")
	}
	pm.pendingCode = ""

	// Authorize remote key
	if err := AuthorizeKey(remotePubKey); err != nil {
		return "", err
	}

	// Store paired node
	node := engine.PairedNode{
		ID:        generateNodeID(),
		Name:      remoteName,
		Host:      remoteHost,
		Port:      22,
		PairedAt:  time.Now().Unix(),
		PublicKey: remotePubKey,
	}
	pm.nodes = append(pm.nodes, node)
	pm.save()

	// Return our public key
	pubKey, err := PublicKey()
	if err != nil {
		return "", err
	}
	return pubKey, nil
}

// CompletePairing is called by the joining node to store the remote node after exchange.
func (pm *PairingManager) CompletePairing(remoteName, remoteHost, remotePubKey string) {
	if idx := strings.LastIndex(remoteHost, ":"); idx > 0 {
		remoteHost = remoteHost[:idx]
	}
	pm.mu.Lock()
	defer pm.mu.Unlock()
	AuthorizeKey(remotePubKey)
	pm.nodes = append(pm.nodes, engine.PairedNode{
		ID: generateNodeID(), Name: remoteName, Host: remoteHost,
		Port: 22, PairedAt: time.Now().Unix(), PublicKey: remotePubKey,
	})
	pm.save()
}

// JoinRemote sends our key to a remote node and completes pairing.
func (pm *PairingManager) JoinRemote(codeAtHost, localName, localHost string) error {
	parts := strings.SplitN(codeAtHost, "@", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid format, expected CODE@HOST:PORT")
	}
	code, remoteAddr := parts[0], parts[1]
	if !strings.Contains(remoteAddr, ":") {
		remoteAddr += ":8080"
	}

	if err := EnsureKeys(); err != nil {
		return err
	}
	pubKey, err := PublicKey()
	if err != nil {
		return err
	}

	body := fmt.Sprintf(`{"code":"%s","name":"%s","host":"%s","public_key":"%s"}`, code, localName, localHost, pubKey)
	resp, err := http.Post("http://"+remoteAddr+"/api/pair/exchange", "application/json", strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("connect to %s: %w", remoteAddr, err)
	}
	defer resp.Body.Close()

	var result struct {
		PublicKey string `json:"public_key"`
		Name     string `json:"name"`
		Host     string `json:"host"`
		Error    string `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error != "" {
		return fmt.Errorf("remote: %s", result.Error)
	}

	pm.CompletePairing(result.Name, result.Host, result.PublicKey)
	return nil
}

func (pm *PairingManager) Nodes() []engine.PairedNode {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return append([]engine.PairedNode{}, pm.nodes...)
}

func (pm *PairingManager) RemoveNode(id string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for i, n := range pm.nodes {
		if n.ID == id {
			RemoveKey(n.PublicKey)
			pm.nodes = append(pm.nodes[:i], pm.nodes[i+1:]...)
			pm.save()
			return nil
		}
	}
	return fmt.Errorf("node %q not found", id)
}

func (pm *PairingManager) FindNode(id string) *engine.PairedNode {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for _, n := range pm.nodes {
		if n.ID == id {
			return &n
		}
	}
	return nil
}

func (pm *PairingManager) save() {
	os.MkdirAll(filepath.Dir(pairingFile), 0755)
	data, _ := json.MarshalIndent(pm.nodes, "", "  ")
	os.WriteFile(pairingFile, data, 0600)
}

func (pm *PairingManager) load() {
	data, err := os.ReadFile(pairingFile)
	if err != nil {
		return
	}
	json.Unmarshal(data, &pm.nodes)
}

func generateNodeID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
