package sharing

import (
	"os"
	"strings"
)

const poolforgeConf = "/etc/poolforge.conf"

// readConfValue reads a key=value from /etc/poolforge.conf, returning fallback if not found.
func readConfValue(key, fallback string) string {
	data, err := os.ReadFile(poolforgeConf)
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return fallback
}
