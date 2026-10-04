package catalog

import (
	"fmt"
	"testing"
)

func TestTargetsHaveUniqueNonEmptyIDs(t *testing.T) {
	seen := map[string]bool{}
	for _, tg := range Targets() {
		if tg.ID == "" {
			t.Fatalf("empty target ID: %+v", tg)
		}
		if seen[tg.ID] {
			t.Fatalf("duplicate target ID: %s", tg.ID)
		}
		seen[tg.ID] = true
		if tg.RepoURL == "" {
			t.Errorf("%s: empty RepoURL", tg.ID)
		}
		if len(tg.Prereqs) == 0 {
			t.Errorf("%s: missing prereqs", tg.ID)
		}
	}
}

func TestByID(t *testing.T) {
	tg, ok := ByID("boot-sample")
	if !ok {
		t.Fatal("boot-sample not found")
	}
	if tg.Port != 8090 || len(tg.Run) == 0 {
		t.Fatalf("boot-sample wrong shape: %+v", tg)
	}
	if _, ok := ByID("does-not-exist"); ok {
		t.Fatal("unexpected hit for missing id")
	}
}

func TestRunnableHavePort(t *testing.T) {
	for _, tg := range Targets() {
		if len(tg.Run) > 0 && tg.Port == 0 {
			t.Errorf("%s runnable but Port==0", tg.ID)
		}
	}
}

// Port 0 means "no served port" / "no port wait" and is exempt; every real
// port (target or Run-step dependency) must be unique across the catalog so
// targets can run concurrently.
func TestPortsUniqueAcrossCatalog(t *testing.T) {
	owner := map[int]string{}
	claim := func(port int, who string) {
		if port == 0 {
			return
		}
		if port < 0 || port > 65535 {
			t.Errorf("%s: invalid port %d", who, port)
			return
		}
		if prev, dup := owner[port]; dup {
			t.Errorf("port %d collides: %s and %s", port, prev, who)
			return
		}
		owner[port] = who
	}
	for _, tg := range Targets() {
		claim(tg.Port, tg.ID+" (target port)")
		for i, c := range tg.Run {
			claim(c.Port, fmt.Sprintf("%s (run[%d] %s)", tg.ID, i, c.Name))
		}
	}
}

func TestAvailableKnownTool(t *testing.T) {
	if !Available("go") {
		t.Skip("go not on PATH in test env")
	}
	if Available("definitely-not-a-real-tool-xyz") {
		t.Error("false positive for missing tool")
	}
}

func TestDeployTypeValid(t *testing.T) {
	valid := map[string]bool{"boot": true, "war": true, "react": true, "lib": true}
	for _, tg := range Targets() {
		if !valid[tg.DeployType] {
			t.Errorf("%s: invalid DeployType %q (must be boot|war|react|lib)", tg.ID, tg.DeployType)
		}
	}
}

func TestWARTargetsShape(t *testing.T) {
	warIDs := map[string]bool{
		"web-sample":          true,
		"portal":              true,
		"enterprise-business": true,
		"homepage":            true,
		"common-components":   true,
	}
	for _, tg := range Targets() {
		if tg.DeployType == "war" {
			if !warIDs[tg.ID] {
				t.Errorf("unexpected WAR target: %s", tg.ID)
			}
			if len(tg.Run) != 0 {
				t.Errorf("%s: WAR target must have empty Run, got %v", tg.ID, tg.Run)
			}
			if len(tg.Build) == 0 {
				t.Errorf("%s: WAR target must have non-empty Build", tg.ID)
			}
		}
		if warIDs[tg.ID] && tg.DeployType != "war" {
			t.Errorf("%s: expected DeployType=war, got %q", tg.ID, tg.DeployType)
		}
	}
}
