//go:build darwin

package runner

import (
	"os"
	"os/exec"
	"path/filepath"
)

// OpenBrowser opens url in the default browser on macOS (supports arm64 and amd64 architectures).
func OpenBrowser(url string) error {
	return exec.Command("open", url).Start()
}

// launchVSCode opens dir in VS Code, injecting the selected javaHome environment variables
// and separating the process group so the editor window persists independently.
func launchVSCode(codeBin, dir, javaHome string) error {
	cmd := exec.Command(codeBin, dir)
	setProcAttr(cmd)

	env := os.Environ()
	if javaHome != "" {
		env = append(
			env,
			"JAVA_HOME="+javaHome,
			"PATH="+filepath.Join(javaHome, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
		)
	}
	cmd.Env = env
	return cmd.Start()
}
