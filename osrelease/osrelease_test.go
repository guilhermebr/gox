package osrelease

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected *OSRelease
		wantErr  bool
	}{
		{
			name: "valid ubuntu os-release",
			content: `NAME="Ubuntu"
VERSION="22.04.3 LTS (Jammy Jellyfish)"
ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="Ubuntu 22.04.3 LTS"
VERSION_ID="22.04"
HOME_URL="https://www.ubuntu.com/"
SUPPORT_URL="https://help.ubuntu.com/"
BUG_REPORT_URL="https://bugs.launchpad.net/ubuntu/"
VERSION_CODENAME=jammy`,
			expected: &OSRelease{
				ID:              "ubuntu",
				IDLike:          "debian",
				Name:            "Ubuntu",
				Version:         "22.04.3 LTS (Jammy Jellyfish)",
				VersionID:       "22.04",
				PrettyName:      "Ubuntu 22.04.3 LTS",
				HomeURL:         "https://www.ubuntu.com/",
				SupportURL:      "https://help.ubuntu.com/",
				BugReportURL:    "https://bugs.launchpad.net/ubuntu/",
				VersionCodename: "jammy",
			},
		},
		{
			name: "valid fedora os-release",
			content: `NAME="Fedora Linux"
VERSION="39 (Workstation Edition)"
ID=fedora
VERSION_ID=39
PRETTY_NAME="Fedora Linux 39 (Workstation Edition)"
HOME_URL="https://fedoraproject.org/"`,
			expected: &OSRelease{
				ID:         "fedora",
				Name:       "Fedora Linux",
				Version:    "39 (Workstation Edition)",
				VersionID:  "39",
				PrettyName: "Fedora Linux 39 (Workstation Edition)",
				HomeURL:    "https://fedoraproject.org/",
			},
		},
		{
			name: "values with single quotes",
			content: `NAME='Arch Linux'
ID='arch'
PRETTY_NAME='Arch Linux'`,
			expected: &OSRelease{
				ID:         "arch",
				Name:       "Arch Linux",
				PrettyName: "Arch Linux",
			},
		},
		{
			name: "values without quotes",
			content: `ID=debian
VERSION_ID=12`,
			expected: &OSRelease{
				ID:        "debian",
				VersionID: "12",
			},
		},
		{
			name:     "empty file",
			content:  "",
			expected: &OSRelease{},
		},
		{
			name: "file with only comments",
			content: `# This is a comment
# Another comment`,
			expected: &OSRelease{},
		},
		{
			name: "file with empty lines and comments mixed",
			content: `# Comment at top

ID=test

# Comment in middle
NAME="Test OS"

`,
			expected: &OSRelease{
				ID:   "test",
				Name: "Test OS",
			},
		},
		{
			name: "malformed lines are skipped",
			content: `ID=valid
MALFORMED_NO_EQUALS
NAME="Test"
ALSO_MALFORMED
VERSION_ID=1.0`,
			expected: &OSRelease{
				ID:        "valid",
				Name:      "Test",
				VersionID: "1.0",
			},
		},
		{
			name: "values with equals sign in value",
			content: `ID=test
NAME="Test=OS"`,
			expected: &OSRelease{
				ID:   "test",
				Name: "Test=OS",
			},
		},
		{
			name: "whitespace handling",
			content: `  ID=test
  NAME="Test OS"
   VERSION_ID=1.0   `,
			expected: &OSRelease{
				ID:        "test",
				Name:      "Test OS",
				VersionID: "1.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "os-release")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to create temp file: %v", err)
			}

			got, err := ReadFile(tmpFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if got.ID != tt.expected.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.expected.ID)
			}
			if got.IDLike != tt.expected.IDLike {
				t.Errorf("IDLike = %q, want %q", got.IDLike, tt.expected.IDLike)
			}
			if got.Name != tt.expected.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.expected.Name)
			}
			if got.Version != tt.expected.Version {
				t.Errorf("Version = %q, want %q", got.Version, tt.expected.Version)
			}
			if got.VersionID != tt.expected.VersionID {
				t.Errorf("VersionID = %q, want %q", got.VersionID, tt.expected.VersionID)
			}
			if got.PrettyName != tt.expected.PrettyName {
				t.Errorf("PrettyName = %q, want %q", got.PrettyName, tt.expected.PrettyName)
			}
			if got.HomeURL != tt.expected.HomeURL {
				t.Errorf("HomeURL = %q, want %q", got.HomeURL, tt.expected.HomeURL)
			}
			if got.SupportURL != tt.expected.SupportURL {
				t.Errorf("SupportURL = %q, want %q", got.SupportURL, tt.expected.SupportURL)
			}
			if got.BugReportURL != tt.expected.BugReportURL {
				t.Errorf("BugReportURL = %q, want %q", got.BugReportURL, tt.expected.BugReportURL)
			}
			if got.VersionCodename != tt.expected.VersionCodename {
				t.Errorf("VersionCodename = %q, want %q", got.VersionCodename, tt.expected.VersionCodename)
			}
		})
	}
}

func TestReadFileNotFound(t *testing.T) {
	_, err := ReadFile("/nonexistent/path/os-release")
	if err == nil {
		t.Error("ReadFile() expected error for non-existent file, got nil")
	}
}

func TestUnquote(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "double quoted string",
			input:    `"Ubuntu"`,
			expected: "Ubuntu",
		},
		{
			name:     "single quoted string",
			input:    `'Arch Linux'`,
			expected: "Arch Linux",
		},
		{
			name:     "unquoted string",
			input:    "debian",
			expected: "debian",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single character",
			input:    "a",
			expected: "a",
		},
		{
			name:     "only quotes",
			input:    `""`,
			expected: "",
		},
		{
			name:     "mismatched quotes - double then single",
			input:    `"Ubuntu'`,
			expected: `"Ubuntu'`,
		},
		{
			name:     "mismatched quotes - single then double",
			input:    `'Ubuntu"`,
			expected: `'Ubuntu"`,
		},
		{
			name:     "quotes inside string",
			input:    `"Ubuntu "LTS""`,
			expected: `Ubuntu "LTS"`,
		},
		{
			name:     "whitespace around value",
			input:    "  ubuntu  ",
			expected: "ubuntu",
		},
		{
			name:     "whitespace around quoted value",
			input:    `  "Ubuntu"  `,
			expected: "Ubuntu",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unquote(tt.input)
			if got != tt.expected {
				t.Errorf("unquote(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRead(t *testing.T) {
	// This test depends on the system having an os-release file
	// Skip if running in a container or environment without it
	if _, err := os.Stat(EtcOsRelease); os.IsNotExist(err) {
		if _, err := os.Stat(UsrLibOsRelease); os.IsNotExist(err) {
			t.Skip("Skipping TestRead: no os-release file found on system")
		}
	}

	release, err := Read()
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	// Basic sanity checks - ID should be non-empty on most systems
	if release.ID == "" {
		t.Error("Read() returned empty ID, expected non-empty")
	}
}

func TestReadNoOsReleaseFile(t *testing.T) {
	// Save original constants (we can't modify them, so we test via ReadFile)
	// This tests the error path when the file doesn't exist
	_, err := ReadFile("/nonexistent/os-release")
	if err == nil {
		t.Error("ReadFile() with non-existent file should return error")
	}
}
