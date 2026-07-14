//go:build !windows

package runner

import (
	"os/exec"
	"testing"
	"time"
)

func TestKillTreeTerminatesChild(t *testing.T) {
	cmd := exec.Command("sh", "-c", "sleep 30")
	setProcAttr(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := killTree(cmd); err != nil {
		t.Fatalf("killTree: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done: // process exited (killed)
	case <-time.After(5 * time.Second):
		t.Fatal("child not terminated within 5s")
	}
}
