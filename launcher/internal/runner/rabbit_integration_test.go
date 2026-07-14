package runner

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"egovframe-launcher/internal/logbuf"
)

// TestEnsureRabbitMQContainer_CookieFix is an integration test that verifies
// the afterStart hook correctly fixes .erlang.cookie ownership.
// Requires: Docker running, port 5672 free.
// Run:  go test -run TestEnsureRabbitMQContainer_CookieFix -v -count=1
func TestEnsureRabbitMQContainer_CookieFix(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon not running")
	}

	// Clean slate — remove any existing container.
	exec.Command("docker", "rm", "-f", "-v", launcherRabbitContainer).Run()
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", "-v", launcherRabbitContainer).Run()
	})



	logs := logbuf.New(500)

	// Call the function under test.
	if err := ensureRabbitMQContainer(logs); err != nil {
		t.Fatalf("ensureRabbitMQContainer failed: %v\nLogs:\n%s", err, fmtLogLines(logs))
	}

	// Verify container is running.
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", launcherRabbitContainer).Output()
	if err != nil || strings.TrimSpace(string(out)) != "true" {
		t.Fatalf("container not running after ensureRabbitMQContainer; inspect=%q err=%v", string(out), err)
	}

	// Verify .erlang.cookie ownership is rabbitmq (not root).
	out, err = exec.Command("docker", "exec", launcherRabbitContainer,
		"stat", "-c", "%U:%G", "/var/lib/rabbitmq/.erlang.cookie").Output()
	if err != nil {
		// stat -c may not exist in some images; try ls -la fallback.
		out, err = exec.Command("docker", "exec", launcherRabbitContainer,
			"ls", "-la", "/var/lib/rabbitmq/.erlang.cookie").Output()
		if err != nil {
			t.Fatalf("cannot stat .erlang.cookie: %v", err)
		}
		if !strings.Contains(string(out), "rabbitmq") {
			t.Errorf(".erlang.cookie not owned by rabbitmq: %s", string(out))
		}
	} else {
		owner := strings.TrimSpace(string(out))
		if owner != "rabbitmq:rabbitmq" {
			t.Errorf(".erlang.cookie owner = %q, want rabbitmq:rabbitmq", owner)
		}
	}

	// Verify rabbitmq-diagnostics ping works (service is healthy).
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if exec.Command("docker", "exec", launcherRabbitContainer,
			"rabbitmq-diagnostics", "-q", "ping").Run() == nil {
			t.Log("rabbitmq-diagnostics ping: OK")
			t.Logf("Logs:\n%s", fmtLogLines(logs))
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Errorf("rabbitmq-diagnostics ping did not succeed within 30s\nLogs:\n%s", fmtLogLines(logs))
}

func fmtLogLines(logs *logbuf.Buf) string {
	lines := logs.Snapshot()
	buf := strings.Builder{}
	for i, l := range lines {
		buf.WriteString(fmt.Sprintf("  %d: %s\n", i, l))
	}
	return buf.String()
}
