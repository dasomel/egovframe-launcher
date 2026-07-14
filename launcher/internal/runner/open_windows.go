//go:build windows

package runner

import (
	"os"
	"os/exec"
	"path/filepath"
)

// OpenBrowser opens url in the default browser (Windows).
func OpenBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// launchVSCode opens dir in VS Code on Windows, injecting the selected javaHome environment variables.
func launchVSCode(codeBin, dir, javaHome string) error {
	if codeBin == "" {
		codeBin = "code"
	}
	cmd := exec.Command(codeBin, dir)

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
