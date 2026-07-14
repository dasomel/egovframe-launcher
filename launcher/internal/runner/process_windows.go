//go:build windows

package runner

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

func setProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// killTree uses taskkill to terminate the child and its descendants.
func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	return exec.Command("taskkill", "/T", "/F", "/PID", pid).Run()
}

// tomcatCatalina returns the command name and arguments to run Tomcat in
// foreground mode on Windows.
func tomcatCatalina(tomcatHome string) (name string, args []string) {
	return filepath.Join(tomcatHome, "bin", "catalina.bat"), []string{"run"}
}
