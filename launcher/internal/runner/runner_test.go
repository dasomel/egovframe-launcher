package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"egovframe-launcher/internal/catalog"
	"egovframe-launcher/internal/logbuf"
	"egovframe-launcher/internal/persist"
)

// testCommentRE mirrors serverXMLCommentRE in runner.go, used here to pull
// out an individual comment block for assertions.
var testCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)

// findCommentContaining returns the first XML comment block in s that
// contains needle, failing the test if none is found.
func findCommentContaining(t *testing.T, s, needle string) string {
	t.Helper()
	for _, block := range testCommentRE.FindAllString(s, -1) {
		if strings.Contains(block, needle) {
			return block
		}
	}
	t.Fatalf("no comment block containing %q found in:\n%s", needle, s)
	return ""
}

func TestMain(m *testing.M) {
	// Prevent persist.Load() from picking up the user's real ~/.egov-launcher.json
	// (which may contain a workspacePath that would override test temp dirs).
	dir, err := os.MkdirTemp("", "runner-test-persist-*")
	if err != nil {
		panic(err)
	}
	persist.Init(filepath.Join(dir, "config.json"))

	// Disable real Tomcat downloading in tests to avoid network calls and state hanging
	autoInstallTomcatFn = func(logs *logbuf.Buf) (string, error) {
		return "", fmt.Errorf("auto-installation disabled in tests")
	}

	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fakeTarget swaps the catalog with a target driven by portable shell-free
// commands so the test runs without git/mvn.
func withFakeCatalog(t *testing.T) func() {
	t.Helper()
	orig := catalogTargets
	catalogTargets = func() []catalog.Target {
		return []catalog.Target{{
			ID: "demo", Tier: catalog.Tier1, Port: 9999, RepoURL: "x",
			Build: []catalog.Command{{Name: "go", Args: []string{"version"}}},
			Run:   []catalog.Command{{Name: "go", Args: []string{"version"}}},
		}}
	}
	return func() { catalogTargets = orig }
}

func withSleepCatalog(t *testing.T) func() {
	t.Helper()
	orig := catalogTargets
	catalogTargets = func() []catalog.Target {
		return []catalog.Target{{
			ID: "demo", Tier: catalog.Tier1, Port: 9999, RepoURL: "x",
			Build: []catalog.Command{{Name: "go", Args: []string{"version"}}},
			Run:   []catalog.Command{{Name: "sleep", Args: []string{"30"}}},
		}}
	}
	return func() { catalogTargets = orig }
}

func TestBuildRunsToDone(t *testing.T) {
	defer withFakeCatalog(t)()
	ws := t.TempDir()
	// pre-create the clone dir so build skips git
	if err := os.MkdirAll(filepath.Join(ws, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(ws)
	if err := r.Do("demo", "build"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, r, "demo", StatusDone)
}

func TestDoRejectsUnknownTargetAndAction(t *testing.T) {
	defer withFakeCatalog(t)()
	r := New(t.TempDir())
	if err := r.Do("nope", "build"); err == nil {
		t.Error("expected error for unknown target")
	}
	if err := r.Do("demo", "frobnicate"); err == nil {
		t.Error("expected error for unknown action")
	}
}

func TestRunStopsCleanly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on Windows")
	}
	defer withSleepCatalog(t)()
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(ws)
	if err := r.Do("demo", "run"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, r, "demo", StatusRunning)
	if err := r.Stop("demo"); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, r, "demo", StatusStopped)
}

// withWARCatalog swaps the catalog with a minimal WAR-type target.
func withWARCatalog(t *testing.T) func() {
	t.Helper()
	orig := catalogTargets
	catalogTargets = func() []catalog.Target {
		return []catalog.Target{{
			ID: "demo", Tier: catalog.Tier1, Port: 8080, RepoURL: "x",
			DeployType: "war",
			Build:      []catalog.Command{{Name: "go", Args: []string{"version"}}},
		}}
	}
	return func() { catalogTargets = orig }
}

func TestRunWARRequiresTomcatPath(t *testing.T) {
	defer withWARCatalog(t)()
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := New(ws)
	if err := r.RunWAR("demo", "", 0); err != nil {
		t.Fatalf("RunWAR returned synchronous error: %v", err)
	}
	waitStatus(t, r, "demo", StatusError)
	if st := r.State("demo"); !strings.Contains(st.Err, "Tomcat") {
		t.Errorf("expected Err to mention Tomcat, got: %q", st.Err)
	}
	// Verify that the error is also appended to logs
	snapshot := r.Logs("demo").Snapshot()
	found := false
	for _, line := range snapshot {
		if strings.Contains(line, "[error]") && strings.Contains(line, "Tomcat") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error logs to contain Tomcat message, got logs: %q", snapshot)
	}
}

// stockServerXML mirrors a real Tomcat 10 server.xml: Server/Service, the
// live HTTP Connector on 8080, an AJP connector, and the stock commented-out
// "shared thread pool" Connector block that also mentions port="8080".
const stockServerXML = `<?xml version="1.0" encoding="UTF-8"?>
<Server port="8005" shutdown="SHUTDOWN">
  <Service name="Catalina">
    <!--The connectors can use a shared executor, you can define one or more named thread pools-->
    <!--
    <Executor name="tomcatThreadPool" namePrefix="catalina-exec-"
        maxThreads="150" minSpareThreads="4"/>
    -->
    <!--
    <Connector executor="tomcatThreadPool"
               port="8080" protocol="HTTP/1.1"
               connectionTimeout="20000"
               redirectPort="8443" />
    -->
    <Connector port="8080" protocol="HTTP/1.1"
               connectionTimeout="20000"
               redirectPort="8443" />
    <Connector port="8009" protocol="AJP/1.3" redirectPort="8443" secretRequired="false" />
    <Engine name="Catalina" defaultHost="localhost">
      <Host name="localhost" appBase="webapps" unpackWARs="true" autoDeploy="true">
      </Host>
    </Engine>
  </Service>
</Server>`

// rspServerXML mirrors the isolated .catalina-base/conf/server.xml produced
// after the RSP feature rewrites the shared Tomcat's active Connector to
// port="8081": the only remaining port="8080" sits inside the commented
// thread-pool Connector block, which is the regression this test guards.
const rspServerXML = `<?xml version="1.0" encoding="UTF-8"?>
<Server port="8005" shutdown="SHUTDOWN">
  <Service name="Catalina">
    <!--
    <Connector executor="tomcatThreadPool"
               port="8080" protocol="HTTP/1.1"
               connectionTimeout="20000"
               redirectPort="8443" />
    -->
    <Connector port="8081" protocol="HTTP/1.1"
               connectionTimeout="20000"
               redirectPort="8443" />
    <Connector port="8009" protocol="AJP/1.3" redirectPort="8443" secretRequired="false" />
    <Engine name="Catalina" defaultHost="localhost">
      <Host name="localhost" appBase="webapps" unpackWARs="true" autoDeploy="true">
      </Host>
    </Engine>
  </Service>
</Server>`

func TestPatchServerXML(t *testing.T) {
	t.Run("stock server.xml: commented thread-pool connector left untouched", func(t *testing.T) {
		out, err := patchServerXML(stockServerXML, 9090, 9005)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, `<Server port="9005" shutdown="SHUTDOWN">`) {
			t.Error("shutdown port not replaced on <Server>")
		}
		if !strings.Contains(out, `<Connector port="9090" protocol="HTTP/1.1"`) {
			t.Error("active HTTP connector port not replaced")
		}
		if !strings.Contains(out, `useBodyEncodingForURI="true"`) {
			t.Error("expected URIEncoding attrs injected into the active connector")
		}
		// AJP connector must be untouched.
		if !strings.Contains(out, `port="8009"`) {
			t.Error("AJP connector port must not be changed")
		}
		// The commented thread-pool Connector must still contain the
		// original literal port="8080" and must NOT have gained URIEncoding.
		commentBlock := findCommentContaining(t, out, `port="8080"`)
		if strings.Contains(commentBlock, "URIEncoding") {
			t.Error("commented connector must not have URIEncoding injected")
		}
	})

	t.Run("RSP-modified input: active 8081 connector patched, comment untouched (regression)", func(t *testing.T) {
		out, err := patchServerXML(rspServerXML, 9090, 9005)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out, `<Connector port="9090" protocol="HTTP/1.1"`) {
			t.Error("active HTTP connector (originally 8081) not replaced with requested port")
		}
		if strings.Contains(out, `port="8081"`) {
			t.Error("old active connector port 8081 should no longer be present")
		}
		commentBlock := findCommentContaining(t, out, `port="8080"`)
		if strings.Contains(commentBlock, "URIEncoding") {
			t.Error("commented connector must not have URIEncoding injected")
		}
	})

	t.Run("no HTTP connector returns error", func(t *testing.T) {
		noConnector := `<?xml version="1.0" encoding="UTF-8"?>
<Server port="8005" shutdown="SHUTDOWN">
  <Service name="Catalina">
    <Connector port="8009" protocol="AJP/1.3" redirectPort="8443" />
  </Service>
</Server>`
		_, err := patchServerXML(noConnector, 9090, 9005)
		if err == nil {
			t.Fatal("expected error when no HTTP connector is present")
		}
	})
}

func TestPatchCentralConfigEurekaPort(t *testing.T) {
	t.Run("rewrites 8761 to 8762", func(t *testing.T) {
		dir := t.TempDir()
		configDir := filepath.Join(dir, "config")
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(configDir, "application.yml")
		original := "defaultZone: http://admin:admin@${eureka.instance.hostname:localhost}:8761/eureka\n"
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}

		patchCentralConfigEurekaPort(dir)

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read back application.yml: %v", err)
		}
		out := string(data)
		if !strings.Contains(out, ":8762/eureka") {
			t.Error("expected :8762/eureka after patch")
		}
		if strings.Contains(out, ":8761/eureka") {
			t.Error("did not expect :8761/eureka to remain after patch")
		}
	})

	t.Run("idempotent on repeated calls", func(t *testing.T) {
		dir := t.TempDir()
		configDir := filepath.Join(dir, "config")
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(configDir, "application.yml")
		original := "defaultZone: http://admin:admin@${eureka.instance.hostname:localhost}:8761/eureka\n"
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
			t.Fatal(err)
		}

		patchCentralConfigEurekaPort(dir)
		firstPass, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read back after first patch: %v", err)
		}

		patchCentralConfigEurekaPort(dir)
		secondPass, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read back after second patch: %v", err)
		}

		if string(firstPass) != string(secondPass) {
			t.Error("expected idempotent output across repeated calls")
		}
		if strings.Count(string(secondPass), ":8762/eureka") != 1 {
			t.Errorf("expected exactly one :8762/eureka occurrence, got content: %q", string(secondPass))
		}
	})

	t.Run("no config dir is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		// No config subdirectory created; must not panic or error.
		patchCentralConfigEurekaPort(dir)
	})
}

func waitStatus(t *testing.T, r *Runner, id string, want Status) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if r.State(id).Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("status of %s = %s, want %s", id, r.State(id).Status, want)
}

func TestCloneCleansUpExistingDir(t *testing.T) {
	defer withFakeCatalog(t)()
	ws := t.TempDir()
	dir := filepath.Join(ws, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "somefile.txt")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(ws)
	_ = r.DoWithOptions("demo", "clone", true)

	deadline := time.Now().Add(5 * time.Second)
	deleted := false
	for time.Now().Before(deadline) {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			deleted = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !deleted {
		t.Error("expected pre-existing files in target directory to be cleaned up before clone")
	}
}

func TestCloneKeepsExistingDir(t *testing.T) {
	defer withFakeCatalog(t)()
	ws := t.TempDir()
	dir := filepath.Join(ws, "demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "somefile.txt")
	if err := os.WriteFile(file, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(ws)
	_ = r.DoWithOptions("demo", "clone", false)

	waitStatus(t, r, "demo", StatusDone)

	if _, err := os.Stat(file); os.IsNotExist(err) {
		t.Error("expected existing files to be preserved when clean=false")
	}
}

func TestTomcatMajorVersion(t *testing.T) {
	cases := map[string]int{
		"                     Apache Tomcat Version 9.0.107\n": 9,
		"Apache Tomcat Version 10.1.56\nmore text":             10,
		"Apache Tomcat Version 11.0.0-M1\n":                    11,
		"no version line here":                                 0,
	}
	for notes, want := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "RELEASE-NOTES"), []byte(notes), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := tomcatMajorVersion(dir); got != want {
			t.Errorf("notes %q: got %d, want %d", notes, got, want)
		}
	}
	if got := tomcatMajorVersion(t.TempDir()); got != 0 {
		t.Errorf("missing RELEASE-NOTES: got %d, want 0", got)
	}
}

func TestRunWARExploded(t *testing.T) {
	defer withWARCatalog(t)()
	ws := t.TempDir()

	// Create mock project directory
	projectDir := filepath.Join(ws, "demo")
	if err := os.MkdirAll(filepath.Join(projectDir, "target", "egovframe-web-5.0.0", "WEB-INF"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create mock tomcat directory
	tomcatHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tomcatHome, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tomcatHome, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create minimal catalina script
	var catScript string
	if runtime.GOOS == "windows" {
		catScript = filepath.Join(tomcatHome, "bin", "catalina.bat")
	} else {
		catScript = filepath.Join(tomcatHome, "bin", "catalina.sh")
	}
	if err := os.WriteFile(catScript, []byte("echo Mock Catalina"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create mock server.xml
	serverXML := `<?xml version="1.0" encoding="UTF-8"?>
<Server port="8005" shutdown="SHUTDOWN">
  <Service name="Catalina">
    <Connector port="8080" protocol="HTTP/1.1" />
  </Service>
</Server>`
	if err := os.WriteFile(filepath.Join(tomcatHome, "conf", "server.xml"), []byte(serverXML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create RELEASE-NOTES with Tomcat 10.1 version
	if err := os.WriteFile(filepath.Join(tomcatHome, "RELEASE-NOTES"), []byte("Apache Tomcat Version 10.1.56\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := New(ws)
	// We run it as a job directly
	j := &job{
		target: catalogTargets()[0], // WAR target from withWARCatalog
		logs:   logbuf.New(1000),
	}
	r.jobs["demo"] = j

	// Run runWAR logic
	r.runWAR(j, tomcatHome, 8082)

	// Check if runWAR created config files successfully
	base := filepath.Join(projectDir, ".catalina-base")
	rootXMLPath := filepath.Join(base, "conf", "Catalina", "localhost", "ROOT.xml")

	data, err := os.ReadFile(rootXMLPath)
	if err != nil {
		t.Fatalf("expected ROOT.xml to be created: %v", err)
	}

	content := string(data)
	expectedDocBase := filepath.ToSlash(filepath.Join(projectDir, "target", "egovframe-web-5.0.0"))
	if !strings.Contains(content, expectedDocBase) {
		t.Errorf("expected ROOT.xml docBase to contain %q, got: %q", expectedDocBase, content)
	}
}

func TestPatchVSCodeSettings(t *testing.T) {
	dir := t.TempDir()
	jh := "/mock/java/home/path"

	// Test creating new settings
	if err := patchVSCodeSettings(dir, jh); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	settingsPath := filepath.Join(dir, ".vscode", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("failed to read settings.json: %v", err)
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if settings["rsp-ui.rsp.java.home"] != jh {
		t.Errorf("expected rsp-ui.rsp.java.home to be %q, got %q", jh, settings["rsp-ui.rsp.java.home"])
	}
	if settings["java.jdt.ls.java.home"] != jh {
		t.Errorf("expected java.jdt.ls.java.home to be %q, got %q", jh, settings["java.jdt.ls.java.home"])
	}
}

func TestPatchRSPDeployables(t *testing.T) {
	// Create mock home and project directories
	mockHome := t.TempDir()
	projectDir := t.TempDir()

	// Setup environment variables so os.UserHomeDir() redirects to mockHome
	t.Setenv("HOME", mockHome)
	t.Setenv("USERPROFILE", mockHome)

	// Create mock exploded target directory structure
	explodedDir := filepath.Join(projectDir, "target", "egovframe-web-5.0.0")
	if err := os.MkdirAll(filepath.Join(explodedDir, "WEB-INF"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Create mock RSP server configuration file
	rspServersDir := filepath.Join(mockHome, ".rsp", "redhat-community-server-connector", "servers")
	if err := os.MkdirAll(rspServersDir, 0o755); err != nil {
		t.Fatal(err)
	}

	serverConfPath := filepath.Join(rspServersDir, "tomcat-10.1.23")
	initialConfig := map[string]interface{}{
		"id":                                "tomcat-10.1.23",
		"org.jboss.tools.rsp.server.typeId": "org.jboss.ide.eclipse.as.server.tomcat.100",
		"deployables": map[string]interface{}{
			// Point to stale projectDir root which should be removed by patch
			projectDir: map[string]interface{}{
				"label": projectDir,
				"path":  projectDir,
			},
		},
	}

	initData, err := json.Marshal(initialConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(serverConfPath, initData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Run patching
	if err := patchRSPDeployables(projectDir); err != nil {
		t.Fatalf("unexpected error patching deployables: %v", err)
	}

	// Read back and verify configuration
	data, err := os.ReadFile(serverConfPath)
	if err != nil {
		t.Fatalf("failed to read back RSP configuration: %v", err)
	}

	var patched map[string]interface{}
	if err := json.Unmarshal(data, &patched); err != nil {
		t.Fatalf("failed to unmarshal patched JSON: %v", err)
	}

	deployables, ok := patched["deployables"].(map[string]interface{})
	if !ok {
		t.Fatal("expected deployables section to exist")
	}

	// Project root directory must be cleaned up
	if _, exists := deployables[filepath.ToSlash(projectDir)]; exists {
		t.Error("expected project root folder deployable to be deleted")
	}

	// Exploded target directory must be added
	expectedExplodedSlash := filepath.ToSlash(explodedDir)
	info, exists := deployables[expectedExplodedSlash]
	if !exists {
		t.Fatalf("expected exploded path %q to be added, but not found", expectedExplodedSlash)
	}

	infoMap, _ := info.(map[string]interface{})
	if infoMap["path"] != expectedExplodedSlash {
		t.Errorf("expected deployable path to be %q, got: %q", expectedExplodedSlash, infoMap["path"])
	}
}

func TestNeedsDatabaseDetectsComposeInit(t *testing.T) {
	dir := t.TempDir()
	if needsDatabase(dir) {
		t.Fatal("expected empty project to not need a database")
	}

	initDir := filepath.Join(dir, "docker-compose", "mysql", "init")
	if err := os.MkdirAll(initDir, 0o755); err != nil {
		t.Fatalf("failed to create init dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(initDir, "com_mysql.sql"), []byte("-- seed"), 0o644); err != nil {
		t.Fatalf("failed to write init sql: %v", err)
	}

	if !needsDatabase(dir) {
		t.Error("expected docker-compose/mysql/init/*.sql to signal a DB requirement")
	}
	if got := composeInitSQLDir(dir); got != initDir {
		t.Errorf("composeInitSQLDir = %q, want %q", got, initDir)
	}
}

func TestBootPropsDBInfo(t *testing.T) {
	dir := t.TempDir()
	if sqlDir, schema, props := bootPropsDBInfo(dir); sqlDir != "" || schema != "" || props != "" {
		t.Fatalf("expected empty project to not match, got (%q, %q, %q)", sqlDir, schema, props)
	}

	dbDir := filepath.Join(dir, "DATABASE")
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		t.Fatalf("failed to create DATABASE dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dbDir, "all_sht_ddl_mysql.sql"), []byte("-- ddl"), 0o644); err != nil {
		t.Fatalf("failed to write ddl sql: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dbDir, "all_sht_data_mysql.sql"), []byte("-- data"), 0o644); err != nil {
		t.Fatalf("failed to write data sql: %v", err)
	}

	// DDL/DATA present but no application.properties yet -> still no match.
	if sqlDir, schema, props := bootPropsDBInfo(dir); sqlDir != "" || schema != "" || props != "" {
		t.Fatalf("expected missing application.properties to not match, got (%q, %q, %q)", sqlDir, schema, props)
	}

	propsDir := filepath.Join(dir, "src", "main", "resources")
	if err := os.MkdirAll(propsDir, 0o755); err != nil {
		t.Fatalf("failed to create resources dir: %v", err)
	}
	propsPath := filepath.Join(propsDir, "application.properties")
	if err := os.WriteFile(propsPath, []byte("Globals.DbType=hsql\n"), 0o644); err != nil {
		t.Fatalf("failed to write application.properties: %v", err)
	}

	gotSQLDir, gotSchema, gotProps := bootPropsDBInfo(dir)
	if gotSQLDir != dbDir {
		t.Errorf("sqlDir = %q, want %q", gotSQLDir, dbDir)
	}
	if gotSchema != "sht" {
		t.Errorf("schema = %q, want %q", gotSchema, "sht")
	}
	if gotProps != propsPath {
		t.Errorf("propsPath = %q, want %q", gotProps, propsPath)
	}

	if !needsDatabase(dir) {
		t.Error("expected boot-properties DB pattern to signal a DB requirement")
	}
}

func TestParseComposeDBConfig(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "ConfigServer", "src", "main", "resources", "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	yaml := "datasource:\n" +
		"    driver-class-name: com.mysql.cj.jdbc.Driver\n" +
		"    url: jdbc:mysql://localhost:3306/com\n" +
		"    username: com\n" +
		"    password: com01\n"
	if err := os.WriteFile(filepath.Join(configDir, "application-local.yml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("failed to write application-local.yml: %v", err)
	}

	dbName, user, pass := parseComposeDBConfig(dir)
	if dbName != "com" || user != "com" || pass != "com01" {
		t.Errorf("parseComposeDBConfig = (%q, %q, %q), want (com, com, com01)", dbName, user, pass)
	}
}

func TestParseComposeDBConfigDefaultsWhenMissing(t *testing.T) {
	dir := t.TempDir()
	dbName, user, pass := parseComposeDBConfig(dir)
	if dbName != defaultComposeDBName || user != defaultComposeDBUser || pass != defaultComposeDBPass {
		t.Errorf("parseComposeDBConfig defaults = (%q, %q, %q), want (%q, %q, %q)",
			dbName, user, pass, defaultComposeDBName, defaultComposeDBUser, defaultComposeDBPass)
	}
}

func TestParseRedisConfig(t *testing.T) {
	dir := t.TempDir()
	resourcesDir := filepath.Join(dir, "EgovLogin", "src", "main", "resources")
	if err := os.MkdirAll(resourcesDir, 0o755); err != nil {
		t.Fatalf("failed to create resources dir: %v", err)
	}
	yaml := "spring:\n" +
		"  data:\n" +
		"    redis:\n" +
		"      host: localhost\n" +
		"      port: 6379\n" +
		"      password: rhdxhd12\n"
	if err := os.WriteFile(filepath.Join(resourcesDir, "application.yml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("failed to write application.yml: %v", err)
	}

	port, password := parseRedisConfig(dir)
	if port != 6379 || password != "rhdxhd12" {
		t.Errorf("parseRedisConfig = (%d, %q), want (6379, %q)", port, password, "rhdxhd12")
	}
}

func TestParseRedisConfigAbsentWhenMissing(t *testing.T) {
	dir := t.TempDir()
	port, password := parseRedisConfig(dir)
	if port != 0 || password != "" {
		t.Errorf("parseRedisConfig = (%d, %q), want (0, \"\")", port, password)
	}
}
