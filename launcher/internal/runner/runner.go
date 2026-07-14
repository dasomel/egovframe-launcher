// Package runner clones, builds and runs catalog targets as child processes,
// streaming merged stdout/stderr into a per-target log buffer.
package runner

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"egovframe-launcher/internal/catalog"
	"egovframe-launcher/internal/logbuf"
	"egovframe-launcher/internal/persist"
)

// catalogTargets is a package var so tests can substitute the catalog.
var catalogTargets = catalog.Targets

// catalogByID looks a target up in whatever catalog catalogTargets returns.
func catalogByID(id string) (catalog.Target, bool) {
	for _, t := range catalogTargets() {
		if t.ID == id {
			return t, true
		}
	}
	return catalog.Target{}, false
}

// autoInstallTomcatFn is a package variable so tests can bypass Tomcat auto-installation.
var autoInstallTomcatFn = autoInstallTomcat

type Status string

const (
	StatusIdle     Status = "idle"
	StatusCloning  Status = "cloning"
	StatusBuilding Status = "building"
	StatusRunning  Status = "running"
	StatusDone     Status = "done"
	StatusStopped  Status = "stopped"
	StatusError    Status = "error"
)

type State struct {
	ID       string `json:"id"`
	Status   Status `json:"status"`
	Port     int    `json:"port"`
	Err      string `json:"err,omitempty"`
	OpenPath string `json:"openPath,omitempty"`
}

type job struct {
	target     catalog.Target
	logs       *logbuf.Buf
	mu         sync.Mutex
	status     Status
	err        string
	stopping   bool
	cmd        *exec.Cmd   // current long-running process (run)
	depCmds    []*exec.Cmd // detached dependency services started before cmd (MSA chains)
	port       int
	openPath   string
	rspManaged bool
}

type Runner struct {
	workspace string
	skipTests bool
	javaHome  string
	mu        sync.Mutex
	jobs      map[string]*job
}

func New(workspace string) *Runner {
	cfg := persist.Load()

	// Workspace: persisted value overrides the argument (CLI flag is only the bootstrap default).
	if cfg.WorkspacePath != "" {
		workspace = cfg.WorkspacePath
	}

	// JavaHome: use persisted value, else auto-detect.
	jh := cfg.JavaHome
	if jh == "" {
		jh = defaultJavaHome()
	}

	skip := true // default
	if cfg.JavaHome != "" || cfg.WorkspacePath != "" {
		// Only use persisted skipTests if we have a saved config.
		skip = cfg.SkipTests
	}

	return &Runner{workspace: workspace, skipTests: skip, javaHome: jh, jobs: map[string]*job{}}
}

// defaultJavaHome picks JDK 17 if available, else the highest installed version.
func defaultJavaHome() string {
	jdks := DetectJDKs()
	for _, j := range jdks {
		if j.Version == 17 {
			return j.Home
		}
	}
	if len(jdks) > 0 {
		return jdks[len(jdks)-1].Home
	}
	return ""
}

func (r *Runner) jobFor(id string) (*job, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j, ok := r.jobs[id]; ok {
		return j, true
	}
	for _, tg := range catalogTargets() {
		if tg.ID == id {
			j := &job{target: tg, logs: logbuf.New(2000), status: StatusIdle}
			r.jobs[id] = j
			return j, true
		}
	}
	return nil, false
}

func (r *Runner) State(id string) State {
	j, ok := r.jobFor(id)
	if !ok {
		return State{ID: id, Status: StatusIdle}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	port := j.port
	if port == 0 {
		port = j.target.Port
	}
	return State{ID: id, Status: j.status, Port: port, Err: j.err, OpenPath: j.openPath}
}

func (r *Runner) Logs(id string) *logbuf.Buf {
	j, ok := r.jobFor(id)
	if !ok {
		return logbuf.New(1)
	}
	return j.logs
}

func (j *job) set(s Status, errMsg string) {
	j.mu.Lock()
	j.status = s
	j.err = errMsg
	j.mu.Unlock()
	if s == StatusError && errMsg != "" {
		errLine := "[error] " + errMsg
		if j.logs.LastLine() != errLine {
			j.logs.Append(errLine)
		}
	}
}

func (r *Runner) dir(id string) string {
	r.mu.Lock()
	ws := r.workspace
	r.mu.Unlock()
	return filepath.Join(ws, id)
}

func (r *Runner) Workspace() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.workspace
}

func (r *Runner) SetWorkspace(path string) {
	r.mu.Lock()
	r.workspace = path
	r.mu.Unlock()
	_ = persist.Update(func(c *persist.Config) { c.WorkspacePath = path })
}

func (r *Runner) SkipTests() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.skipTests
}

func (r *Runner) SetSkipTests(skip bool) {
	r.mu.Lock()
	r.skipTests = skip
	r.mu.Unlock()
	_ = persist.Update(func(c *persist.Config) { c.SkipTests = skip })
}

func (r *Runner) JavaHome() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.javaHome
}

func (r *Runner) SetJavaHome(home string) {
	r.mu.Lock()
	r.javaHome = home
	r.mu.Unlock()
	_ = persist.Update(func(c *persist.Config) { c.JavaHome = home })
}

// Do starts an action asynchronously. It only returns an error for invalid
// input (unknown target or action); execution failures surface via State.
func (r *Runner) Do(id, action string) error {
	return r.DoWithPort(id, action, 0)
}

func (r *Runner) DoWithOptions(id, action string, clean bool) error {
	j, ok := r.jobFor(id)
	if !ok {
		return fmt.Errorf("unknown target: %s", id)
	}
	switch action {
	case "clone":
		go r.runClone(j, clean)
	default:
		return r.DoWithPort(id, action, 0)
	}
	return nil
}

func (r *Runner) DoWithPort(id, action string, customPort int) error {
	j, ok := r.jobFor(id)
	if !ok {
		return fmt.Errorf("unknown target: %s", id)
	}
	switch action {
	case "build":
		if len(j.target.Build) == 0 {
			return fmt.Errorf("%s has no build step", id)
		}
		go r.runSteps(j, StatusBuilding, StatusDone, j.target.Build)
	case "run":
		if len(j.target.Run) == 0 {
			return fmt.Errorf("%s is not runnable", id)
		}
		go r.runLongWithPort(j, j.target.Run, customPort)
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
	return nil
}

// RunWAR builds the target's WAR, deploys it to an isolated per-target Tomcat
// instance (own CATALINA_BASE + own ports) and runs catalina foreground so logs
// stream and Stop() kills it. tomcatHome is the user-configured Apache Tomcat dir.
func (r *Runner) RunWAR(id, tomcatHome string, customPort int) error {
	j, ok := r.jobFor(id)
	if !ok {
		return fmt.Errorf("unknown target: %s", id)
	}
	go r.runWAR(j, tomcatHome, customPort)
	return nil
}

func (r *Runner) runWAR(j *job, tomcatHome string, customPort int) {
	// 0. Guard: only WAR deploy type.
	if j.target.DeployType != "war" {
		j.set(StatusError, "이 타깃은 WAR 배포 대상이 아닙니다")
		return
	}

	// 1. Validate and automatically install Tomcat if path is empty or invalid.
	var err error
	var catBin string
	if tomcatHome != "" {
		catBin, _ = tomcatCatalina(tomcatHome)
	}

	if tomcatHome == "" || catBin == "" {
		j.logs.Append("[info] Tomcat 설치 경로가 설정되지 않았거나 유효하지 않습니다. Tomcat 10.1.25 자동 설치를 진행합니다...")
		j.set(StatusBuilding, "Tomcat 자동 다운로드 중...")
		tomcatHome, err = autoInstallTomcatFn(j.logs)
		if err != nil {
			j.set(StatusError, "Tomcat 자동 설치 실패: "+err.Error())
			return
		}

		// Persistently update the setting
		_ = persist.Update(func(cfg *persist.Config) {
			cfg.TomcatPath = tomcatHome
		})
		j.logs.Append("[info] 새 Tomcat 경로를 설정에 저장했습니다: " + tomcatHome)

		// Reset state back to building to continue the flow
		j.set(StatusBuilding, "")
	} else {
		if _, err := os.Stat(catBin); err != nil {
			j.logs.Append("[info] 설정된 Tomcat 경로가 유효하지 않습니다. Tomcat 10.1.25 자동 설치를 다시 진행합니다...")
			j.set(StatusBuilding, "Tomcat 자동 다운로드 중...")
			tomcatHome, err = autoInstallTomcatFn(j.logs)
			if err != nil {
				j.set(StatusError, "Tomcat 자동 설치 실패: "+err.Error())
				return
			}
			_ = persist.Update(func(cfg *persist.Config) {
				cfg.TomcatPath = tomcatHome
			})
			j.logs.Append("[info] 새 Tomcat 경로를 설정에 저장했습니다: " + tomcatHome)
			j.set(StatusBuilding, "")
		}
	}

	// Ensure Tomcat bin scripts have execution permissions (necessary on Unix if unpacked without preserving permissions).
	if shFiles, err := filepath.Glob(filepath.Join(tomcatHome, "bin", "*.sh")); err == nil {
		for _, sh := range shFiles {
			_ = os.Chmod(sh, 0o755)
		}
	}

	// eGovFrame WAR 샘플은 Jakarta EE 10(jakarta.*) 기반이라 Tomcat 10.1+ 필요.
	// Tomcat 9 이하면 컨텍스트가 조용히 죽고 "/"가 404가 되므로 미리 차단.
	if mv := tomcatMajorVersion(tomcatHome); mv > 0 && mv < 10 {
		j.set(StatusError, fmt.Sprintf("이 WAR은 Jakarta EE 10 기반이라 Tomcat 10.1 이상이 필요합니다. 설정된 Tomcat이 %d.x 입니다 — 설정에서 Tomcat 10.1+ 경로로 변경하세요.", mv))
		return
	}

	// 2. Ensure source dir exists; run Build steps.
	dir := r.dir(j.target.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		j.set(StatusError, err.Error())
		return
	}
	j.set(StatusBuilding, "")
	patchJDBCScriptEncoding(dir, j.logs)

	// Automatically setup Docker DB container if project requires a database
	dbNeeded := needsDatabase(dir)
	if dbNeeded {
		j.logs.Append("[info] 프로젝트 DB 연동 필요 감지 — Docker MySQL 컨테이너 셋업 및 SQL 임포트를 진행합니다...")
		if err := setupProjectDatabase(dir, j.logs); err != nil {
			j.logs.Append("[warn] Docker DB 컨테이너 연동 경고: " + err.Error())
		} else {
			j.logs.Append("[success] Docker MySQL 컨테이너 셋업 및 데이터베이스 연동 완료")
		}
	}

	for _, c := range j.target.Build {
		if err := r.exec(j, c); err != nil {
			j.logs.Append("[error] " + err.Error())
			j.set(StatusError, err.Error())
			return
		}
	}

	// Ensure all built target/classes and WEB-INF/classes globals.properties
	// match Docker DB credentials. The "script" pattern is excluded: its
	// password is encrypted (egovPasswordResolver) and must not be rewritten.
	if dbNeeded {
		if kind, _, _, _ := detectDBPattern(dir); kind != "script" {
			_ = updateAllGlobalsProperties(dir, j.logs)
		}
	}

	// 3. Locate either Exploded WAR directory or built WAR.
	var explodedPath string
	if webInfMatches, err := filepath.Glob(filepath.Join(dir, "target", "*", "WEB-INF")); err == nil && len(webInfMatches) > 0 {
		explodedPath = filepath.Dir(webInfMatches[0])
		j.logs.Append(fmt.Sprintf("[info] Exploded WAR 디렉토리 감지됨: %s", filepath.Base(explodedPath)))
	}

	var warPath string
	if explodedPath == "" {
		matches, err := filepath.Glob(filepath.Join(dir, "target", "*.war"))
		if err != nil || len(matches) == 0 {
			j.set(StatusError, "WAR 산출물이나 Exploded 디렉토리를 찾을 수 없습니다")
			return
		}
		if len(matches) > 1 {
			j.logs.Append(fmt.Sprintf("[warn] WAR이 %d개 발견됨, 첫 번째 사용: %s", len(matches), filepath.Base(matches[0])))
		}
		warPath = matches[0]
	}

	// 4. Allocate two distinct free ports.
	basePort := j.target.Port
	if customPort > 0 {
		basePort = customPort
	}
	if basePort == 0 {
		basePort = 8080
	}
	httpPort := findAvailablePort(basePort)
	if httpPort != basePort {
		j.logs.Append(fmt.Sprintf("[warn] 요청 포트 %d 사용 중 → %d 포트로 기동합니다", basePort, httpPort))
	}
	shutdownPort := -1 // Disable Tomcat shutdown port to avoid conflicts

	j.mu.Lock()
	j.port = httpPort
	j.openPath = "/"
	j.stopping = false
	j.mu.Unlock()

	// 5. Build isolated CATALINA_BASE.
	base := filepath.Join(dir, ".catalina-base")
	for _, sub := range []string{"conf", "logs", "temp", "webapps", "work"} {
		if err := os.MkdirAll(filepath.Join(base, sub), 0o755); err != nil {
			j.set(StatusError, err.Error())
			return
		}
	}

	// Recursively copy <tomcatHome>/conf into <base>/conf (preserves subdirs like conf/Catalina/localhost).
	srcConf := filepath.Join(tomcatHome, "conf")
	if err := filepath.WalkDir(srcConf, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcConf, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(base, "conf", rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, src)
		return err
	}); err != nil {
		j.set(StatusError, "conf 파일 복사 실패: "+err.Error())
		return
	}

	// The shared Tomcat's console encoding may have been aligned to the RSP
	// backend charset (e.g. MS949); the launcher itself streams logs to a UTF-8
	// web UI, so force UTF-8 in this isolated copy.
	_ = setTomcatConsoleEncoding(filepath.Join(base, "conf"), "UTF-8")

	// Patch server.xml: replace shutdown port and first HTTP connector port.
	serverXML := filepath.Join(base, "conf", "server.xml")
	xmlData, err := os.ReadFile(serverXML)
	if err != nil {
		j.set(StatusError, "server.xml 읽기 실패: "+err.Error())
		return
	}
	xmlStr, err := patchServerXML(string(xmlData), httpPort, shutdownPort)
	if err != nil {
		j.set(StatusError, "server.xml 패치 실패: "+err.Error())
		return
	}
	if err := os.WriteFile(serverXML, []byte(xmlStr), 0o644); err != nil {
		j.set(StatusError, "server.xml 패치 실패: "+err.Error())
		return
	}

	// 5. Deploy application
	// Remove stale exploded webapps or context configurations
	_ = os.RemoveAll(filepath.Join(base, "webapps", "ROOT"))
	_ = os.RemoveAll(filepath.Join(base, "webapps", "ROOT.war"))

	contextConfDir := filepath.Join(base, "conf", "Catalina", "localhost")
	_ = os.RemoveAll(contextConfDir) // Clear existing configurations

	if explodedPath != "" {
		// Deploy via context XML referencing the exploded target directory
		if err := os.MkdirAll(contextConfDir, 0o755); err != nil {
			j.set(StatusError, "Context 디렉토리 생성 실패: "+err.Error())
			return
		}
		rootXML := filepath.Join(contextConfDir, "ROOT.xml")
		xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Context docBase="%s" reloadable="true">
</Context>`, filepath.ToSlash(explodedPath))

		if err := os.WriteFile(rootXML, []byte(xmlContent), 0o644); err != nil {
			j.set(StatusError, "ROOT.xml 컨텍스트 파일 생성 실패: "+err.Error())
			return
		}
		j.logs.Append(fmt.Sprintf("[info] Context XML 배포 등록 성공 (Exploded: %s)", filepath.Base(explodedPath)))
	} else {
		// Fallback: Stream WAR to <base>/webapps/ROOT.war (deploys at context path "/")
		srcWAR, err := os.Open(warPath)
		if err != nil {
			j.set(StatusError, "WAR 복사 실패: "+err.Error())
			return
		}
		defer srcWAR.Close()
		dstWAR, err := os.Create(filepath.Join(base, "webapps", "ROOT.war"))
		if err != nil {
			j.set(StatusError, "WAR 복사 실패: "+err.Error())
			return
		}
		defer dstWAR.Close()
		if _, err := io.Copy(dstWAR, srcWAR); err != nil {
			j.set(StatusError, "WAR 복사 실패: "+err.Error())
			return
		}
		j.logs.Append(fmt.Sprintf("[info] WAR 파일 배포 등록 성공 (%s)", filepath.Base(warPath)))
	}

	// 6. Start Tomcat in foreground; mirror runLongWithPort lifecycle.
	catBin, catArgs := tomcatCatalina(tomcatHome)
	cmd := exec.Command(catBin, catArgs...)
	cmd.Dir = tomcatHome

	r.mu.Lock()
	jh := r.javaHome
	r.mu.Unlock()

	env := append(
		os.Environ(),
		"CATALINA_HOME="+tomcatHome,
		"CATALINA_BASE="+base,
		"JAVA_OPTS=-Dfile.encoding=UTF-8 -Dfile.client.encoding=UTF-8 -Dclient.encoding.override=UTF-8",
		"CATALINA_OPTS=-Dfile.encoding=UTF-8 -Dfile.client.encoding=UTF-8 -Dclient.encoding.override=UTF-8",
	)
	if jh != "" {
		env = append(
			env,
			"JAVA_HOME="+jh,
			"PATH="+filepath.Join(jh, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
	}
	cmd.Env = env

	setProcAttr(cmd)
	pipe := pipeOutput(cmd, j.logs)
	if err := cmd.Start(); err != nil {
		j.set(StatusError, err.Error())
		return
	}

	j.mu.Lock()
	j.cmd = cmd
	j.status = StatusRunning
	j.mu.Unlock()

	waitErr := cmd.Wait()
	pipe()

	j.mu.Lock()
	wasStopped := j.stopping
	j.cmd = nil
	if wasStopped {
		j.status = StatusStopped
	} else if waitErr != nil {
		j.status, j.err = StatusError, waitErr.Error()
	} else {
		j.status = StatusDone
	}
	j.mu.Unlock()
}

// patchServerXML rewrites the shutdown port (8005) and the first HTTP connector
// port (8080) in a Tomcat server.xml. Returns an error if either default port
// string is not found (non-default Tomcat install).
// tomcatMajorVersion reads <home>/RELEASE-NOTES (present in standard Tomcat
// distributions) and returns the major version, or 0 if it can't be determined.
func tomcatMajorVersion(home string) int {
	data, err := os.ReadFile(filepath.Join(home, "RELEASE-NOTES"))
	if err != nil {
		return 0
	}
	const marker = "Apache Tomcat Version "
	i := strings.Index(string(data), marker)
	if i < 0 {
		return 0
	}
	rest := strings.TrimSpace(string(data)[i+len(marker):])
	dot := strings.IndexByte(rest, '.')
	if dot <= 0 {
		return 0
	}
	major, err := strconv.Atoi(strings.TrimSpace(rest[:dot]))
	if err != nil {
		return 0
	}
	return major
}

// cleanTomcatWebapps cleans up dangling application folders in tomcatHome/webapps
// to prevent Tomcat HostConfig from failing with NoSuchFileException during annotation scanning.
func cleanTomcatWebapps(tomcatHome string, logs *logbuf.Buf) {
	webappsDir := filepath.Join(tomcatHome, "webapps")
	entries, err := os.ReadDir(webappsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if name == "ROOT" || name == "docs" || name == "examples" || name == "manager" || name == "host-manager" {
			continue
		}
		target := filepath.Join(webappsDir, name)
		if logs != nil {
			logs.Append("[info] webapps 잔여 디렉터리 자동 정리: " + name)
		}
		_ = os.RemoveAll(target)
	}
}

// ensureTomcatUTF8Setenv writes/updates setenv.sh and setenv.bat in tomcatHome/bin
// to ensure all Tomcat invocations (including VSCode RSP Server Connector)
// use -Dfile.encoding=UTF-8 so Korean characters render correctly without corruption.
func ensureTomcatUTF8Setenv(tomcatHome string) {
	binDir := filepath.Join(tomcatHome, "bin")
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		return
	}

	shPath := filepath.Join(binDir, "setenv.sh")
	shContent := `export JAVA_OPTS="$JAVA_OPTS -Dfile.encoding=UTF-8 -Dfile.client.encoding=UTF-8 -Dclient.encoding.override=UTF-8"
export CATALINA_OPTS="$CATALINA_OPTS -Dfile.encoding=UTF-8 -Dfile.client.encoding=UTF-8 -Dclient.encoding.override=UTF-8"
`
	_ = os.WriteFile(shPath, []byte(shContent), 0o755)

	batPath := filepath.Join(binDir, "setenv.bat")
	batContent := `@echo off
set "JAVA_OPTS=%JAVA_OPTS% -Dfile.encoding=UTF-8 -Dfile.client.encoding=UTF-8 -Dclient.encoding.override=UTF-8"
set "CATALINA_OPTS=%CATALINA_OPTS% -Dfile.encoding=UTF-8 -Dfile.client.encoding=UTF-8 -Dclient.encoding.override=UTF-8"
`
	_ = os.WriteFile(batPath, []byte(batContent), 0o644)
}

// consoleEncodingRe matches the ConsoleHandler encoding line in Tomcat's
// conf/logging.properties.
var consoleEncodingRe = regexp.MustCompile(`(?m)^\s*java\.util\.logging\.ConsoleHandler\.encoding\s*=.*$`)

// setTomcatConsoleEncoding rewrites java.util.logging.ConsoleHandler.encoding in
// confDir/logging.properties to enc. File-handler encodings stay untouched so
// catalina.log etc. remain UTF-8.
func setTomcatConsoleEncoding(confDir, enc string) error {
	p := filepath.Join(confDir, "logging.properties")
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	line := "java.util.logging.ConsoleHandler.encoding = " + enc
	var out string
	if consoleEncodingRe.Match(data) {
		out = consoleEncodingRe.ReplaceAllString(string(data), line)
	} else {
		out = strings.TrimRight(string(data), "\n") + "\n" + line + "\n"
	}
	return os.WriteFile(p, []byte(out), 0o644)
}

// javaDefaultFileEncoding reports the default file.encoding of the JVM at
// javaHome ("" → java on PATH). On Korean Windows this is MS949 for JDK ≤ 17
// and UTF-8 for JDK 18+ (JEP 400); returns "" if it cannot be determined.
func javaDefaultFileEncoding(javaHome string) string {
	javaBin := "java"
	if javaHome != "" {
		javaBin = filepath.Join(javaHome, "bin", "java")
	}
	out, err := exec.Command(javaBin, "-XshowSettings:properties", "-version").CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "file.encoding ="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// alignTomcatConsoleEncodingWithRSP sets the shared Tomcat's console log
// encoding to the RSP backend JVM's default charset. The RSP backend decodes
// the Tomcat process stdout with its own default charset (MS949 on Korean
// Windows for JDK ≤ 17), so UTF-8 console output turns into mojibake in
// VSCode's server log view unless the two sides agree.
func alignTomcatConsoleEncodingWithRSP(tomcatHome, javaHome string, logs *logbuf.Buf) {
	enc := javaDefaultFileEncoding(javaHome)
	if enc == "" {
		return
	}
	if err := setTomcatConsoleEncoding(filepath.Join(tomcatHome, "conf"), enc); err != nil {
		logs.Append("[warn] Tomcat 콘솔 로그 인코딩 설정 실패: " + err.Error())
		return
	}
	if !strings.EqualFold(enc, "UTF-8") {
		logs.Append("[info] Tomcat 콘솔 로그 인코딩을 RSP 백엔드 문자셋(" + enc + ")에 맞췄습니다 (VSCode 한글 로그 깨짐 방지)")
	}
}

// jdbcScriptRe matches a Spring <jdbc:script .../> element opening tag.
var jdbcScriptRe = regexp.MustCompile(`<jdbc:script\b[^>]*>`)

// patchJDBCScriptEncoding adds encoding="UTF-8" to every <jdbc:script> element
// that lacks one, in all *.xml files under dir (source and build output alike).
// Spring's ResourceDatabasePopulator otherwise reads seed SQL with the JVM
// default charset — MS949 on Korean Windows for JDK ≤ 17 when Tomcat is
// launched by the RSP backend — which corrupts UTF-8 Korean seed data in
// embedded-database templates (e.g. the simple-homepage HSQLDB seed).
func patchJDBCScriptEncoding(dir string, logs *logbuf.Buf) {
	count := 0
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".xml") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || !strings.Contains(string(data), "<jdbc:script") {
			return nil
		}
		out := jdbcScriptRe.ReplaceAllStringFunc(string(data), func(m string) string {
			if strings.Contains(m, "encoding=") {
				return m
			}
			return strings.Replace(m, "<jdbc:script", `<jdbc:script encoding="UTF-8"`, 1)
		})
		if out != string(data) {
			if os.WriteFile(path, []byte(out), info.Mode()) == nil {
				count++
			}
		}
		return nil
	})
	if count > 0 {
		logs.Append(fmt.Sprintf("[info] %d개 XML의 <jdbc:script>에 encoding=\"UTF-8\"을 지정했습니다 (임베디드 DB 시드 한글 깨짐 방지)", count))
	}
}

// ensureTomcatHTTPPort rewrites the shared Tomcat's server.xml HTTP Connector
// port to desiredPort (or an available fallback port if desiredPort is occupied by an external process).
func ensureTomcatHTTPPort(tomcatHome string, desiredPort, rspPort int, serverID string, logs *logbuf.Buf) (int, error) {
	ensureTomcatUTF8Setenv(tomcatHome)
	cleanTomcatWebapps(tomcatHome, logs)

	serverXML := filepath.Join(tomcatHome, "conf", "server.xml")
	data, err := os.ReadFile(serverXML)
	if err != nil {
		return desiredPort, err
	}
	re := regexp.MustCompile(`(<Connector port=")(\d+)("\s+protocol="HTTP/1\.1")`)
	m := re.FindSubmatch(data)
	if m == nil {
		return desiredPort, fmt.Errorf("server.xml에서 HTTP Connector를 찾지 못했습니다")
	}
	currentPort, _ := strconv.Atoi(string(m[2]))

	targetPort := desiredPort

	// If target port is currently occupied by an active process, attempt Tomcat stop first
	if portListening(targetPort, 500*time.Millisecond) {
		logs.Append(fmt.Sprintf("[info] 포트 %d가 점유 중입니다. 실행 중인 Tomcat 정지를 시도합니다", targetPort))
		_ = stopTomcatCatalina(tomcatHome, logs)
		if rspPort > 0 {
			if c, err := dialRSP(rspPort); err == nil {
				_, _ = c.call("server/stopServerAsync", map[string]any{"id": serverID, "force": true}, 5*time.Second)
				_ = c.conn.Close()
			}
		}
		time.Sleep(1 * time.Second)
	}

	// If port is STILL listening after stopping Tomcat (external process collision), auto fallback
	if portListening(targetPort, 500*time.Millisecond) {
		fallback := findAvailablePort(targetPort + 1)
		if fallback > 0 {
			logs.Append(fmt.Sprintf("[warning] 포트 %d가 타 프로세스에 의해 사용 중입니다 → 빈 포트 %d로 자동 충돌 회피", targetPort, fallback))
			targetPort = fallback
		}
	}

	if currentPort == targetPort {
		return targetPort, nil
	}

	logs.Append(fmt.Sprintf("[info] Tomcat HTTP 포트를 %d → %d로 변경합니다", currentPort, targetPort))
	newData := re.ReplaceAll(data, []byte("${1}"+strconv.Itoa(targetPort)+"${3}"))
	if err := os.WriteFile(serverXML, newData, 0o644); err != nil {
		return currentPort, err
	}
	logs.Append(fmt.Sprintf("[success] server.xml Connector 포트를 %d로 변경했습니다", targetPort))
	return targetPort, nil
}

// serverXMLCommentRE matches XML comments so the HTTP connector search can
// skip over the shared Tomcat's commented-out "shared thread pool" Connector
// block, which the RSP feature leaves behind after rewriting the active
// Connector's port (see ensureTomcatHTTPPort). Without this, a literal
// port="8080" search matches the comment instead of the live connector.
var serverXMLCommentRE = regexp.MustCompile(`(?s)<!--.*?-->`)

var (
	serverXMLServerTagRE    = regexp.MustCompile(`<Server\b[^>]*>`)
	serverXMLConnectorTagRE = regexp.MustCompile(`<Connector\b[^>]*>`)
	serverXMLPortAttrRE     = regexp.MustCompile(`port="\d+"`)
)

// serverXMLReplacePort rewrites the (assumed single) port="N" attribute
// inside tag to newPort, returning an error if tag has no port attribute.
func serverXMLReplacePort(tag string, newPort int) (string, error) {
	if !serverXMLPortAttrRE.MatchString(tag) {
		return "", fmt.Errorf("port 속성을 찾을 수 없습니다")
	}
	return serverXMLPortAttrRE.ReplaceAllString(tag, fmt.Sprintf(`port="%d"`, newPort)), nil
}

// serverXMLEnsureAttr appends `name="value"` to tag just before its closing
// bracket, unless an attribute with that name is already present.
func serverXMLEnsureAttr(tag, name, value string) string {
	if strings.Contains(tag, name+"=") {
		return tag
	}
	insert := fmt.Sprintf(` %s="%s"`, name, value)
	if strings.HasSuffix(tag, "/>") {
		return tag[:len(tag)-2] + insert + "/>"
	}
	return tag[:len(tag)-1] + insert + ">"
}

// patchServerXML rewrites the <Server> shutdown port and the active HTTP
// Connector's port in a Tomcat server.xml, without assuming their current
// values. The RSP feature (ensureTomcatHTTPPort) rewrites the shared Tomcat's
// live Connector port away from the Tomcat-default 8080, and runWAR copies
// that shared conf into the project's isolated .catalina-base — so matching
// the literal string port="8080" no longer identifies the real connector; it
// can instead match the stock server.xml's commented-out thread-pool
// Connector block, silently leaving the live connector (and the actual bound
// port) unpatched. This version locates tags structurally instead.
func patchServerXML(xml string, httpPort, shutdownPort int) (string, error) {
	serverLoc := serverXMLServerTagRE.FindStringIndex(xml)
	if serverLoc == nil {
		return "", fmt.Errorf("server.xml에서 <Server> 태그를 찾을 수 없습니다 — 비표준 Tomcat 설치")
	}
	serverTag, err := serverXMLReplacePort(xml[serverLoc[0]:serverLoc[1]], shutdownPort)
	if err != nil {
		return "", fmt.Errorf("server.xml의 <Server> 태그에서 shutdown port를 찾을 수 없습니다 — 비표준 Tomcat 설치")
	}
	xml = xml[:serverLoc[0]] + serverTag + xml[serverLoc[1]:]

	commentRanges := serverXMLCommentRE.FindAllStringIndex(xml, -1)
	inComment := func(idx int) bool {
		for _, r := range commentRanges {
			if idx >= r[0] && idx < r[1] {
				return true
			}
		}
		return false
	}

	var connectorLoc []int
	for _, loc := range serverXMLConnectorTagRE.FindAllStringIndex(xml, -1) {
		if inComment(loc[0]) {
			continue
		}
		tag := xml[loc[0]:loc[1]]
		if !strings.Contains(tag, `protocol="HTTP/1.1"`) || strings.Contains(tag, `SSLEnabled="true"`) {
			continue
		}
		connectorLoc = loc
		break
	}
	if connectorLoc == nil {
		return "", fmt.Errorf("server.xml에서 사용 가능한 HTTP Connector를 찾을 수 없습니다 — 비표준 Tomcat 설치")
	}

	connectorTag, err := serverXMLReplacePort(xml[connectorLoc[0]:connectorLoc[1]], httpPort)
	if err != nil {
		return "", fmt.Errorf("server.xml의 HTTP Connector에서 port 속성을 찾을 수 없습니다 — 비표준 Tomcat 설치")
	}
	connectorTag = serverXMLEnsureAttr(connectorTag, "URIEncoding", "UTF-8")
	connectorTag = serverXMLEnsureAttr(connectorTag, "useBodyEncodingForURI", "true")
	xml = xml[:connectorLoc[0]] + connectorTag + xml[connectorLoc[1]:]

	return xml, nil
}

func (r *Runner) cloneCmds(t catalog.Target) []catalog.Command {
	args := []string{"clone", "--depth", "1"}
	if t.Branch != "" {
		args = append(args, "-b", t.Branch)
	}
	args = append(args, t.RepoURL, ".")
	return []catalog.Command{{Name: "git", Args: args}}
}

func (r *Runner) runClone(j *job, clean bool) {
	dir := r.dir(j.target.ID)

	exists := false
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		if files, err := os.ReadDir(dir); err == nil && len(files) > 0 {
			exists = true
		}
	}

	if exists && !clean {
		j.logs.Append("Using existing source code in " + dir)
		r.patchPomXml(dir)
		patchGradleSourcesJar(dir)
		patchCentralConfigEurekaPort(dir)
		needsDBCache.Delete(dir)
		j.logs.Append("[success] clone completed")
		j.set(StatusDone, "")
		return
	}

	if clean {
		_ = os.RemoveAll(dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		j.set(StatusError, err.Error())
		return
	}

	j.set(StatusCloning, "")
	for _, c := range r.cloneCmds(j.target) {
		if err := r.exec(j, c); err != nil {
			j.logs.Append("[error] " + err.Error())
			j.set(StatusError, err.Error())
			return
		}
	}
	r.patchPomXml(dir)
	patchGradleSourcesJar(dir)
	patchCentralConfigEurekaPort(dir)
	needsDBCache.Delete(dir)
	j.logs.Append("[success] clone completed")
	j.set(StatusDone, "")
}

// patchGradleSourcesJar fixes egovframe-msa-edu's Gradle builds. Both
// defects are confirmed upstream and fixed by
// https://github.com/eGovFramework/egovframe-msa-edu/pull/87 — these
// patches skip themselves once a clone already contains that fix, and can
// be removed entirely after #87 merges:
//  1. module-common: sourcesJar packs the generated querydsl dir without
//     declaring a dependency on compileJava, which Gradle 8 treats as an
//     error — append an explicit dependsOn.
//  2. logback-based services: egovframe-rte-fdl-logging transitively pulls
//     log4j-slf4j2-impl alongside Spring Boot's log4j-to-slf4j, which
//     aborts startup ("cannot be present with") — exclude the intruder.
func patchGradleSourcesJar(dir string) {
	mc := filepath.Join(dir, "backend", "egovframe-cloud-module-common", "build.gradle")
	if data, err := os.ReadFile(mc); err == nil {
		content := string(data)
		if strings.Contains(content, "withSourcesJar()") &&
			!strings.Contains(content, "egov-launcher: sourcesJar fix") &&
			!strings.Contains(content, "dependsOn tasks.named('compileJava')") {
			content += "\n/* egov-launcher: sourcesJar fix — Gradle 8 implicit-dependency validation (upstream PR #87) */\ntasks.named('sourcesJar') { dependsOn tasks.named('compileJava') }\n"
			_ = os.WriteFile(mc, []byte(content), 0o644)
		}
	}
	for _, svc := range []string{"user-service", "portal-service", "board-service"} {
		path := filepath.Join(dir, "backend", svc, "build.gradle")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		// Only skip when log4j-slf4j2-impl is already excluded. Upstream's
		// build.gradle excludes egovframe-rte-fdl-logging from its DIRECT rte
		// dependency but not from the module-common path — matching on that
		// name would falsely read as "already fixed" and leave the services
		// unbootable. Re-excluding an absent module is a harmless no-op, so
		// erring toward patching is safe once upstream PR #87 lands.
		if strings.Contains(content, "egov-launcher: log4j exclude") ||
			strings.Contains(content, "log4j-slf4j2-impl") {
			continue
		}
		content += "\n/* egov-launcher: log4j exclude — logback 사용 서비스에 log4j-slf4j2-impl이 섞이면 기동 불가 (upstream PR #87) */\nconfigurations.configureEach {\n    exclude group: 'org.apache.logging.log4j', module: 'log4j-slf4j2-impl'\n}\n"
		_ = os.WriteFile(path, []byte(content), 0o644)
	}
}

// patchCentralConfigEurekaPort rewrites egovframe-msa-edu's central Spring
// Cloud Config repo (<dir>/config/*.yml) so services register with Eureka on
// 8762 instead of the default 8761. Spring Cloud Config's remote properties
// override local env vars, so EUREKA_CLIENT_SERVICEURL_DEFAULTZONE alone
// cannot move this — the source file itself must change. This keeps msa-edu's
// Eureka off msa's native 8761, letting both stacks run at once. No-op if the
// dir doesn't exist; idempotent (safe to re-run on an existing clone).
func patchCentralConfigEurekaPort(dir string) {
	configDir := filepath.Join(dir, "config")
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		path := filepath.Join(configDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := string(data)
		if !strings.Contains(content, ":8761/eureka") {
			continue
		}
		content = strings.ReplaceAll(content, ":8761/eureka", ":8762/eureka")
		_ = os.WriteFile(path, []byte(content), 0o644)
	}
}

func (r *Runner) patchPomXml(dir string) {
	pomPath := filepath.Join(dir, "pom.xml")
	content, err := os.ReadFile(pomPath)
	if err != nil {
		return
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "lombok") ||
		!strings.Contains(contentStr, "maven-compiler-plugin") ||
		strings.Contains(contentStr, "annotationProcessorPaths") {
		return
	}

	targets := []string{
		`<release>${java.version}</release>`,
		`<source>1.8</source>
					<target>1.8</target>`,
		`<source>11</source>
					<target>11</target>`,
		`<source>17</source>
					<target>17</target>`,
	}

	patched := false
	for _, target := range targets {
		targetClean := strings.ReplaceAll(target, "\r", "")
		contentClean := strings.ReplaceAll(contentStr, "\r", "")
		if strings.Contains(contentClean, targetClean) {
			replacement := targetClean + `
					<annotationProcessorPaths>
						<path>
							<groupId>org.projectlombok</groupId>
							<artifactId>lombok</artifactId>
							<version>1.18.42</version>
						</path>
					</annotationProcessorPaths>`
			newContent := strings.Replace(contentClean, targetClean, replacement, 1)
			_ = os.WriteFile(pomPath, []byte(newContent), 0o644)
			patched = true
			break
		}
	}

	if !patched {
		idx := strings.Index(contentStr, "<artifactId>maven-compiler-plugin</artifactId>")
		if idx != -1 {
			configIdx := strings.Index(contentStr[idx:], "<configuration>")
			if configIdx != -1 {
				absConfigIdx := idx + configIdx + len("<configuration>")
				newContent := contentStr[:absConfigIdx] + `
					<annotationProcessorPaths>
						<path>
							<groupId>org.projectlombok</groupId>
							<artifactId>lombok</artifactId>
							<version>1.18.42</version>
						</path>
					</annotationProcessorPaths>` + contentStr[absConfigIdx:]
				_ = os.WriteFile(pomPath, []byte(newContent), 0o644)
			}
		}
	}
}

// runSteps executes commands sequentially to completion.
func (r *Runner) runSteps(j *job, active, final Status, cmds []catalog.Command) {
	j.set(active, "")
	for _, c := range cmds {
		if err := r.exec(j, c); err != nil {
			j.logs.Append("[error] " + err.Error())
			j.set(StatusError, err.Error())
			return
		}
	}
	if final == StatusDone {
		j.logs.Append("[success] build completed")
	}
	j.set(final, "")
}

// runLong executes all but the last command to completion, then keeps the last
// command running until Stop.
func findExecutableJar(dir string) string {
	patterns := []string{
		filepath.Join(dir, "GatewayServer", "target", "*.jar"),
		filepath.Join(dir, "apigateway-service", "target", "*.jar"),
		filepath.Join(dir, "target", "*.jar"),
		filepath.Join(dir, "*", "target", "*.jar"),
	}
	for _, p := range patterns {
		matches, err := filepath.Glob(p)
		if err == nil {
			for _, m := range matches {
				if !strings.HasSuffix(m, ".original") && !strings.Contains(m, "sources") && !strings.Contains(m, "javadoc") {
					rel, err := filepath.Rel(dir, m)
					if err == nil {
						return rel
					}
					return m
				}
			}
		}
	}
	return ""
}

// VSCodeCLI resolves the CLI entry point for a configured VS Code binary. On
// Windows the GUI binary (Code.exe) ignores CLI flags such as
// --list-extensions / --install-extension (it just opens a window), so the
// real CLI is the bin\code.cmd shim next to it. Returns codeBin unchanged
// when no shim is found.
func VSCodeCLI(codeBin string) string {
	if codeBin == "" {
		return "code"
	}
	if runtime.GOOS == "windows" && strings.EqualFold(filepath.Base(codeBin), "Code.exe") {
		shim := filepath.Join(filepath.Dir(codeBin), "bin", "code.cmd")
		if _, err := os.Stat(shim); err == nil {
			return shim
		}
	}
	return codeBin
}

// appendEnv appends kv to cmd.Env, defaulting cmd.Env to the current process
// environment first if it is unset (a nil Env would otherwise make the child
// process start with only kv and lose PATH/HOME/etc).
func appendEnv(cmd *exec.Cmd, kv ...string) {
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, kv...)
}

// setEnvWithout rebuilds cmd.Env from its current value — falling back to
// the process environment if unset, so a base of nil doesn't silently drop
// PATH/HOME — dropping any entry whose key starts with one of dropPrefixes,
// then appends kv.
func setEnvWithout(cmd *exec.Cmd, dropPrefixes []string, kv ...string) {
	base := cmd.Env
	if base == nil {
		base = os.Environ()
	}
	newEnv := make([]string, 0, len(base)+len(kv))
	for _, env := range base {
		drop := false
		for _, p := range dropPrefixes {
			if strings.HasPrefix(env, p) {
				drop = true
				break
			}
		}
		if !drop {
			newEnv = append(newEnv, env)
		}
	}
	cmd.Env = append(newEnv, kv...)
}

func (r *Runner) runLongWithPort(j *job, cmds []catalog.Command, customPort int) {
	if len(cmds) == 0 {
		dir := r.dir(j.target.ID)
		jarPath := findExecutableJar(dir)
		if jarPath == "" {
			j.set(StatusError, "기동할 실행 명령어(Run) 또는 JAR 파일을 찾을 수 없습니다")
			return
		}
		cmds = []catalog.Command{{Name: "java", Args: []string{"-jar", jarPath}}}
	}

	// Any npm command in the chain needs its node_modules (relative to the
	// command's own Dir) — a fresh clone has none, so run Build first.
	npmDepsMissing := false
	for _, c := range cmds {
		if c.Name != "npm" {
			continue
		}
		if _, err := os.Stat(filepath.Join(r.dir(j.target.ID), c.Dir, "node_modules")); os.IsNotExist(err) {
			npmDepsMissing = true
			break
		}
	}
	if npmDepsMissing {
		j.set(StatusBuilding, "")
		j.logs.Append("[info] node_modules 없음 — 의존성 설치를 먼저 실행합니다")
		installSteps := j.target.Build
		if len(installSteps) == 0 {
			installSteps = []catalog.Command{{Name: "npm", Args: []string{"ci"}}}
		}
		for _, c := range installSteps {
			if err := r.exec(j, c); err != nil {
				j.logs.Append("[error] " + err.Error())
				j.set(StatusError, err.Error())
				return
			}
		}
		j.logs.Append("[success] 의존성 설치 완료 — 서버를 기동합니다")
	}

	missingJar := false
scan:
	for _, c := range cmds {
		if c.Name != "java" {
			continue
		}
		for i, a := range c.Args {
			if a != "-jar" || i+1 >= len(c.Args) {
				continue
			}
			jarPath := c.Args[i+1]
			if !filepath.IsAbs(jarPath) {
				jarPath = filepath.Join(r.dir(j.target.ID), jarPath)
			}
			if _, err := os.Stat(jarPath); err != nil {
				missingJar = true
				break scan
			}
		}
	}
	if missingJar && len(j.target.Build) > 0 {
		j.set(StatusBuilding, "")
		j.logs.Append("[info] 빌드 산출물(jar) 없음 — 빌드를 먼저 실행합니다 (수 분 소요될 수 있습니다)")
		for _, c := range j.target.Build {
			if err := r.exec(j, c); err != nil {
				j.logs.Append("[error] " + err.Error())
				j.set(StatusError, err.Error())
				return
			}
		}
		j.logs.Append("[success] 빌드 완료 — 서비스를 기동합니다")
	}

	basePort := j.target.Port
	if customPort > 0 {
		basePort = customPort
	}
	port := findAvailablePort(basePort)
	if port != basePort {
		j.logs.Append(fmt.Sprintf("[warn] 요청 포트 %d 사용 중 → %d 포트로 기동합니다", basePort, port))
	}

	j.mu.Lock()
	j.port = port
	j.stopping = false
	j.depCmds = nil
	j.mu.Unlock()

	j.set(StatusBuilding, "")
	for i, c := range cmds[:len(cmds)-1] {
		if c.Name == "java" || strings.Contains(strings.Join(c.Args, " "), ".jar") {
			cmdStr := strings.Join(c.Args, " ")
			targetDepPort := c.Port

			if targetDepPort > 0 {
				freePort(targetDepPort)
				j.logs.Append(fmt.Sprintf("[info] 선행 dependency 서비스 기동 (%d/%d, 포트 %d 대기): %s %s", i+1, len(cmds)-1, targetDepPort, c.Name, cmdStr))
			} else {
				j.logs.Append(fmt.Sprintf("[info] 선행 서브 서비스 기동 (%d/%d): %s %s", i+1, len(cmds)-1, c.Name, cmdStr))
			}

			depCmd := r.command(j, c)
			// Dependency services must never inherit the job port meant for
			// the final process: mapped deps get their own port, unmapped
			// deps (random-port services) get no override at all —
			// otherwise SERVER_PORT makes them steal the gateway's port.
			var kv []string
			if targetDepPort > 0 {
				kv = append(kv, fmt.Sprintf("SERVER_PORT=%d", targetDepPort), fmt.Sprintf("PORT=%d", targetDepPort))
			}
			kv = append(kv, j.target.RunEnv...)
			setEnvWithout(depCmd, []string{"SERVER_PORT=", "PORT="}, kv...)

			setProcAttr(depCmd)
			_ = pipeOutput(depCmd, j.logs)
			if err := depCmd.Start(); err != nil {
				j.logs.Append(fmt.Sprintf("[warn] 선행 서비스 기동 실패: %s", err.Error()))
			} else {
				j.mu.Lock()
				j.depCmds = append(j.depCmds, depCmd)
				j.mu.Unlock()
				if targetDepPort > 0 {
					if waitForPort(targetDepPort, 20*time.Second) {
						j.logs.Append(fmt.Sprintf("[success] 선행 서비스 (포트 %d) 준비 완료", targetDepPort))
					} else {
						j.logs.Append(fmt.Sprintf("[warn] 선행 서비스 포트(%d) 대기 시간 초과, 계속 진행합니다", targetDepPort))
					}
				} else {
					time.Sleep(2 * time.Second)
				}
			}
		} else {
			if err := r.exec(j, c); err != nil {
				j.set(StatusError, err.Error())
				return
			}
		}
	}
	last := cmds[len(cmds)-1]
	var backendPort int
	if last.Name == "npm" && len(last.Args) > 0 && last.Args[0] == "run" {
		// vite만 CLI 인자로 포트를 받는다. Next 커스텀 서버 등은 PORT env
		// (r.command가 주입)를 읽으므로 인자를 덧붙이면 오히려 깨진다.
		if j.target.DeployType == "react" {
			last.Args = append(append([]string{}, last.Args...), "--", "--port", strconv.Itoa(port), "--strictPort")
			j.logs.Append(fmt.Sprintf("[info] dev 서버 포트 지정: --port %d", port))
		} else {
			j.logs.Append(fmt.Sprintf("[info] dev 서버 포트 지정(PORT env): %d", port))
		}

		// 백엔드가 이미 떠 있으면 그 실제 포트를, 아직이면 백엔드가 선언한
		// 기본 포트를 프록시 대상으로 넣는다. 프론트를 먼저 띄우는 순서에서도
		// (템플릿 vite.config에 하드코딩된 8080이 아니라) 카탈로그 포트를 향한다.
		if j.target.BackendID != "" {
			st := r.State(j.target.BackendID)
			switch {
			case st.Status == StatusRunning && st.Port > 0:
				backendPort = st.Port
				j.logs.Append(fmt.Sprintf("[info] 백엔드(%s) 실행 감지 — API 프록시 대상 반영: http://localhost:%d", j.target.BackendID, backendPort))
			default:
				if bt, ok := catalogByID(j.target.BackendID); ok && bt.Port > 0 {
					backendPort = bt.Port
				}
				j.logs.Append(fmt.Sprintf("[info] 백엔드(%s) 미실행 — 기본 포트(%d) 기준으로 프록시 대상 설정", j.target.BackendID, backendPort))
			}
		}
	}
	cmd := r.command(j, last)
	if backendPort > 0 {
		appendEnv(
			cmd,
			fmt.Sprintf("VITE_APP_API_PROXY_TARGET=http://localhost:%d", backendPort),
			fmt.Sprintf("VITE_APP_EGOV_CONTEXT_URL=localhost:%d", backendPort),
		)
	}
	if len(j.target.RunEnv) > 0 {
		appendEnv(cmd, j.target.RunEnv...)
	}
	setProcAttr(cmd)
	pipe := pipeOutput(cmd, j.logs)
	if err := cmd.Start(); err != nil {
		j.set(StatusError, err.Error())
		return
	}
	j.mu.Lock()
	j.cmd = cmd
	j.status = StatusRunning
	j.mu.Unlock()
	err := cmd.Wait()
	pipe()
	j.mu.Lock()
	wasStopped := j.stopping
	j.cmd = nil
	deps := j.depCmds
	j.depCmds = nil
	if wasStopped {
		j.status = StatusStopped
	} else {
		if err != nil {
			j.status, j.err = StatusError, err.Error()
		} else {
			j.status = StatusDone
		}
	}
	j.mu.Unlock()
	// The chain's dependency services are detached processes — reap them
	// whenever the main process ends so Stop (or a crash) doesn't leave
	// orphans logging forever.
	if len(deps) > 0 {
		j.logs.Append(fmt.Sprintf("[info] 선행 서비스 %d개 종료", len(deps)))
		for _, d := range deps {
			_ = killTree(d)
		}
	}
}

// winShellCommand rewrites catalog commands that assume a POSIX shell for
// Windows. Gradle-wrapper invocations declared as {sh, [.../gradlew, ...]}
// run via the sibling gradlew.bat (same wrapper, same project-dir semantics);
// other sh commands are left as-is and fail with a clear message.
func winShellCommand(c catalog.Command) catalog.Command {
	if runtime.GOOS != "windows" || c.Name != "sh" || len(c.Args) == 0 || !strings.HasSuffix(c.Args[0], "gradlew") {
		return c
	}
	bat := filepath.FromSlash(c.Args[0]) + ".bat"
	return catalog.Command{
		Name: "cmd",
		Args: append([]string{"/c", bat}, c.Args[1:]...),
		Dir:  c.Dir,
		Port: c.Port,
	}
}

func (r *Runner) command(j *job, c catalog.Command) *exec.Cmd {
	c = winShellCommand(c)
	cmd := exec.Command(c.Name, c.Args...)
	cmd.Dir = filepath.Join(r.dir(j.target.ID), c.Dir)
	j.mu.Lock()
	port := j.port
	j.mu.Unlock()

	baseEnv := os.Environ()
	var extra []string

	r.mu.Lock()
	jh := r.javaHome
	r.mu.Unlock()

	if jh != "" {
		extra = append(
			extra,
			"JAVA_HOME="+jh,
			"PATH="+filepath.Join(jh, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
	}
	if port > 0 {
		extra = append(
			extra,
			fmt.Sprintf("PORT=%d", port),
			fmt.Sprintf("SERVER_PORT=%d", port),
		)
	}
	if runtime.GOOS == "windows" {
		// Windows JVMs default to the ANSI codepage (e.g. MS949 on Korean
		// Windows), but the launcher streams child output to a UTF-8 web UI —
		// force UTF-8 on every JVM this command may spawn (mvn itself and any
		// forked app). stdout/stderr.encoding cover JDK 18+, where
		// file.encoding is already UTF-8 but redirected System.out still uses
		// the native encoding.
		const jvmUTF8 = "-Dfile.encoding=UTF-8 -Dstdout.encoding=UTF-8 -Dstderr.encoding=UTF-8"
		jto := jvmUTF8
		if prev := os.Getenv("JAVA_TOOL_OPTIONS"); prev != "" {
			jto = prev + " " + jvmUTF8
		}
		extra = append(extra, "JAVA_TOOL_OPTIONS="+jto)
	}
	if len(extra) > 0 {
		cmd.Env = append(baseEnv, extra...)
	}
	return cmd
}

func isPortAvailable(port int) bool {
	ln4, err := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return false
	}
	_ = ln4.Close()

	ln6, err := net.Listen("tcp6", fmt.Sprintf("[::]:%d", port))
	if err != nil {
		return false
	}
	_ = ln6.Close()

	return true
}

// portListening reports whether something is currently accepting TCP
// connections on 127.0.0.1:port (unlike isPortAvailable, which checks
// whether the port can be bound).
func waitForPort(port int, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if portListening(port, 300*time.Millisecond) {
			return true
		}
		time.Sleep(300 * time.Millisecond)
	}
	return false
}

// waitForHTTPReady polls http://127.0.0.1:port/path until a response other
// than 404/5xx arrives (a login redirect counts as ready). False on timeout.
func waitForHTTPReady(port int, path string, maxWait time.Duration) bool {
	client := &http.Client{
		Timeout: 3 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d%s", port, path))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound && resp.StatusCode < 500 {
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

func portListening(port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// stopTomcatCatalina stops the Tomcat at tomcatHome via its own catalina
// script — RSP's stopServerAsync doesn't reliably kill the process.
func stopTomcatCatalina(tomcatHome string, logs *logbuf.Buf) error {
	catBin, _ := tomcatCatalina(tomcatHome)
	cmd := exec.Command(catBin, "stop", "10", "-force")
	cmd.Env = append(os.Environ(), "CATALINA_HOME="+tomcatHome, "CATALINA_BASE="+tomcatHome)
	out, err := cmd.CombinedOutput()
	logs.Append("[info] catalina stop: " + strings.TrimSpace(string(out)))
	return err
}

func findAvailablePort(startPort int) int {
	for port := startPort; port < startPort+100; port++ {
		if isPortAvailable(port) {
			return port
		}
	}
	return startPort
}

// exec runs a single command to completion, streaming output.
func (r *Runner) exec(j *job, c catalog.Command) error {
	name := c.Name
	args := append([]string(nil), c.Args...)

	if name == "mvn" {
		// Auto-exclude EgovMobileId if project has EgovMobileId/pom.xml to bypass Raonsecure ZipException
		dir := r.dir(j.target.ID)
		if _, err := os.Stat(filepath.Join(dir, "EgovMobileId", "pom.xml")); err == nil {
			hasPl := false
			for _, arg := range args {
				if arg == "-pl" || strings.HasPrefix(arg, "-pl=") {
					hasPl = true
					break
				}
			}
			if !hasPl {
				args = append(args, "-pl", "!EgovMobileId")
				j.logs.Append("[info] EgovMobileId 모듈 감지 → 난독화 JAR 예외 방지 옵션(-pl !EgovMobileId)을 빌드에 자동 적용했습니다")
			}
		}

		hasSkip := false
		for _, arg := range args {
			if arg == "-DskipTests" {
				hasSkip = true
				break
			}
		}

		r.mu.Lock()
		skip := r.skipTests
		r.mu.Unlock()

		if skip && !hasSkip {
			args = append(args, "-DskipTests")
		} else if !skip && hasSkip {
			newArgs := make([]string, 0, len(args))
			for _, arg := range args {
				if arg != "-DskipTests" {
					newArgs = append(newArgs, arg)
				}
			}
			args = newArgs
		}
		c.Args = args
	}

	j.logs.Append(fmt.Sprintf("$ %s %s", c.Name, strings.Join(c.Args, " ")))
	cmd := r.command(j, c)
	setProcAttr(cmd)
	pipe := pipeOutput(cmd, j.logs)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Wait before closing outW: Go's internal copy goroutine writes to outW
	// until the child closes its stdout; closing outW first races with that.
	err := cmd.Wait()
	pipe()
	return err
}

// pipeOutput wires stdout+stderr into logs; returns a func that blocks until
// both streams are drained (call after Start).
func pipeOutput(cmd *exec.Cmd, logs *logbuf.Buf) func() {
	outR, outW := io.Pipe()
	cmd.Stdout = outW
	cmd.Stderr = outW
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer outR.Close()
		defer wg.Done()
		sc := bufio.NewScanner(outR)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			logs.Append(sc.Text())
		}
	}()
	return func() {
		_ = outW.Close()
		wg.Wait()
	}
}

// forEachRSPManagedJob invokes fn, with its lock held, on every job except
// exceptID that has rspManaged set.
func (r *Runner) forEachRSPManagedJob(exceptID string, fn func(*job)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, job := range r.jobs {
		if id == exceptID {
			continue
		}
		job.mu.Lock()
		if job.rspManaged {
			fn(job)
		}
		job.mu.Unlock()
	}
}

func (r *Runner) stopAllRSPManagedJobs(exceptID string) {
	r.forEachRSPManagedJob(exceptID, func(job *job) {
		job.status = StatusStopped
		job.port = 0
		job.openPath = ""
		job.rspManaged = false
		job.logs.Append("[info] 공유 Tomcat 정지로 인해 상태가 정지됨으로 동기화되었습니다.")
	})
}

func (r *Runner) syncRSPJobsPort(activeID string, newPort int) {
	r.forEachRSPManagedJob(activeID, func(job *job) {
		if job.status == StatusRunning {
			job.port = newPort
		}
	})
}

func (r *Runner) Stop(id string) error {
	j, ok := r.jobFor(id)
	if !ok {
		return fmt.Errorf("unknown target: %s", id)
	}
	j.mu.Lock()
	cmd := j.cmd
	rspManaged := j.rspManaged
	if cmd != nil {
		j.status = StatusStopped
		j.stopping = true
	}
	var deps []*exec.Cmd
	if cmd == nil && !rspManaged && len(j.depCmds) > 0 {
		// Stop pressed while the chain was still starting its dependency
		// services (before the final process launched) — reap them here;
		// the normal path reaps deps when the final process exits.
		deps = j.depCmds
		j.depCmds = nil
		j.status = StatusStopped
		j.stopping = true
	}
	j.mu.Unlock()
	if cmd != nil {
		return killTree(cmd)
	}
	if len(deps) > 0 {
		j.logs.Append(fmt.Sprintf("[info] 선행 서비스 %d개 종료", len(deps)))
		for _, d := range deps {
			_ = killTree(d)
		}
		return nil
	}
	if rspManaged {
		// 1. Best-effort, non-blocking: tell RSP the server is stopping.
		go func() {
			if rspPort, perr := findRSPPort(); perr == nil && rspPort > 0 {
				_ = rspStopServer(rspPort, rspServerID, j.logs)
			}
		}()

		// 2. Actually terminate Tomcat via its own catalina stop command.
		tomcatHome := persist.Load().TomcatPath
		if tomcatHome != "" {
			if err := stopTomcatCatalina(tomcatHome, j.logs); err != nil {
				j.logs.Append("[warn] Tomcat 정지 중 경고: " + err.Error())
			}

			// 3. Verify: poll whether the app port is still accepting connections.
			j.mu.Lock()
			appPort := j.port
			j.mu.Unlock()
			if appPort == 0 {
				appPort = j.target.Port
			}
			if appPort == 0 {
				appPort = 8080
			}
			stopped := false
			for i := 0; i < 8; i++ {
				if !portListening(appPort, 300*time.Millisecond) {
					stopped = true
					break
				}
				time.Sleep(500 * time.Millisecond)
			}
			if stopped {
				j.logs.Append("[success] Tomcat 정지 확인됨")
				j.markStopped()

				r.stopAllRSPManagedJobs(id)
			} else {
				j.logs.Append(fmt.Sprintf("[error] Tomcat 정지 실패: 포트 %d가 여전히 응답 중입니다", appPort))
				j.mu.Lock()
				j.status = StatusError
				j.err = fmt.Sprintf("Tomcat 정지 실패 (포트 %d가 여전히 응답 중)", appPort)
				j.mu.Unlock()
				return fmt.Errorf("Tomcat 정지 실패 (포트 %d가 여전히 응답 중)", appPort)
			}
		} else {
			j.markStopped()

			r.stopAllRSPManagedJobs(id)
		}
	}
	return nil
}

// markStopped resets a job to the stopped state, clearing its assigned
// port/openPath/rspManaged flag. Caller must not hold j.mu.
func (j *job) markStopped() {
	j.mu.Lock()
	j.status = StatusStopped
	j.port = 0
	j.openPath = ""
	j.rspManaged = false
	j.mu.Unlock()
}

func (r *Runner) OpenVSCode(id string, codeBin string) error {
	j, ok := r.jobFor(id)
	if !ok {
		return fmt.Errorf("unknown target: %s", id)
	}
	dir := r.dir(j.target.ID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return fmt.Errorf("target must be cloned first")
	}

	// Patch VSCode settings to configure java.home and rsp-ui java home before opening
	r.mu.Lock()
	jh := r.javaHome
	r.mu.Unlock()
	if jh != "" {
		_ = patchVSCodeSettings(dir, jh)
	}

	// Automatically configure VSCode Server Connector's deployables to use the Exploded WAR folder
	_ = patchRSPDeployables(dir)

	if codeBin == "" || codeBin == "Not Found (Add to PATH or specify)" {
		codeBin = "code"
	}

	// Launch detached via the OS-specific helper so the editor survives
	// independently of this launcher process (the macOS `code` CLI wrapper
	// otherwise ties a freshly-opened window to our process lifetime).
	return launchVSCode(codeBin, dir, jh)
}

func patchVSCodeSettings(projectDir string, javaHome string) error {
	vscodeDir := filepath.Join(projectDir, ".vscode")
	if err := os.MkdirAll(vscodeDir, 0o755); err != nil {
		return err
	}
	settingsPath := filepath.Join(vscodeDir, "settings.json")

	// Read existing settings
	settings := make(map[string]interface{})
	data, err := os.ReadFile(settingsPath)
	if err == nil {
		_ = json.Unmarshal(data, &settings)
	}

	// Normalize windows backslashes to forward slashes for VSCode settings paths
	cleanJH := filepath.ToSlash(javaHome)

	// Apply settings
	settings["rsp-ui.rsp.java.home"] = cleanJH
	settings["java.jdt.ls.java.home"] = cleanJH

	// Write back
	newData, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, newData, 0o644)
}

func patchRSPDeployables(projectDir string) error {
	// 1. Find if there is an exploded WAR directory under target/
	var explodedPath string
	if matches, err := filepath.Glob(filepath.Join(projectDir, "target", "*", "WEB-INF")); err == nil && len(matches) > 0 {
		explodedPath = filepath.Dir(matches[0])
	}
	if explodedPath == "" {
		return nil // If Maven package hasn't run yet, skip
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// RSP server configs are stored in ~/.rsp/redhat-community-server-connector/servers/*
	rspServersDir := filepath.Join(home, ".rsp", "redhat-community-server-connector", "servers")
	if _, err := os.Stat(rspServersDir); os.IsNotExist(err) {
		return nil
	}

	serverFiles, err := filepath.Glob(filepath.Join(rspServersDir, "*"))
	if err != nil || len(serverFiles) == 0 {
		return nil
	}

	for _, file := range serverFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var config map[string]interface{}
		if err := json.Unmarshal(data, &config); err != nil {
			continue
		}

		typeId, _ := config["org.jboss.tools.rsp.server.typeId"].(string)
		if !strings.Contains(typeId, "tomcat") && !strings.Contains(filepath.Base(file), "tomcat") {
			continue
		}

		deployables, ok := config["deployables"].(map[string]interface{})
		if !ok {
			deployables = make(map[string]interface{})
		}

		projectDirSlash := filepath.ToSlash(projectDir)
		explodedPathSlash := filepath.ToSlash(explodedPath)

		modified := false
		vmArgs, _ := config["args.vm.override"].(string)
		if !strings.Contains(vmArgs, "file.encoding=UTF-8") {
			config["args.vm.override"] = "-Dfile.encoding=UTF-8 -Dfile.client.encoding=UTF-8 -Dclient.encoding.override=UTF-8"
			modified = true
		}

		// Clean up any stale deployables that point to the projectDir itself (the root)
		for k := range deployables {
			kSlash := filepath.ToSlash(k)
			if kSlash == projectDirSlash || kSlash == projectDirSlash+"/" {
				delete(deployables, k)
				modified = true
			}
		}

		// Insert or update explodedPath in deployables
		if _, exists := deployables[explodedPathSlash]; !exists {
			deployables[explodedPathSlash] = map[string]interface{}{
				"label":   explodedPathSlash,
				"path":    explodedPathSlash,
				"options": map[string]interface{}{},
			}
			modified = true
		}

		if modified {
			config["deployables"] = deployables
			newData, err := json.MarshalIndent(config, "", "  ")
			if err == nil {
				_ = os.WriteFile(file, newData, 0o644)
			}
		}
	}

	return nil
}

func autoInstallTomcat(logs *logbuf.Buf) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	installDir := filepath.Join(home, ".egov-launcher")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return "", err
	}
	tomcatDirName := "apache-tomcat-10.1.25"
	targetHome := filepath.Join(installDir, tomcatDirName)

	// Check if catalina script already exists (meaning it's already installed)
	catBin, _ := tomcatCatalina(targetHome)
	if _, err := os.Stat(catBin); err == nil {
		logs.Append("[info] 이미 설치된 Tomcat 10.1.25가 존재합니다: " + targetHome)
		return targetHome, nil
	}

	zipURL := "https://archive.apache.org/dist/tomcat/tomcat-10/v10.1.25/bin/apache-tomcat-10.1.25.zip"
	logs.Append("[info] Tomcat 10.1.25 다운로드 시작: " + zipURL)

	resp, err := http.Get(zipURL)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: HTTP status %d", resp.StatusCode)
	}

	// Save to temp file
	tmpFile, err := os.CreateTemp("", "tomcat-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	logs.Append("[info] Tomcat zip 파일 서버에서 수신 중...")
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return "", fmt.Errorf("failed to save downloaded zip: %w", err)
	}

	logs.Append("[info] Tomcat zip 압축 해제 중: " + targetHome)
	if err := unzip(tmpFile.Name(), installDir); err != nil {
		return "", fmt.Errorf("failed to unzip Tomcat: %w", err)
	}

	// Make sure bin/*.sh are executable on Unix
	if runtime.GOOS != "windows" {
		if shFiles, err := filepath.Glob(filepath.Join(targetHome, "bin", "*.sh")); err == nil {
			for _, sh := range shFiles {
				_ = os.Chmod(sh, 0o755)
			}
		}
	}

	logs.Append("[info] Tomcat 10.1.25 설치 및 실행 준비 완료: " + targetHome)
	return targetHome, nil
}

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		// Check for Zip Slip vulnerability
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}
	return nil
}

// --- RSP (VSCode Community Server Connectors) 서버 자동 등록 ---

// findExplodedWar returns the exploded WAR directory under <projectDir>/target
// (the folder containing WEB-INF), or "" if not built yet.
func findExplodedWar(projectDir string) string {
	if matches, err := filepath.Glob(filepath.Join(projectDir, "target", "*", "WEB-INF")); err == nil && len(matches) > 0 {
		return filepath.Dir(matches[0])
	}
	return ""
}

// rspTomcatTypeID maps the Tomcat major version to the CSC server type id.
func rspTomcatTypeID(tomcatHome string) string {
	if tomcatMajorVersion(tomcatHome) == 11 {
		return "org.jboss.ide.eclipse.as.server.tomcat.110"
	}
	return "org.jboss.ide.eclipse.as.server.tomcat.100"
}

// createRSPTomcatServer writes a Community Server Connectors server config file
// so a Tomcat server appears in the VSCode RSP "Servers" view without manual
// setup. Returns the server id and whether a new server was created (false if a
// tomcat server already existed). RSP loads servers/ at activation, so VSCode
// must (re)start the RSP server to pick up a freshly written file.
func createRSPTomcatServer(tomcatHome, explodedPath string) (string, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	serversDir := filepath.Join(home, ".rsp", "redhat-community-server-connector", "servers")
	if err := os.MkdirAll(serversDir, 0o755); err != nil {
		return "", false, err
	}

	// Reuse an existing Tomcat server if one is already configured.
	if files, _ := filepath.Glob(filepath.Join(serversDir, "*")); len(files) > 0 {
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var c map[string]interface{}
			if json.Unmarshal(data, &c) == nil {
				if tid, _ := c["org.jboss.tools.rsp.server.typeId"].(string); strings.Contains(tid, "tomcat") {
					return strings.TrimSuffix(filepath.Base(f), ".json"), false, nil
				}
			}
		}
	}

	id := "egovframe-tomcat"
	// Match the exact shape RSP's ServerModel writes (decompiled from
	// org.jboss.tools.rsp.server): flat string attributes incl. "id-set", plus a
	// "deployables" child keyed by the deployable label.
	cfg := map[string]interface{}{
		"id":                                id,
		"id-set":                            "true",
		"org.jboss.tools.rsp.server.typeId": rspTomcatTypeID(tomcatHome),
		"server.home.dir":                   filepath.ToSlash(tomcatHome),
		"server.base.dir":                   filepath.ToSlash(tomcatHome),
	}
	if explodedPath != "" {
		ep := filepath.ToSlash(explodedPath)
		label := filepath.Base(explodedPath)
		cfg["deployables"] = map[string]interface{}{
			label: map[string]interface{}{"label": label, "path": ep, "options": map[string]interface{}{}},
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", false, err
	}
	if err := os.WriteFile(filepath.Join(serversDir, id), data, 0o644); err != nil {
		return "", false, err
	}
	return id, true, nil
}

// SetupRSP resolves/auto-installs Tomcat, ensures the WAR is built, writes an
// RSP Tomcat server config, then opens the project in VSCode. Async; progress
// surfaces via the job's status/logs.
func (r *Runner) SetupRSP(id string) error {
	j, ok := r.jobFor(id)
	if !ok {
		return fmt.Errorf("unknown target: %s", id)
	}
	go r.setupRSP(j)
	return nil
}

func (r *Runner) setupRSP(j *job) {
	if j.target.DeployType != "war" {
		j.set(StatusError, "WAR 타깃만 RSP 등록 대상입니다")
		return
	}
	dir := r.dir(j.target.ID)
	if _, err := os.Stat(dir); err != nil {
		j.set(StatusError, "먼저 Clone 하세요")
		return
	}

	cfg := persist.Load()
	tomcatHome := cfg.TomcatPath
	catBin := ""
	if tomcatHome != "" {
		catBin, _ = tomcatCatalina(tomcatHome)
	}
	var err error
	if tomcatHome == "" || catBin == "" {
		j.logs.Append("[info] Tomcat 경로 미설정/무효 → 자동 설치를 진행합니다...")
		j.set(StatusBuilding, "Tomcat 자동 다운로드 중...")
		tomcatHome, err = autoInstallTomcatFn(j.logs)
		if err != nil {
			j.set(StatusError, "Tomcat 자동 설치 실패: "+err.Error())
			return
		}
		_ = persist.Update(func(c *persist.Config) { c.TomcatPath = tomcatHome })
		j.logs.Append("[info] 새 Tomcat 경로를 설정에 저장했습니다: " + tomcatHome)
	}
	if mv := tomcatMajorVersion(tomcatHome); mv > 0 && mv < 10 {
		j.set(StatusError, fmt.Sprintf("이 WAR은 Jakarta EE 10 기반이라 Tomcat 10.1+ 필요. 현재 %d.x", mv))
		return
	}

	r.mu.Lock()
	rspJH := r.javaHome
	r.mu.Unlock()
	alignTomcatConsoleEncodingWithRSP(tomcatHome, rspJH, j.logs)
	patchJDBCScriptEncoding(dir, j.logs)

	// Ensure the exploded WAR exists (build if needed) so it can be added as a deployable.
	j.set(StatusBuilding, "")
	explodedPath := findExplodedWar(dir)
	if explodedPath == "" {
		j.logs.Append("[info] exploded WAR이 없어 빌드를 실행합니다...")
		for _, c := range j.target.Build {
			if err := r.exec(j, c); err != nil {
				j.logs.Append("[error] " + err.Error())
				j.set(StatusError, err.Error())
				return
			}
		}
		explodedPath = findExplodedWar(dir)
	}
	if explodedPath == "" {
		j.logs.Append("[warn] exploded WAR 폴더를 찾지 못했습니다 — 배포물 없이 서버만 등록합니다.")
	}

	// --- RSP API 자동화 경로 (RSP 백엔드가 실행 중인 경우) ---
	if explodedPath != "" {
		port, perr := findRSPPort()
		if perr != nil || port <= 0 {
			j.logs.Append("[info] RSP 백엔드 미감지 — 파일 방식으로 진행합니다 (VSCode에서 RSP가 켜져 있어야 자동 기동됩니다)")
		}
		if perr == nil && port > 0 {
			j.logs.Append(fmt.Sprintf("[info] RSP 백엔드 감지 (port %d) — 생성·기동·배포를 자동화합니다", port))
			serverID := rspServerID
			typeID := rspTomcatTypeID(tomcatHome)
			label := filepath.Base(explodedPath)
			desiredPort := cfg.RSPTomcatPort
			if desiredPort == 0 {
				desiredPort = 8080
			}
			appPort, perr := ensureTomcatHTTPPort(tomcatHome, desiredPort, port, serverID, j.logs)
			if perr != nil {
				j.logs.Append(fmt.Sprintf("[warn] Tomcat 포트 변경 실패, 기존 포트(%d)로 진행합니다: %s", appPort, perr.Error()))
			}
			if rspErr := rspDeployAndStart(port, serverID, typeID, tomcatHome, label, explodedPath, j.logs); rspErr == nil {
				j.mu.Lock()
				j.port = appPort
				j.openPath = "/" + label + "/"
				j.rspManaged = true
				j.mu.Unlock()
				r.syncRSPJobsPort(j.target.ID, appPort)
				url := fmt.Sprintf("http://localhost:%d/%s/", appPort, label)
				j.logs.Append("[success] RSP 자동 기동·배포 완료 → " + url)
				// 배포 레이스 보정: autoDeploy가 복사 도중 컨텍스트를 시작하면
				// 클래스 누락(NoClassDefFoundError)으로 컨텍스트가 죽은 채 404가
				// 남는다. 응답이 없으면 publish를 한 번 더 실행해 재배포시킨다.
				if !waitForHTTPReady(appPort, "/"+label+"/", 20*time.Second) {
					j.logs.Append("[warn] 앱이 아직 응답하지 않습니다 — 배포 레이스 보정을 위해 publish를 재시도합니다")
					if perr := rspPublish(port, serverID, j.logs); perr != nil {
						j.logs.Append("[warn] publish 재시도 실패: " + perr.Error())
					} else if waitForHTTPReady(appPort, "/"+label+"/", 40*time.Second) {
						j.logs.Append("[success] 재배포 후 앱 응답 확인")
					} else {
						j.logs.Append("[warn] 앱이 여전히 응답하지 않습니다 — 로그에서 애플리케이션 기동 오류를 확인하세요")
					}
				}
				codeBin := cfg.VSCodePath
				if codeBin == "" || codeBin == "Not Found (Add to PATH or specify)" {
					codeBin = "code"
				}
				r.mu.Lock()
				jh := r.javaHome
				r.mu.Unlock()
				if jh != "" {
					_ = patchVSCodeSettings(r.dir(j.target.ID), jh)
				}
				_ = launchVSCode(codeBin, r.dir(j.target.ID), jh)
				j.set(StatusRunning, "")
				return
			} else {
				j.logs.Append("[warn] RSP API 자동화 실패, 파일 방식으로 폴백합니다: " + rspErr.Error())
			}
		}
	}

	// --- 파일 방식 폴백 ---
	sid, created, err := createRSPTomcatServer(tomcatHome, explodedPath)
	if err != nil {
		j.set(StatusError, "RSP 서버 생성 실패: "+err.Error())
		return
	}
	if created {
		j.logs.Append("[success] RSP Tomcat 서버 '" + sid + "' 생성 완료 (home: " + tomcatHome + ")")
	} else {
		_ = patchRSPDeployables(dir)
		j.logs.Append("[info] 기존 RSP Tomcat 서버 사용: " + sid + " (deployables 갱신)")
	}
	j.logs.Append("[info] VSCode를 엽니다. RSP가 이미 실행 중이면 'Developer: Reload Window' 후 Servers 패널에서 서버 → Start 하세요.")

	r.mu.Lock()
	jh := r.javaHome
	r.mu.Unlock()
	if jh != "" {
		_ = patchVSCodeSettings(dir, jh)
	}
	codeBin := cfg.VSCodePath
	if codeBin == "" || codeBin == "Not Found (Add to PATH or specify)" {
		codeBin = "code"
	}
	_ = launchVSCode(codeBin, dir, jh)
	j.set(StatusDone, "")
}

// --- DB (Docker MySQL) 자동 설정 ---

// findGlobalsProperties locates globals.properties under dir, skipping
// build-output and VCS directories. Returns "" if not found.
func findGlobalsProperties(dir string) string {
	var found string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "target" || base == ".git" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if info.Name() == "globals.properties" {
			found = path
		}
		return nil
	})
	return found
}

// detectDBPattern sniffs which of the three eGovFrame DB layouts dir uses:
//   - "war" – DATABASE/mysql/*.sql alongside a globals.properties file (the
//     eGovFrame templates that require an external MySQL instance to boot).
//   - "script" – script/ddl/mysql/*.sql (+ script/dml/mysql) alongside a
//     globals.properties (egovframe-common-components; encrypted password,
//     shared "com" schema — see setupScriptDatabase).
//   - "compose" – docker-compose/mysql/init/*.sql (eGovFrame MSA targets
//     whose services expect a pre-seeded shared MySQL schema).
//   - "bootprops" – DATABASE/all_<schema>_ddl_mysql.sql paired with a Spring
//     Boot application.properties booting against Globals.DbType (the
//     egovframe-template-simple-backend pattern).
//
// Returns kind == "" when dir matches none of them. sqlDir/schema/propsPath
// are populated only for the patterns that use them ("bootprops" is the only
// one needing schema/propsPath).
func detectDBPattern(dir string) (kind, sqlDir, schema, propsPath string) {
	warSQLDir := filepath.Join(dir, "DATABASE", "mysql")
	if entries, err := os.ReadDir(warSQLDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
				if findGlobalsProperties(dir) != "" {
					return "war", warSQLDir, "", ""
				}
				break
			}
		}
	}
	scriptDDLDir := filepath.Join(dir, "script", "ddl", "mysql")
	if entries, err := os.ReadDir(scriptDDLDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
				if findGlobalsProperties(dir) != "" {
					return "script", filepath.Join(dir, "script"), "", ""
				}
				break
			}
		}
	}
	if initDir := composeInitSQLDir(dir); initDir != "" {
		return "compose", initDir, "", ""
	}
	if sd, sch, props := bootPropsDBInfo(dir); sd != "" {
		return "bootprops", sd, sch, props
	}
	return "", "", "", ""
}

// needsDatabase reports whether dir requires an external MySQL instance to
// boot, per detectDBPattern.
func needsDatabase(dir string) bool {
	kind, _, _, _ := detectDBPattern(dir)
	return kind != ""
}

// composeInitSQLDir returns <dir>/docker-compose/mysql/init if it contains
// at least one *.sql file (the eGovFrame MSA init-script layout), else "".
func composeInitSQLDir(dir string) string {
	initDir := filepath.Join(dir, "docker-compose", "mysql", "init")
	entries, err := os.ReadDir(initDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
			return initDir
		}
	}
	return ""
}

// bootPropsDDLRe matches the DDL filename convention used by the
// egovframe-template-simple-backend DATABASE dir, e.g.
// "all_sht_ddl_mysql.sql" -> schema "sht".
var bootPropsDDLRe = regexp.MustCompile(`^all_([A-Za-z0-9_]+)_ddl_mysql\.sql$`)

// bootPropsDBInfo detects the "boot properties" DB pattern used by
// egovframe-template-simple-backend: MySQL DDL/DATA scripts directly under
// <dir>/DATABASE (no mysql/ subdirectory), paired with a Spring Boot
// application.properties that boots against Globals.DbType by default.
// Returns non-empty values only when both are present.
func bootPropsDBInfo(dir string) (sqlDir, schema, propsPath string) {
	dbDir := filepath.Join(dir, "DATABASE")
	matches, err := filepath.Glob(filepath.Join(dbDir, "all_*_ddl_mysql.sql"))
	if err != nil || len(matches) == 0 {
		return "", "", ""
	}
	m := bootPropsDDLRe.FindStringSubmatch(filepath.Base(matches[0]))
	if m == nil {
		return "", "", ""
	}
	props := filepath.Join(dir, "src", "main", "resources", "application.properties")
	data, err := os.ReadFile(props)
	if err != nil || !strings.Contains(string(data), "Globals.DbType") {
		return "", "", ""
	}
	return dbDir, m[1], props
}

// needsDBCache memoizes needsDatabase per project dir — the answer only
// changes on (re-)clone, and the walk is too expensive for the UI's 1.5s
// target-list polling. Invalidated by runClone.
var needsDBCache sync.Map

// NeedsDatabase reports whether the project at dir requires a MySQL
// instance to boot (see needsDatabase). Exported for use by the server
// package when building the target list view.
func NeedsDatabase(dir string) bool {
	if v, ok := needsDBCache.Load(dir); ok {
		return v.(bool)
	}
	v := needsDatabase(dir)
	needsDBCache.Store(dir, v)
	return v
}

// parseMySQLDBName extracts the database name from globals.properties'
// active (uncommented) Globals.Url line, e.g.
// "Globals.Url = jdbc:log4jdbc:mysql://127.0.0.1:3306/pst" -> "pst".
func parseMySQLDBName(globalsPropsPath string) (string, error) {
	data, err := os.ReadFile(globalsPropsPath)
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`(?m)^Globals\.(?:[A-Za-z0-9_]+\.)?Url\s*=\s*jdbc:.*mysql://[^/]+/([A-Za-z0-9_]+)`)
	m := re.FindSubmatch(data)
	if m == nil {
		return "", fmt.Errorf("globals.properties에서 MySQL DB 이름을 찾지 못했습니다")
	}
	return string(m[1]), nil
}

// defaultComposeDBName/User/Pass are the eGovFrame MSA common-component
// schema/credentials, used when application-local.yml is absent or a field
// can't be parsed out of it.
const (
	defaultComposeDBName = "com"
	defaultComposeDBUser = "com"
	defaultComposeDBPass = "com01"
)

var (
	composeDBURLRe  = regexp.MustCompile(`url:\s*jdbc:mysql://[^/]+/([A-Za-z0-9_]+)`)
	composeDBUserRe = regexp.MustCompile(`username:\s*(\S+)`)
	composeDBPassRe = regexp.MustCompile(`password:\s*(\S+)`)
)

// parseComposeDBConfig extracts the datasource schema/credentials that the
// MSA services expect at runtime from
// <dir>/ConfigServer/src/main/resources/config/application-local.yml.
// Falls back to the eGovFrame MSA defaults (com/com/com01) for any value
// that's missing or can't be parsed — the file only ships one datasource
// block, so a whole-file regex is sufficient.
func parseComposeDBConfig(dir string) (dbName, user, pass string) {
	dbName, user, pass = defaultComposeDBName, defaultComposeDBUser, defaultComposeDBPass
	path := filepath.Join(dir, "ConfigServer", "src", "main", "resources", "config", "application-local.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return dbName, user, pass
	}
	if m := composeDBURLRe.FindSubmatch(data); m != nil {
		dbName = string(m[1])
	}
	if m := composeDBUserRe.FindSubmatch(data); m != nil {
		user = string(m[1])
	}
	if m := composeDBPassRe.FindSubmatch(data); m != nil {
		pass = string(m[1])
	}
	return dbName, user, pass
}

var (
	composeMySQLUserRe = regexp.MustCompile(`(?im)^\s*MYSQL_USER:\s*(\S+)\s*$`)
	composeMySQLPassRe = regexp.MustCompile(`(?im)^\s*MYSQL_PASSWORD:\s*(\S+)\s*$`)
)

// parseComposeMySQLEnvUser extracts the MYSQL_USER/MYSQL_PASSWORD env values
// from <dockerComposeDir>/docker-compose.yml, if present. Self-managing init
// scripts (see initSQLSelfManagesSchema) sometimes GRANT privileges to a user
// that the official mysql image would normally auto-create from these env
// vars on first boot — since the shared launcher container was created
// independently of that compose file, that user never exists unless we
// create it ourselves.
func parseComposeMySQLEnvUser(dockerComposeDir string) (user, pass string) {
	data, err := os.ReadFile(filepath.Join(dockerComposeDir, "docker-compose.yml"))
	if err != nil {
		return "", ""
	}
	if m := composeMySQLUserRe.FindSubmatch(data); m != nil {
		user = string(m[1])
	}
	if m := composeMySQLPassRe.FindSubmatch(data); m != nil {
		pass = string(m[1])
	}
	return user, pass
}

// composeRabbitMQNeeded reports whether the MSA target's native config-repo
// files (<dir>/config/*.yml) or any service's application.yml
// (<dir>/*/src/main/resources/application.yml) declare a "rabbitmq:" block.
func composeRabbitMQNeeded(dir string) bool {
	patterns := []string{
		filepath.Join(dir, "config", "*.yml"),
		filepath.Join(dir, "*", "src", "main", "resources", "application.yml"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if strings.Contains(string(data), "rabbitmq:") {
				return true
			}
		}
	}
	return false
}

var (
	redisKeyRe  = regexp.MustCompile(`^(\s*)redis:\s*$`)
	redisPortRe = regexp.MustCompile(`^\s*port:\s*(\d+)\s*$`)
	redisPassRe = regexp.MustCompile(`^\s*password:\s*(\S+)\s*$`)
)

// extractRedisBlock scans content line by line for a "redis:" mapping key
// and returns the port/password nested beneath it (matched by indentation),
// stopping at the first non-blank line indented at or below the "redis:"
// key itself. ok is false when no "redis:" key with a port is found.
func extractRedisBlock(content string) (port int, password string, ok bool) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		m := redisKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		baseIndent := len(m[1])
		for _, sub := range lines[i+1:] {
			if strings.TrimSpace(sub) == "" {
				continue
			}
			indent := len(sub) - len(strings.TrimLeft(sub, " \t"))
			if indent <= baseIndent {
				break
			}
			if pm := redisPortRe.FindStringSubmatch(sub); pm != nil {
				port, _ = strconv.Atoi(pm[1])
			}
			if pwm := redisPassRe.FindStringSubmatch(sub); pwm != nil {
				password = pwm[1]
			}
		}
		if port > 0 {
			return port, password, true
		}
	}
	return 0, "", false
}

// parseRedisConfig scans each MSA service's application.yml
// (<dir>/*/src/main/resources/application.yml) for a "redis:" block (e.g.
// EgovLogin's spring.data.redis config) and returns its port/password.
// Returns (0, "") when no service declares one.
func parseRedisConfig(dir string) (port int, password string) {
	matches, err := filepath.Glob(filepath.Join(dir, "*", "src", "main", "resources", "application.yml"))
	if err != nil {
		return 0, ""
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if p, pw, ok := extractRedisBlock(string(data)); ok {
			return p, pw
		}
	}
	return 0, ""
}

var dbSetupMu sync.Mutex

const (
	launcherMySQLContainer = "egov-launcher-mysql"
	launcherMySQLRootPass  = "egovlauncher"
	launcherMySQLPort      = 3306
	launcherRedisContainer = "egov-launcher-redis"
)

const launcherRabbitContainer = "egov-launcher-rabbitmq"

// containerSpec describes a single named Docker container for
// ensureDockerContainer: how to create it, how to tell it's ready, and how
// to recover it from a crashed state.
type containerSpec struct {
	name         string
	displayName  string
	image        string
	runArgs      []string
	ready        func() bool
	timeout      time.Duration
	pollInterval time.Duration
	notReadyErr  string
	// hostPort is the published host port. When > 0 and the container is not
	// already running, a pre-flight check fails fast with a clear message if
	// another process (e.g. a locally installed MySQL) holds the port, instead
	// of surfacing docker's raw bind error after an image pull.
	hostPort int
	// recreateOnCrash, when true, replaces (docker rm -f -v + recreate)
	// a container found stopped with a non-zero exit code instead of
	// `docker start`-ing it, since some crash-loop causes (e.g. a
	// poisoned anonymous volume) aren't fixed by a plain restart. Must stay
	// false for containers whose data must not be wiped automatically.
	recreateOnCrash bool
	// afterStart is called once right after the container is created or
	// (re)started but before the ready-poll loop begins. Use it for
	// one-shot fixups such as file-ownership corrections inside the
	// container. Nil means no-op.
	afterStart func() error
}

// ensureDockerContainer starts (or reuses) a single named Docker container:
// verifies Docker is installed and running, reuses the container if already
// running, restarts it if stopped cleanly, otherwise creates it fresh via
// spec.runArgs — then polls spec.ready until it succeeds or spec.timeout
// elapses (checking every spec.pollInterval). spec.notReadyErr is returned
// verbatim on timeout, since Korean grammar particles (이/가) attaching to
// each service's name can't be derived generically. Idempotent: safe to
// call repeatedly.
func ensureDockerContainer(spec containerSpec, logs *logbuf.Buf) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("Docker가 설치되어 있지 않습니다")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		return fmt.Errorf("Docker 데몬이 실행 중이지 않습니다 (Docker Desktop을 실행하세요)")
	}
	createFresh := func() error {
		logs.Append(fmt.Sprintf("[info] %s 컨테이너를 새로 생성합니다 (%s)", spec.displayName, spec.image))
		out, err := exec.Command("docker", spec.runArgs...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s 컨테이너 생성 실패: %w (%s)", spec.displayName, err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	// Already running?
	out, _ := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", spec.name).Output()
	switch {
	case strings.TrimSpace(string(out)) == "true":
		logs.Append(fmt.Sprintf("[info] 기존 %s 컨테이너 재사용: %s", spec.displayName, spec.name))
	default:
		// The container is not running, so binding spec.hostPort will be
		// attempted; fail fast with a clear message if another process (e.g. a
		// locally installed MySQL service) already holds it.
		if spec.hostPort > 0 && portListening(spec.hostPort, 500*time.Millisecond) {
			return fmt.Errorf("호스트 포트 %d를 다른 프로세스가 사용 중입니다. 로컬 %s 서비스 등을 중지한 뒤 다시 시도하세요", spec.hostPort, spec.displayName)
		}
		// Container may exist but stopped -> start/recreate it; otherwise create fresh.
		status, _ := exec.Command("docker", "inspect", "-f", "{{.State.Status}}", spec.name).Output()
		switch strings.TrimSpace(string(status)) {
		case "":
			if err := createFresh(); err != nil {
				return err
			}
		case "created":
			// "created" means the container never started (e.g. docker run's
			// port bind failed) — it holds no data, and `docker start` would
			// keep failing for the same reason, so replace it.
			logs.Append(fmt.Sprintf("[warn] 기동된 적 없는 %s 컨테이너가 남아 있습니다 — 새로 생성합니다", spec.displayName))
			exec.Command("docker", "rm", "-f", "-v", spec.name).Run()
			if err := createFresh(); err != nil {
				return err
			}
		default:
			crashed := false
			if spec.recreateOnCrash {
				exitCode, _ := exec.Command("docker", "inspect", "-f", "{{.State.ExitCode}}", spec.name).Output()
				crashed = strings.TrimSpace(string(exitCode)) != "0"
			}
			if crashed {
				logs.Append(fmt.Sprintf("[warn] %s 컨테이너가 비정상 종료 상태입니다 — 새로 생성합니다", spec.displayName))
				exec.Command("docker", "rm", "-f", "-v", spec.name).Run()
				if err := createFresh(); err != nil {
					return err
				}
			} else {
				logs.Append(fmt.Sprintf("[info] 정지된 %s 컨테이너를 재시작합니다", spec.displayName))
				if out, err := exec.Command("docker", "start", spec.name).CombinedOutput(); err != nil {
					return fmt.Errorf("%s 컨테이너 재시작 실패: %w (%s)", spec.displayName, err, strings.TrimSpace(string(out)))
				}
			}
		}
	}
	if spec.afterStart != nil {
		if err := spec.afterStart(); err != nil {
			logs.Append(fmt.Sprintf("[warn] %s afterStart: %v", spec.displayName, err))
		}
	}
	logs.Append(fmt.Sprintf("[info] %s 준비 대기 중...", spec.displayName))
	deadline := time.Now().Add(spec.timeout)
	for time.Now().Before(deadline) {
		if spec.ready() {
			logs.Append(fmt.Sprintf("[success] %s 준비 완료", spec.displayName))
			return nil
		}
		time.Sleep(spec.pollInterval)
	}
	return fmt.Errorf("%s", spec.notReadyErr)
}

// ensureMySQLContainer starts (or reuses) a single shared MySQL container
// used by all DB-dependent WAR targets, each getting its own schema inside
// it. Idempotent: safe to call repeatedly.
func ensureMySQLContainer(logs *logbuf.Buf) error {
	runArgs := []string{
		"run", "-d", "--name", launcherMySQLContainer,
		"-e", "MYSQL_ROOT_PASSWORD=" + launcherMySQLRootPass,
		"-p", fmt.Sprintf("%d:3306", launcherMySQLPort),
		// eGovFrame 앱들은 테이블명을 대문자(DDL/매퍼)와 소문자(런타임
		// 접근제어 SQL 등)로 섞어 쓴다. 개발 표준 환경(Windows MySQL,
		// lower_case_table_names=1)과 달리 Linux 컨테이너 기본값(0)은
		// 대소문자를 구분해 "Table doesn't exist"가 나므로 1로 맞춘다.
		// (MySQL 8은 데이터 초기화 시에만 설정 가능 — 컨테이너 생성 시 지정)
		"mysql:8.0",
		"--lower-case-table-names=1",
	}
	// Wait for MySQL to accept AUTHENTICATED connections. mysqladmin ping
	// answers "alive" even during the image's init phase (temporary server,
	// root password not yet applied), which let CREATE DATABASE race into
	// "Access denied" — so probe with a real query instead.
	ready := func() bool {
		pingCmd := exec.Command("docker", "exec", "-e", "MYSQL_PWD="+launcherMySQLRootPass, launcherMySQLContainer, "mysql", "-uroot", "-e", "SELECT 1")
		return pingCmd.Run() == nil
	}
	if err := ensureDockerContainer(containerSpec{
		name:         launcherMySQLContainer,
		displayName:  "MySQL",
		image:        "mysql:8.0",
		runArgs:      runArgs,
		ready:        ready,
		timeout:      120 * time.Second,
		pollInterval: 2 * time.Second,
		notReadyErr:  "MySQL이 120초 내에 준비되지 않았습니다",
		hostPort:     launcherMySQLPort,
		// recreateOnCrash left false: MySQL's imported schemas live in the
		// container and must never be wiped automatically.
	}, logs); err != nil {
		return err
	}
	// Containers created before v1.0.5 predate --lower-case-table-names=1;
	// they keep working for uppercase-only apps but break the mixed-case ones.
	// Never recreate automatically (data loss) — surface how to migrate.
	out, _ := exec.Command("docker", "exec", "-e", "MYSQL_PWD="+launcherMySQLRootPass, launcherMySQLContainer,
		"mysql", "-uroot", "-N", "-e", "SELECT @@lower_case_table_names").Output()
	if strings.TrimSpace(string(out)) == "0" {
		logs.Append("[warn] 기존 MySQL 컨테이너가 테이블명 대소문자를 구분합니다(lower_case_table_names=0) — 공통컴포넌트 등 일부 앱의 소문자 SQL이 실패할 수 있습니다. `docker rm -f " + launcherMySQLContainer + "` 후 각 타깃의 DB 설정을 다시 실행하면 새 설정으로 재생성됩니다")
	}
	return nil
}

// ensureRedisContainer starts (or reuses) a single shared Redis container
// used by MSA targets that need a token/session store (e.g. EgovLogin's
// AuthorizeTokenRedisConfig). Idempotent: safe to call repeatedly.
func ensureRedisContainer(port int, password string, logs *logbuf.Buf) error {
	runArgs := []string{
		"run", "-d", "--name", launcherRedisContainer,
		"-p", fmt.Sprintf("%d:6379", port),
		"redis:7-alpine", "redis-server",
	}
	if password != "" {
		runArgs = append(runArgs, "--requirepass", password)
	}
	pingArgs := []string{"exec", launcherRedisContainer, "redis-cli"}
	if password != "" {
		pingArgs = append(pingArgs, "-a", password, "--no-auth-warning")
	}
	pingArgs = append(pingArgs, "ping")
	ready := func() bool {
		out, err := exec.Command("docker", pingArgs...).CombinedOutput()
		return err == nil && strings.Contains(string(out), "PONG")
	}
	return ensureDockerContainer(containerSpec{
		name:            launcherRedisContainer,
		displayName:     "Redis",
		image:           "redis:7-alpine",
		runArgs:         runArgs,
		ready:           ready,
		timeout:         60 * time.Second,
		pollInterval:    1 * time.Second,
		notReadyErr:     "Redis가 60초 내에 준비되지 않았습니다",
		hostPort:        port,
		recreateOnCrash: true,
	}, logs)
}

// ensureRabbitMQContainer starts (or reuses) a single shared RabbitMQ
// container for MSA targets whose services expect localhost:5672 with the
// image's default guest/guest credentials (no env needed). Idempotent: safe
// to call repeatedly.
//
// D2: 런처 프로세스가 컨테이너를 생성하면 .erlang.cookie가 root 소유로
// 만들어져 rabbitmq 유저가 읽지 못해 기동 실패하는 환경이 있다(쉘에서
// 동일 명령을 직접 실행하면 정상). entrypoint를 래핑하여 cookie 파일을
// 임의의 값으로 선제 생성하고 소유권 및 권한을 교정한 뒤 원래 entrypoint(docker-entrypoint.sh)를 exec한다.
// 비용: 없음(entrypoint 인라인 sh 1줄). 탈출구: --entrypoint/sh/-c 제거.
func ensureRabbitMQContainer(logs *logbuf.Buf) error {
	// Wrap the entrypoint: pre-create the cookie with valid string, chown and chmod it,
	// then exec the real entrypoint. This avoids both eacces and too short cookie crash.
	runArgs := []string{
		"run", "-d", "--name", launcherRabbitContainer,
		"-p", "5672:5672",
		"--entrypoint", "sh",
		"rabbitmq:3",
		"-c", "echo \"egovframe_secret_rabbitmq_cookie_12345\" > /var/lib/rabbitmq/.erlang.cookie && chown rabbitmq:rabbitmq /var/lib/rabbitmq/.erlang.cookie && chmod 600 /var/lib/rabbitmq/.erlang.cookie && exec docker-entrypoint.sh rabbitmq-server",
	}
	ready := func() bool {
		return exec.Command("docker", "exec", launcherRabbitContainer, "rabbitmq-diagnostics", "-q", "ping").Run() == nil
	}
	return ensureDockerContainer(containerSpec{
		name:            launcherRabbitContainer,
		displayName:     "RabbitMQ",
		image:           "rabbitmq:3",
		runArgs:         runArgs,
		ready:           ready,
		timeout:         120 * time.Second,
		pollInterval:    2 * time.Second,
		notReadyErr:     "RabbitMQ가 120초 내에 준비되지 않았습니다",
		hostPort:        5672,
		recreateOnCrash: true,
	}, logs)
}



// ensureMySQLAppUser (re)creates the app-facing MySQL user reachable from
// the host, container bridge, and Docker Desktop's host gateway, then
// grants it full privileges. Idempotent: safe to call repeatedly.
func ensureMySQLAppUser(user, pass string) error {
	hosts := []string{"%", "localhost", "192.168.65.1"}
	var stmts strings.Builder
	for _, h := range hosts {
		fmt.Fprintf(&stmts, "CREATE USER IF NOT EXISTS '%s'@'%s' IDENTIFIED BY '%s'; ALTER USER '%s'@'%s' IDENTIFIED BY '%s'; ", user, h, pass, user, h, pass)
	}
	for _, h := range hosts {
		fmt.Fprintf(&stmts, "GRANT ALL PRIVILEGES ON *.* TO '%s'@'%s'; ", user, h)
	}
	stmts.WriteString("FLUSH PRIVILEGES;")
	cmd := exec.Command("docker", "exec", "-e", "MYSQL_PWD="+launcherMySQLRootPass, launcherMySQLContainer, "mysql", "-uroot", "-e", stmts.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("MySQL 사용자 생성 실패: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// dropCreateSchema drops (if present) and recreates dbName as an empty
// utf8mb4 schema in the shared MySQL container.
func dropCreateSchema(dbName string, logs *logbuf.Buf) error {
	logs.Append(fmt.Sprintf("[info] 데이터베이스 '%s' 초기화 및 깨끗하게 재생성", dbName))
	dropCmd := exec.Command("docker", "exec", "-e", "MYSQL_PWD="+launcherMySQLRootPass, launcherMySQLContainer, "mysql", "-uroot",
		"-e", fmt.Sprintf("DROP DATABASE IF EXISTS `%s`", dbName))
	_, _ = dropCmd.CombinedOutput()

	createCmd := exec.Command("docker", "exec", "-e", "MYSQL_PWD="+launcherMySQLRootPass, launcherMySQLContainer, "mysql", "-uroot",
		"-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", dbName))
	if out, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("데이터베이스 생성 실패: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// setupProjectDatabase creates dbName's schema in the shared MySQL
// container (if missing), imports every *.sql file under
// <dir>/DATABASE/mysql (DDL files before DATA files, by filename
// containing "ddl"/"data", otherwise alphabetical), and rewrites
// globals.properties to use root credentials against that schema.
func setupProjectDatabase(dir string, logs *logbuf.Buf) error {
	dbSetupMu.Lock()
	defer dbSetupMu.Unlock()

	if kind, sqlDir, _, _ := detectDBPattern(dir); kind == "script" {
		return setupScriptDatabase(dir, sqlDir, logs)
	}

	if err := ensureMySQLContainer(logs); err != nil {
		return err
	}
	globalsPath := findGlobalsProperties(dir)
	if globalsPath == "" {
		return fmt.Errorf("globals.properties를 찾지 못했습니다")
	}
	dbName, err := parseMySQLDBName(globalsPath)
	if err != nil {
		return err
	}

	if err := dropCreateSchema(dbName, logs); err != nil {
		return err
	}
	if err := ensureMySQLAppUser("com", "com01"); err != nil {
		return err
	}

	sqlDir := filepath.Join(dir, "DATABASE", "mysql")
	entries, err := os.ReadDir(sqlDir)
	if err != nil {
		return fmt.Errorf("SQL 디렉터리를 읽지 못했습니다: %w", err)
	}
	var ddlFiles, dataFiles, otherFiles []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
			continue
		}
		name := strings.ToLower(e.Name())
		switch {
		case strings.Contains(name, "ddl"):
			ddlFiles = append(ddlFiles, e.Name())
		case strings.Contains(name, "data"):
			dataFiles = append(dataFiles, e.Name())
		default:
			otherFiles = append(otherFiles, e.Name())
		}
	}
	sort.Strings(ddlFiles)
	sort.Strings(dataFiles)
	sort.Strings(otherFiles)
	ordered := append(append(ddlFiles, otherFiles...), dataFiles...)

	if err := importSQLFiles(sqlDir, dbName, ordered, logs); err != nil {
		return err
	}
	logs.Append("[success] SQL 임포트 완료")
	return updateAllGlobalsProperties(dir, logs)
}

// setupScriptDatabase provisions the shared MySQL container for the
// egovframe-common-components layout: script/ddl/mysql/*.sql then
// script/dml/mysql/*.sql, imported into the schema named by
// globals.properties (usually "com"). Two deliberate differences from the
// "war" flow:
//   - The schema is created WITHOUT dropping: "com" is shared with the MSA
//     common-components stack (different table names), and a drop would
//     destroy its data.
//   - globals.properties is left untouched: its password goes through
//     egovPasswordResolver (encrypted), so rewriting it to plaintext would
//     break the datasource. Instead the app user it decrypts to (com/com01)
//     is (re)created.
func setupScriptDatabase(dir, scriptDir string, logs *logbuf.Buf) error {
	if err := ensureMySQLContainer(logs); err != nil {
		return err
	}
	schema := defaultComposeDBName
	if globalsPath := findGlobalsProperties(dir); globalsPath != "" {
		if s, err := parseMySQLDBName(globalsPath); err == nil {
			schema = s
		}
	}
	logs.Append(fmt.Sprintf("[info] 데이터베이스 '%s' 준비 (공유 스키마 보호 — DROP 없이 생성)", schema))
	createCmd := exec.Command("docker", "exec", "-e", "MYSQL_PWD="+launcherMySQLRootPass, launcherMySQLContainer, "mysql", "-uroot",
		"-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;", schema))
	if out, err := createCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("데이터베이스 생성 실패: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if err := ensureMySQLAppUser(defaultComposeDBUser, defaultComposeDBPass); err != nil {
		return err
	}
	// ddl → dml → comment 순서: comment는 테이블 설명(ALTER TABLE ... COMMENT)
	// 스크립트라 테이블 생성 이후에 실행해야 한다.
	for _, sub := range []string{"ddl", "dml", "comment"} {
		sqlDir := filepath.Join(scriptDir, sub, "mysql")
		entries, err := os.ReadDir(sqlDir)
		if err != nil {
			continue
		}
		var files []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
				files = append(files, e.Name())
			}
		}
		sort.Strings(files)
		if err := importSQLFiles(sqlDir, schema, files, logs); err != nil {
			return err
		}
	}
	logs.Append("[success] SQL 임포트 완료 — 접속정보는 globals.properties의 기존 값(com 계정, 암호화 password)을 그대로 사용합니다")
	return nil
}

// importSQLFiles imports each named file in sqlDir into dbName, in the
// given order, via the shared MySQL container's root user. dbName=="" omits
// the default-database argument entirely (for init scripts that create/USE
// their own databases). Tolerates "already exists" errors (idempotent
// re-imports/partial-overlap schemas).
func importSQLFiles(sqlDir, dbName string, files []string, logs *logbuf.Buf) error {
	for _, fname := range files {
		fpath := filepath.Join(sqlDir, fname)
		logs.Append("[info] SQL 임포트: " + fname)
		f, err := os.Open(fpath)
		if err != nil {
			return fmt.Errorf("%s 열기 실패: %w", fname, err)
		}
		args := []string{"exec", "-e", "MYSQL_PWD=" + launcherMySQLRootPass, "-i", launcherMySQLContainer, "mysql", "-uroot", "--default-character-set=utf8mb4", "-f"}
		if dbName != "" {
			args = append(args, dbName)
		}
		cmd := exec.Command("docker", args...)
		cmd.Stdin = f
		out, err := cmd.CombinedOutput()
		f.Close()
		// Clean output message: filter out any harmless warnings or empty space
		outStr := strings.TrimSpace(string(out))
		if err != nil && !strings.Contains(outStr, "already exists") {
			return fmt.Errorf("%s 임포트 실패: %w (%s)", fname, err, outStr)
		}
	}
	return nil
}

// (including src/main/resources, target/classes, WEB-INF/classes)
// to use root credentials against the shared Docker MySQL container.
func updateAllGlobalsProperties(dir string, logs *logbuf.Buf) error {
	var count int
	reUser := regexp.MustCompile(`(?m)^(Globals\.(?:[A-Za-z0-9_]+\.)?UserName\s*=).*$`)
	rePass := regexp.MustCompile(`(?m)^(Globals\.(?:[A-Za-z0-9_]+\.)?Password\s*=).*$`)

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && info.Name() == "globals.properties" {
			data, err := os.ReadFile(path)
			if err == nil {
				content := string(data)
				content = reUser.ReplaceAllString(content, "${1} root")
				content = rePass.ReplaceAllString(content, "${1} "+launcherMySQLRootPass)
				if err := os.WriteFile(path, []byte(content), info.Mode()); err == nil {
					count++
				}
			}
		}
		return nil
	})
	if logs != nil && count > 0 {
		logs.Append(fmt.Sprintf("[success] %d개의 globals.properties에 DB 접속 정보(root)를 반영했습니다", count))
	}
	return nil
}

// createDatabaseRe matches a "CREATE DATABASE ..." statement, used to
// detect init SQL that manages its own schema(s).
var createDatabaseRe = regexp.MustCompile(`(?im)^\s*CREATE\s+DATABASE\b`)

// initSQLSelfManagesSchema reports whether any of the named init SQL files
// contains its own CREATE DATABASE statement (e.g. msa-edu's init.sql,
// which creates and USEs multiple databases itself). Forcing a single
// guessed schema onto such scripts — or dropCreateSchema-ing that guessed
// name beforehand — either imports into the wrong place or destroys an
// unrelated schema that happens to share the guessed name.
func initSQLSelfManagesSchema(initDir string, files []string) bool {
	for _, fname := range files {
		data, err := os.ReadFile(filepath.Join(initDir, fname))
		if err != nil {
			continue
		}
		if createDatabaseRe.Match(data) {
			return true
		}
	}
	return false
}

// setupComposeDatabase provisions the shared MySQL container for an
// eGovFrame MSA target: creates the schema/user described by
// ConfigServer's application-local.yml, then imports every *.sql under
// <dir>/docker-compose/mysql/init in ascending filename order. Most MSA init
// scripts contain no USE/CREATE DATABASE statements — upstream docker-compose
// runs them all against one default database — so everything lands in a
// single schema. Scripts that do self-manage their schema (see
// initSQLSelfManagesSchema) are instead run as-is under root with no forced
// default database.
func setupComposeDatabase(dir string, logs *logbuf.Buf) error {
	dbSetupMu.Lock()
	defer dbSetupMu.Unlock()

	initDir := composeInitSQLDir(dir)
	if initDir == "" {
		return fmt.Errorf("docker-compose/mysql/init을 찾지 못했습니다")
	}

	entries, err := os.ReadDir(initDir)
	if err != nil {
		return fmt.Errorf("SQL 디렉터리를 읽지 못했습니다: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	selfManaged := initSQLSelfManagesSchema(initDir, files)

	// RabbitMQ has no dependency on MySQL — start it concurrently with the
	// MySQL ensure+import below instead of waiting for that to finish first.
	// Failure here stays non-fatal (matches the original sequential check).
	var rabbitWG sync.WaitGroup
	if selfManaged && composeRabbitMQNeeded(dir) {
		rabbitWG.Add(1)
		go func() {
			defer rabbitWG.Done()
			if err := ensureRabbitMQContainer(logs); err != nil {
				logs.Append("[warn] RabbitMQ 자동 기동 실패: " + err.Error())
			}
		}()
	}
	defer rabbitWG.Wait()

	if err := ensureMySQLContainer(logs); err != nil {
		return err
	}

	if selfManaged {
		logs.Append("[info] init 스크립트가 자체적으로 데이터베이스를 생성/선택합니다 — 스키마 강제 없이 root로 실행")
		if err := importSQLFiles(initDir, "", files, logs); err != nil {
			return err
		}
		logs.Append("[success] SQL 임포트 완료")

		// Self-managing init scripts (e.g. msa-edu's init.sql) GRANT
		// privileges to a user that upstream docker-compose auto-creates via
		// MYSQL_USER/MYSQL_PASSWORD env vars on the official mysql image —
		// our shared container never saw those env vars, so that user needs
		// to be created here.
		if user, pass := parseComposeMySQLEnvUser(filepath.Dir(initDir)); user != "" {
			if err := ensureMySQLAppUser(user, pass); err != nil {
				return err
			}
			logs.Append(fmt.Sprintf("[info] MySQL 사용자 '%s' 생성/권한 부여", user))
		}

		return nil
	}

	dbName, user, pass := parseComposeDBConfig(dir)
	logs.Append(fmt.Sprintf("[info] MSA DB 설정: db=%s user=%s (ConfigServer/application-local.yml 기준)", dbName, user))

	if err := dropCreateSchema(dbName, logs); err != nil {
		return err
	}
	if err := ensureMySQLAppUser(user, pass); err != nil {
		return err
	}

	if err := importSQLFiles(initDir, dbName, files, logs); err != nil {
		return err
	}
	logs.Append("[success] SQL 임포트 완료")
	return nil
}

// bootPropsDbTypeRe/URLRe/UserRe/PassRe target the exact Globals.* lines
// application.properties ships with — line-targeted so the file's
// \u-escaped Korean comments are left untouched (never re-encode the file).
var (
	bootPropsDbTypeRe = regexp.MustCompile(`(?m)^Globals\.DbType=.*$`)
	bootPropsURLRe    = regexp.MustCompile(`(?m)^Globals\.mysql\.Url=.*$`)
	bootPropsUserRe   = regexp.MustCompile(`(?m)^Globals\.mysql\.UserName=.*$`)
	bootPropsPassRe   = regexp.MustCompile(`(?m)^Globals\.mysql\.Password=.*$`)
	bootPropsOriginRe = regexp.MustCompile(`(?m)^Globals\.Allow\.Origin\s*=.*$`)
)

// setupBootPropsDatabase provisions the shared MySQL container for the
// "boot properties" pattern (egovframe-template-simple-backend): creates
// the schema, imports all_<schema>_ddl_mysql.sql then all_<schema>_data_mysql.sql
// from sqlDir, then flips application.properties from its default
// Globals.DbType=hsql to mysql against that schema. Since `mvn
// spring-boot:run` re-reads resources on every Run, no rebuild is needed
// afterward. frontendPort (0 = unknown) is the paired frontend's catalog
// port, added to Globals.Allow.Origin's CORS allowlist alongside the
// default localhost:3000 so the browser's dev-server Origin header isn't
// rejected by Spring Security.
func setupBootPropsDatabase(sqlDir, schema, propsPath string, frontendPort int, logs *logbuf.Buf) error {
	dbSetupMu.Lock()
	defer dbSetupMu.Unlock()

	if err := ensureMySQLContainer(logs); err != nil {
		return err
	}
	logs.Append(fmt.Sprintf("[info] 부트 프로퍼티 DB 설정: db=%s (DATABASE/all_%s_*_mysql.sql 기준)", schema, schema))

	if err := dropCreateSchema(schema, logs); err != nil {
		return err
	}

	ddl := fmt.Sprintf("all_%s_ddl_mysql.sql", schema)
	data := fmt.Sprintf("all_%s_data_mysql.sql", schema)
	if err := importSQLFiles(sqlDir, schema, []string{ddl, data}, logs); err != nil {
		return err
	}
	logs.Append("[success] SQL 임포트 완료")

	content, err := os.ReadFile(propsPath)
	if err != nil {
		return fmt.Errorf("application.properties 읽기 실패: %w", err)
	}
	info, err := os.Stat(propsPath)
	if err != nil {
		return fmt.Errorf("application.properties 정보 조회 실패: %w", err)
	}
	updated := string(content)
	updated = bootPropsDbTypeRe.ReplaceAllString(updated, "Globals.DbType=mysql")
	updated = bootPropsURLRe.ReplaceAllString(updated, fmt.Sprintf("Globals.mysql.Url=jdbc:log4jdbc:mysql://127.0.0.1:3306/%s", schema))
	updated = bootPropsUserRe.ReplaceAllString(updated, "Globals.mysql.UserName=root")
	updated = bootPropsPassRe.ReplaceAllString(updated, "Globals.mysql.Password="+launcherMySQLRootPass)

	origins := "http://localhost:3000"
	if frontendPort > 0 && frontendPort != 3000 {
		origins += fmt.Sprintf(",http://localhost:%d", frontendPort)
	}
	if bootPropsOriginRe.MatchString(updated) {
		updated = bootPropsOriginRe.ReplaceAllString(updated, "Globals.Allow.Origin="+origins)
	} else {
		updated += "\nGlobals.Allow.Origin=" + origins + "\n"
	}
	logs.Append("[info] CORS 허용 Origin 반영: " + origins)

	if err := os.WriteFile(propsPath, []byte(updated), info.Mode()); err != nil {
		return fmt.Errorf("application.properties 갱신 실패: %w", err)
	}
	logs.Append("[success] application.properties에 mysql 설정을 반영했습니다 (Globals.DbType=mysql)")
	return nil
}

// SetupDB provisions (or reuses) a Docker MySQL instance for the target's
// database requirements, imports its schema/data, and rewrites its
// globals.properties — then rebuilds so the fix is baked into the WAR.
// Async; progress surfaces via the job's status/logs.
func (r *Runner) SetupDB(id string) error {
	j, ok := r.jobFor(id)
	if !ok {
		return fmt.Errorf("unknown target: %s", id)
	}
	go r.setupDB(j)
	return nil
}

func (r *Runner) setupDB(j *job) {
	dir := r.dir(j.target.ID)
	if _, err := os.Stat(dir); err != nil {
		j.set(StatusError, "먼저 Clone 하세요")
		return
	}
	kind, sqlDir, schema, propsPath := detectDBPattern(dir)
	if kind == "" {
		j.set(StatusError, "이 프로젝트는 DB 설정이 필요하지 않습니다")
		return
	}
	j.set(StatusBuilding, "DB 구성 중...")

	switch kind {
	case "compose":
		// Redis has no dependency on MySQL/RabbitMQ — start it concurrently
		// with setupComposeDatabase (which itself parallels MySQL and
		// RabbitMQ internally) instead of waiting for it to finish first.
		// Failure here stays non-fatal (matches the original sequential check).
		var redisWG sync.WaitGroup
		if redisPort, redisPass := parseRedisConfig(dir); redisPort > 0 {
			redisWG.Add(1)
			go func() {
				defer redisWG.Done()
				if err := ensureRedisContainer(redisPort, redisPass, j.logs); err != nil {
					j.logs.Append("[warn] Redis 자동 기동 실패: " + err.Error())
				}
			}()
		}

		// MSA targets read DB config from ConfigServer at runtime — no
		// globals.properties rewrite or Maven rebuild needed afterward.
		composeErr := setupComposeDatabase(dir, j.logs)
		redisWG.Wait()
		if composeErr != nil {
			j.set(StatusError, "DB 구성 실패: "+composeErr.Error())
			return
		}
		if len(j.target.Run) == 0 && j.target.DeployType == "lib" {
			j.logs.Append("[success] DB 설정 완료 — 이 템플릿은 원클릭 실행 미지원입니다. VSCode로 열어 README의 구동 절차를 따르세요")
		} else {
			j.logs.Append("[success] DB 설정 완료 — 재빌드 불필요, 바로 Run 하세요")
		}
		j.set(StatusDone, "")

	case "bootprops":
		// "Boot properties" targets (egovframe-template-simple-backend) also
		// read their DB config fresh on every `mvn spring-boot:run` — no
		// rebuild needed either.
		var frontendPort int
		for _, tg := range catalogTargets() {
			if tg.BackendID == j.target.ID {
				frontendPort = tg.Port
				break
			}
		}
		if err := setupBootPropsDatabase(sqlDir, schema, propsPath, frontendPort, j.logs); err != nil {
			j.set(StatusError, "DB 구성 실패: "+err.Error())
			return
		}
		j.logs.Append("[success] DB 설정 완료 — Run(재기동)하면 반영됩니다")
		j.set(StatusDone, "")

	case "script":
		// egovframe-common-components: globals.properties stays as-is (the
		// encrypted com-account credentials already point at 127.0.0.1:3306),
		// so no rebuild is needed after the import.
		if err := setupProjectDatabase(dir, j.logs); err != nil {
			j.set(StatusError, "DB 구성 실패: "+err.Error())
			return
		}
		j.logs.Append("[success] DB 설정 완료 — 재빌드 불필요, 바로 배포/Run 하세요")
		j.set(StatusDone, "")

	default: // "war"
		if err := setupProjectDatabase(dir, j.logs); err != nil {
			j.set(StatusError, "DB 구성 실패: "+err.Error())
			return
		}
		j.logs.Append("[info] DB 설정 반영을 위해 재빌드합니다...")
		for _, c := range j.target.Build {
			if err := r.exec(j, c); err != nil {
				j.logs.Append("[error] " + err.Error())
				j.set(StatusError, err.Error())
				return
			}
		}
		j.logs.Append("[success] DB 설정 및 재빌드 완료 — 이제 배포할 수 있습니다")
		j.set(StatusDone, "")
	}
}

func freePort(port int) {
	if port <= 0 || !portListening(port, 200*time.Millisecond) {
		return
	}
	if runtime.GOOS == "windows" {
		out, err := exec.Command("cmd", "/c", fmt.Sprintf("for /f \"tokens=5\" %%a in ('netstat -aon ^| findstr :%d') do taskkill /f /pid %%a", port)).CombinedOutput()
		_ = out
		_ = err
	} else {
		out, err := exec.Command("sh", "-c", fmt.Sprintf("lsof -t -i :%d | xargs kill -9 2>/dev/null || true", port)).CombinedOutput()
		_ = out
		_ = err
	}
}
