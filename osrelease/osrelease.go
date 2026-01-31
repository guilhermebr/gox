// Package osrelease provides functions to parse /etc/os-release
// and detect Linux distribution information.
package osrelease

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Standard paths for os-release file
const (
	EtcOsRelease    = "/etc/os-release"
	UsrLibOsRelease = "/usr/lib/os-release"
)

// OSRelease contains parsed /etc/os-release data.
// See https://www.freedesktop.org/software/systemd/man/os-release.html
type OSRelease struct {
	ID            string // Lowercase identifier (e.g., "ubuntu", "debian", "fedora")
	IDLike        string // Space-separated list of related OS IDs (e.g., "debian ubuntu")
	Name          string // Human-readable OS name (e.g., "Ubuntu")
	Version       string // Version string (e.g., "22.04.3 LTS (Jammy Jellyfish)")
	VersionID     string // Version identifier (e.g., "22.04")
	PrettyName    string // Pretty name (e.g., "Ubuntu 22.04.3 LTS")
	HomeURL       string // OS home page URL
	SupportURL    string // OS support URL
	BugReportURL  string // OS bug report URL
	VersionCodename string // Version codename (e.g., "jammy")
}

// Read parses the os-release file from the standard location.
// It first tries /etc/os-release, then falls back to /usr/lib/os-release.
func Read() (*OSRelease, error) {
	// Try /etc/os-release first
	if _, err := os.Stat(EtcOsRelease); err == nil {
		return ReadFile(EtcOsRelease)
	}

	// Fall back to /usr/lib/os-release
	if _, err := os.Stat(UsrLibOsRelease); err == nil {
		return ReadFile(UsrLibOsRelease)
	}

	return nil, fmt.Errorf("os-release file not found at %s or %s", EtcOsRelease, UsrLibOsRelease)
}

// ReadFile parses an os-release file from the specified path.
func ReadFile(path string) (*OSRelease, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", path, err)
	}
	defer file.Close()

	release := &OSRelease{}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Split on first '='
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := unquote(parts[1])

		switch key {
		case "ID":
			release.ID = value
		case "ID_LIKE":
			release.IDLike = value
		case "NAME":
			release.Name = value
		case "VERSION":
			release.Version = value
		case "VERSION_ID":
			release.VersionID = value
		case "PRETTY_NAME":
			release.PrettyName = value
		case "HOME_URL":
			release.HomeURL = value
		case "SUPPORT_URL":
			release.SupportURL = value
		case "BUG_REPORT_URL":
			release.BugReportURL = value
		case "VERSION_CODENAME":
			release.VersionCodename = value
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading %s: %w", path, err)
	}

	return release, nil
}

// unquote removes surrounding quotes from a value.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
