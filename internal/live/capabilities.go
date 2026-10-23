package live

import (
	"context"
	"os/exec"
	"strings"
	"time"
	"vps-control/internal/executor"
)

type Capability struct {
	Engine    string `json:"engine"`
	Available bool   `json:"available"`
	Version   string `json:"version"`
}

func Capabilities(binaries map[string]executor.Binary) []Capability {
	result := []Capability{}
	for _, engine := range []string{"sing-box", "xray", "frps", "frpc"} {
		b := binaries[engine]
		c := Capability{Engine: engine, Version: "unavailable"}
		hash, e := executor.FileDigest(b.Path)
		if e == nil && len(b.SHA256) == 64 && hash == strings.ToLower(b.SHA256) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			args := []string{"version"}
			if engine == "frps" || engine == "frpc" {
				args = []string{"--version"}
			}
			cmd := exec.CommandContext(ctx, b.Path, args...)
			raw, err := cmd.Output()
			cancel()
			if err == nil {
				line := strings.SplitN(string(raw), "\n", 2)[0]
				if len(line) > 120 {
					line = line[:120]
				}
				c.Version = strings.TrimSpace(line)
				c.Available = true
			}
		}
		result = append(result, c)
	}
	return result
}
