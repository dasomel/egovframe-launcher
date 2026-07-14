//go:build darwin

package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// JDKInfo holds metadata about a detected JDK installation.
type JDKInfo struct {
	Home    string `json:"home"`
	Version int    `json:"version"` // major version, e.g. 17
	Label   string `json:"label"`   // display string, e.g. "Temurin 17.0.19 (arm64)"
}

// DetectJDKs returns all JDKs found on the system, sorted by version then full version.
func DetectJDKs() []JDKInfo {
	seen := map[string]bool{} // key = resolved real path
	var results []JDKInfo

	// 1. /usr/libexec/java_home -V is the most reliable source on macOS.
	//    Each line (except the last) looks like:
	//      "    26.0.1 (arm64) "Eclipse Adoptium" - "OpenJDK 26.0.1" /path"
	if out, err := exec.Command("/usr/libexec/java_home", "-V").CombinedOutput(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// The last line is the selected JDK path only — skip if it starts with "/"
			// but has no spaces after it (that's the default JDK echo).
			// We detect real entries by the presence of " (arm64) " or " (x86_64) ".
			jdk, ok := parseJavaHomeLine(line)
			if !ok {
				continue
			}
			real := realPath(jdk.Home)
			if seen[real] {
				continue
			}
			if _, err := os.Stat(filepath.Join(jdk.Home, "bin", "javac")); err != nil {
				continue
			}
			seen[real] = true
			results = append(results, jdk)
		}
	}

	// 2. Fallback: scan common directories for any JDKs not caught above.
	fallbackDirs := []string{
		"/Library/Java/JavaVirtualMachines",
		filepath.Join(os.Getenv("HOME"), "Library", "Java", "JavaVirtualMachines"),
		"/opt/homebrew/opt",
		"/usr/local/opt",
	}
	for _, dir := range fallbackDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			var home string
			// Homebrew formula layout: /opt/homebrew/opt/openjdk@17/
			if strings.HasPrefix(e.Name(), "openjdk") || strings.HasPrefix(e.Name(), "temurin") {
				home = filepath.Join(dir, e.Name())
			} else {
				// Standard JVM bundle: .jdk/Contents/Home
				home = filepath.Join(dir, e.Name(), "Contents", "Home")
			}
			real := realPath(home)
			if seen[real] {
				continue
			}
			if _, err := os.Stat(filepath.Join(home, "bin", "javac")); err != nil {
				continue
			}
			ver, fullVer := jdkVersionFromBinary(home)
			if ver == 0 {
				continue
			}
			arch := runtime.GOARCH
			if arch == "amd64" {
				arch = "x86_64"
			}
			seen[real] = true
			results = append(results, JDKInfo{
				Home:    home,
				Version: ver,
				Label:   fmt.Sprintf("OpenJDK %s (%s)", fullVer, arch),
			})
		}
	}

	// 3. SDKMAN
	if sdkman := filepath.Join(os.Getenv("HOME"), ".sdkman", "candidates", "java"); sdkman != "" {
		if entries, err := os.ReadDir(sdkman); err == nil {
			for _, e := range entries {
				if e.Name() == "current" {
					continue
				}
				home := filepath.Join(sdkman, e.Name())
				real := realPath(home)
				if seen[real] {
					continue
				}
				if _, err := os.Stat(filepath.Join(home, "bin", "javac")); err != nil {
					continue
				}
				ver, fullVer := jdkVersionFromBinary(home)
				if ver == 0 {
					continue
				}
				seen[real] = true
				results = append(results, JDKInfo{
					Home:    home,
					Version: ver,
					Label:   fmt.Sprintf("SDKMAN %s", fullVer),
				})
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Version != results[j].Version {
			return results[i].Version < results[j].Version
		}
		return results[i].Label < results[j].Label
	})
	return results
}

// parseJavaHomeLine parses a line from `/usr/libexec/java_home -V`.
// Format: `26.0.1 (arm64) "Eclipse Adoptium" - "OpenJDK 26.0.1" /path/to/home`
func parseJavaHomeLine(line string) (JDKInfo, bool) {
	// Must contain architecture marker
	archIdx := strings.Index(line, "(arm64)")
	if archIdx == -1 {
		archIdx = strings.Index(line, "(x86_64)")
	}
	if archIdx == -1 {
		return JDKInfo{}, false
	}

	// Full version is the first token
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return JDKInfo{}, false
	}
	fullVer := parts[0]
	arch := strings.Trim(parts[1], "()")

	major := parseMajorVersion(fullVer)
	if major == 0 {
		return JDKInfo{}, false
	}

	// Home path is the last token that starts with "/"
	home := ""
	for i := len(parts) - 1; i >= 0; i-- {
		if strings.HasPrefix(parts[i], "/") {
			home = parts[i]
			break
		}
	}
	if home == "" {
		return JDKInfo{}, false
	}

	// Vendor name: text between first pair of double-quotes
	vendor := extractQuoted(line, 0)

	var label string
	if vendor != "" {
		// Map verbose vendor names to short names
		vendor = shortVendor(vendor)
		label = fmt.Sprintf("%s %s (%s)", vendor, fullVer, arch)
	} else {
		label = fmt.Sprintf("OpenJDK %s (%s)", fullVer, arch)
	}

	return JDKInfo{Home: home, Version: major, Label: label}, true
}

// shortVendor maps verbose vendor strings to concise display names.
func shortVendor(v string) string {
	v = strings.ToLower(v)
	switch {
	case strings.Contains(v, "adoptium") || strings.Contains(v, "temurin"):
		return "Temurin"
	case strings.Contains(v, "amazon") || strings.Contains(v, "corretto"):
		return "Corretto"
	case strings.Contains(v, "graalvm"):
		return "GraalVM"
	case strings.Contains(v, "zulu"):
		return "Zulu"
	case strings.Contains(v, "bellsoft") || strings.Contains(v, "liberica"):
		return "Liberica"
	case strings.Contains(v, "microsoft"):
		return "Microsoft"
	case strings.Contains(v, "oracle"):
		return "Oracle"
	case strings.Contains(v, "azul"):
		return "Azul"
	default:
		return "OpenJDK"
	}
}

// extractQuoted returns the content of the n-th quoted string in s (0-indexed).
func extractQuoted(s string, n int) string {
	count := 0
	for {
		start := strings.Index(s, "\"")
		if start == -1 {
			return ""
		}
		s = s[start+1:]
		end := strings.Index(s, "\"")
		if end == -1 {
			return ""
		}
		val := s[:end]
		s = s[end+1:]
		if count == n {
			return val
		}
		count++
	}
}

// parseMajorVersion extracts the major version number from a version string like "17.0.10" or "1.8.0_472".
func parseMajorVersion(ver string) int {
	parts := strings.FieldsFunc(ver, func(r rune) bool { return r == '.' || r == '_' })
	if len(parts) == 0 {
		return 0
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	// Old-style "1.8" → 8
	if major == 1 && len(parts) > 1 {
		minor, err := strconv.Atoi(parts[1])
		if err == nil {
			return minor
		}
	}
	return major
}

// jdkVersionFromBinary runs "java -version" for the given JDK home and returns (major, fullVersion).
func jdkVersionFromBinary(home string) (int, string) {
	javaBin := filepath.Join(home, "bin", "java")
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

// realPath resolves symlinks to deduplicate JDK entries.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
