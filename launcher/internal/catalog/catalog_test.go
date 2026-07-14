package catalog

import "testing"

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
