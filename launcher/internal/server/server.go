// Package server exposes the launcher HTTP API and serves the embedded UI.
package server

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"egovframe-launcher/internal/catalog"
	"egovframe-launcher/internal/persist"
	"egovframe-launcher/internal/runner"
)

type TargetView struct {
	catalog.Target
	State   runner.State    `json:"state"`
	Prereqs map[string]bool `json:"prereqs"`
	Cloned  bool            `json:"cloned"`
	NeedsDB bool            `json:"needsDB"`
}

type server struct {
	r       *runner.Runner
	assets  fs.FS
	version string
}

func New(r *runner.Runner, assets fs.FS, version string) http.Handler {
	s := &server{r: r, assets: assets, version: version}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/targets", s.handleTargets)
	mux.HandleFunc("POST /api/targets/{id}/{action}", s.handleAction)
	mux.HandleFunc("GET /api/events/{id}", s.handleEvents)
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("POST /api/config", s.handleSaveConfig)
	mux.HandleFunc("GET /api/jdks", s.handleJDKs)
	mux.HandleFunc("POST /api/install-extension", s.handleInstallExtension)
	mux.HandleFunc("GET /api/extension-status", s.handleExtensionStatus)
	mux.HandleFunc("GET /api/version", s.handleVersion)
	mux.Handle("GET /", noCache(http.FileServer(http.FS(assets))))
	return mux
}

// startedAt is set at package init, ≈ process start time. Surfaced in the UI so
// the user can confirm a restart actually took effect (the recurring "stale
// launcher" trap — code changes only apply after rebuild+restart).
var startedAt = time.Now()

func (s *server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"version":   s.version,
		"startedAt": startedAt.Format("2006-01-02 15:04:05"),
		"pid":       os.Getpid(),
	})
}

// noCache prevents browsers from serving stale embedded UI assets after a
// launcher rebuild, so app.js/style.css changes always take effect on reload.
func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		h.ServeHTTP(w, r)
	})
}

func (s *server) handleTargets(w http.ResponseWriter, _ *http.Request) {
	views := make([]TargetView, 0)
	for _, t := range catalog.Targets() {
		pre := map[string]bool{}
		for _, tool := range t.Prereqs {
			pre[tool] = catalog.Available(tool)
		}
		cloned := false
		if fi, err := os.Stat(filepath.Join(s.r.Workspace(), t.ID)); err == nil && fi.IsDir() {
			cloned = true
		}
		needsDB := false
		if cloned {
			needsDB = runner.NeedsDatabase(filepath.Join(s.r.Workspace(), t.ID))
		}
		st := s.r.State(t.ID)
		// Source already on disk but no job ran this session: show "cloned"
		// instead of "idle" so status doesn't contradict the cloned state.
		if cloned && st.Status == runner.StatusIdle {
			st.Status = "cloned"
		}
		views = append(views, TargetView{
			Target:  t,
			State:   st,
			Prereqs: pre,
			Cloned:  cloned,
			NeedsDB: needsDB,
		})
	}
	writeJSON(w, views)
}

func (s *server) handleAction(w http.ResponseWriter, req *http.Request) {
	id := req.PathValue("id")
	action := req.PathValue("action")
	var err error
	if action == "stop" {
		err = s.r.Stop(id)
	} else if action == "vscode" {
		cfg := persist.Load()
		err = s.r.OpenVSCode(id, cfg.VSCodePath)
	} else if action == "rsp-setup" {
		err = s.r.SetupRSP(id)
	} else if action == "db-setup" {
		err = s.r.SetupDB(id)
	} else if action == "clone" {
		clean := req.URL.Query().Get("clean") == "true"
		err = s.r.DoWithOptions(id, action, clean)
	} else if action == "tomcat" {
		cfg := persist.Load()
		customPort := 0
		if pStr := req.URL.Query().Get("port"); pStr != "" {
			customPort, _ = strconv.Atoi(pStr)
		}
		err = s.r.RunWAR(id, cfg.TomcatPath, customPort)
	} else if action == "run" {
		customPort := 0
		if pStr := req.URL.Query().Get("port"); pStr != "" {
			customPort, _ = strconv.Atoi(pStr)
		}
		err = s.r.DoWithPort(id, action, customPort)
	} else {
		err = s.r.Do(id, action)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *server) handleEvents(w http.ResponseWriter, req *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	logs := s.r.Logs(req.PathValue("id"))
	ch, cancel := logs.Subscribe()
	defer cancel()

	sendSSELine := func(line string) {
		sublines := strings.Split(strings.ReplaceAll(line, "\r\n", "\n"), "\n")
		for _, sl := range sublines {
			fmt.Fprintf(w, "data: %s\n", sl)
		}
		fmt.Fprintf(w, "\n")
	}

	for _, line := range logs.Snapshot() {
		sendSSELine(line)
	}
	flusher.Flush()

	for {
		select {
		case <-req.Context().Done():
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			sendSSELine(line)
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func detectVSCode() string {
	if path, err := exec.LookPath("code"); err == nil {
		return path
	}
	macPath := "/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code"
	if _, err := os.Stat(macPath); err == nil {
		return macPath
	}
	home, _ := os.UserHomeDir()
	winPath := filepath.Join(home, "AppData", "Local", "Programs", "Microsoft VS Code", "bin", "code.cmd")
	if _, err := os.Stat(winPath); err == nil {
		return winPath
	}
	return "Not Found (Add to PATH or specify)"
}

type ConfigResponse struct {
	VSCodePath    string `json:"vscodePath"`
	WorkspacePath string `json:"workspacePath"`
	TomcatPath    string `json:"tomcatPath"`
	SkipTests     bool   `json:"skipTests"`
	JavaHome      string `json:"javaHome"`
	RSPTomcatPort int    `json:"rspTomcatPort"`
}

func (s *server) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	cfg := persist.Load()
	tomcat := cfg.TomcatPath
	vscode := cfg.VSCodePath

	if vscode == "" {
		vscode = detectVSCode()
	}

	rspPort := cfg.RSPTomcatPort
	if rspPort == 0 {
		rspPort = 8080
	}

	writeJSON(w, ConfigResponse{
		VSCodePath:    vscode,
		WorkspacePath: s.r.Workspace(),
		TomcatPath:    tomcat,
		SkipTests:     s.r.SkipTests(),
		JavaHome:      s.r.JavaHome(),
		RSPTomcatPort: rspPort,
	})
}

type SaveConfigRequest struct {
	VSCodePath    string `json:"vscodePath"`
	WorkspacePath string `json:"workspacePath"`
	TomcatPath    string `json:"tomcatPath"`
	SkipTests     bool   `json:"skipTests"`
	JavaHome      string `json:"javaHome"`
	RSPTomcatPort int    `json:"rspTomcatPort"`
}

func (s *server) handleSaveConfig(w http.ResponseWriter, req *http.Request) {
	var body SaveConfigRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if body.WorkspacePath == "" {
		http.Error(w, "workspace path cannot be empty", http.StatusBadRequest)
		return
	}

	absPath, err := filepath.Abs(body.WorkspacePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid workspace path: %v", err), http.StatusBadRequest)
		return
	}

	s.r.SetWorkspace(absPath)
	s.r.SetSkipTests(body.SkipTests)
	if body.JavaHome != "" {
		s.r.SetJavaHome(body.JavaHome)
	}

	_ = persist.Update(func(cfg *persist.Config) {
		cfg.WorkspacePath = absPath
		cfg.SkipTests = body.SkipTests
		cfg.VSCodePath = body.VSCodePath
		cfg.TomcatPath = body.TomcatPath
		cfg.RSPTomcatPort = body.RSPTomcatPort
		if body.JavaHome != "" {
			cfg.JavaHome = body.JavaHome
		}
	})

	w.WriteHeader(http.StatusOK)
}

func (s *server) handleJDKs(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, runner.DetectJDKs())
}

func (s *server) handleExtensionStatus(w http.ResponseWriter, _ *http.Request) {
	codeBin := runner.VSCodeCLI(persist.Load().VSCodePath)

	out, err := exec.Command(codeBin, "--list-extensions").CombinedOutput()
	installed := false
	if err == nil {
		installed = strings.Contains(strings.ToLower(string(out)), "redhat.vscode-community-server-connector")
	}
	writeJSON(w, map[string]bool{"installed": installed})
}

func (s *server) handleInstallExtension(w http.ResponseWriter, _ *http.Request) {
	codeBin := runner.VSCodeCLI(persist.Load().VSCodePath)

	out, err := exec.Command(codeBin, "--install-extension", "redhat.vscode-community-server-connector").CombinedOutput()
	if err != nil {
		http.Error(w, fmt.Sprintf("%v\n%s", err, out), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(out) //nolint:errcheck
}
