//go:build !windows

package runner

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var rspPortRe = regexp.MustCompile(`-Drsp\.server\.port=(\d+)`)

// findRSPPort discovers the RSP backend TCP port from the running java process
// command line on Unix/macOS. It looks for a process with both
// -Drsp.server.port=<N> and -Dorg.jboss.tools.rsp.id=redhat-community-server-connector.
func findRSPPort() (int, error) {
	out, err := exec.Command("ps", "-ax", "-o", "command=").Output()
	if err != nil {
		return 0, fmt.Errorf("ps failed: %w", err)
	}
	var fallback int
	for _, line := range strings.Split(string(out), "\n") {
		m := rspPortRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		port, _ := strconv.Atoi(m[1])
		if port <= 0 {
			continue
		}
		if strings.Contains(line, "redhat-community-server-connector") {
			return port, nil
		}
		if fallback == 0 {
			fallback = port
		}
	}
	if fallback > 0 {
		return fallback, nil
	}
	return 0, fmt.Errorf("RSP 백엔드 프로세스를 찾을 수 없습니다")
}
