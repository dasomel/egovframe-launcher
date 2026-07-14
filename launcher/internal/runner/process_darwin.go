//go:build darwin

package runner

import (
	"os/exec"
	"path/filepath"
	"syscall"
)

func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree sends SIGKILL to the whole process group of cmd on macOS.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		pgid = cmd.Process.Pid
	}
	return syscall.Kill(-pgid, syscall.SIGKILL)
}

// tomcatCatalina returns the command name and arguments to run Tomcat in
// foreground mode on darwin/linux.
func tomcatCatalina(tomcatHome string) (name string, args []string) {
	return filepath.Join(tomcatHome, "bin", "catalina.sh"), []string{"run"}
}
