//go:build windows

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
// command line on Windows via WMI.
func findRSPPort() (int, error) {
	// Filter out processes with null CommandLine to avoid PowerShell ExpandProperty error (exit status 1)
	out, err := exec.Command(
		"powershell", "-NoProfile", "-Command",
		"Get-CimInstance Win32_Process -Filter 'CommandLine is not null' | Select-Object -ExpandProperty CommandLine",
	).Output()
	if err != nil {
		return 0, fmt.Errorf("powershell failed: %w", err)
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
		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, "redhat-community-server-connector") ||
			strings.Contains(lineLower, "rsp") ||
			strings.Contains(lineLower, "server-connector") ||
			strings.Contains(lineLower, "jboss.tools.rsp") {
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
