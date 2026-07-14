//go:build windows

package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// JDKInfo holds metadata about a detected JDK installation.
type JDKInfo struct {
	Home    string `json:"home"`
	Version int    `json:"version"`
	Label   string `json:"label"`
}

// DetectJDKs returns all JDKs found on the system (Windows), sorted by version.
func DetectJDKs() []JDKInfo {
	seen := map[string]bool{}
	var results []JDKInfo

	add := func(home string) {
		home = filepath.Clean(home)
		real := realPath(home)
		if seen[real] {
			return
		}
		if _, err := os.Stat(filepath.Join(home, "bin", "javac.exe")); err != nil {
			return
		}
		major, fullVer := jdkVersionFromBinary(home)
		if major == 0 {
			return
		}
		vendor := vendorFromPath(home)
		seen[real] = true
		results = append(results, JDKInfo{
			Home:    home,
			Version: major,
			Label:   fmt.Sprintf("%s %s (x86_64)", vendor, fullVer),
		})
	}

	// Common Windows JDK install roots
	roots := []string{
		`C:\Program Files\Java`,
		`C:\Program Files\Eclipse Adoptium`,
		`C:\Program Files\Microsoft`,
		`C:\Program Files\Amazon Corretto`,
		`C:\Program Files\Zulu`,
		`C:\Program Files\BellSoft`,
		`C:\Program Files\OpenLogic`,
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				add(filepath.Join(root, e.Name()))
			}
		}
	}

	// Scoop
	if home := os.Getenv("USERPROFILE"); home != "" {
		scoopApps := filepath.Join(home, "scoop", "apps")
		for _, pattern := range []string{"temurin*", "openjdk*", "corretto*"} {
			matches, _ := filepath.Glob(filepath.Join(scoopApps, pattern, "current"))
			for _, m := range matches {
				add(m)
			}
		}
	}

	// JAVA_HOME env
	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		add(jh)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Version != results[j].Version {
			return results[i].Version < results[j].Version
		}
		return results[i].Label < results[j].Label
	})
	return results
}

// vendorFromPath guesses a short vendor name from the install directory name.
func vendorFromPath(home string) string {
	lower := strings.ToLower(home)
	switch {
	case strings.Contains(lower, "adoptium") || strings.Contains(lower, "temurin"):
		return "Temurin"
	case strings.Contains(lower, "corretto"):
		return "Corretto"
	case strings.Contains(lower, "microsoft"):
		return "Microsoft"
	case strings.Contains(lower, "zulu"):
		return "Zulu"
	case strings.Contains(lower, "bellsoft") || strings.Contains(lower, "liberica"):
		return "Liberica"
	case strings.Contains(lower, "graalvm"):
		return "GraalVM"
	default:
		return "OpenJDK"
	}
}

func jdkVersionFromBinary(home string) (int, string) {
	javaBin := filepath.Join(home, "bin", "java.exe")
	if _, err := os.Stat(javaBin); err != nil {
		javaBin = filepath.Join(home, "bin", "java")
	}
	out, err := exec.Command(javaBin, "-version").CombinedOutput()
	if err != nil {
		return 0, ""
	}
	line := string(out)
	idx := strings.Index(line, "version \"")
	if idx == -1 {
		return 0, ""
	}
	vStr := line[idx+len("version \""):]
	vStr = strings.SplitN(vStr, "\"", 2)[0]
	major := parseMajorVersion(vStr)
	return major, vStr
}

func parseMajorVersion(ver string) int {
	parts := strings.FieldsFunc(ver, func(r rune) bool { return r == '.' || r == '_' })
	if len(parts) == 0 {
		return 0
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	if major == 1 && len(parts) > 1 {
		minor, err := strconv.Atoi(parts[1])
		if err == nil {
			return minor
		}
	}
	return major
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
