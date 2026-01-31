package osrelease

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFamilyAndPkgMgr(t *testing.T) {
	tests := []struct {
		name           string
		release        *OSRelease
		expectedFamily string
		expectedPkgMgr string
	}{
		// Debian-based distributions
		{
			name:           "debian by ID",
			release:        &OSRelease{ID: "debian"},
			expectedFamily: FamilyDebian,
			expectedPkgMgr: PkgMgrApt,
		},
		{
			name:           "ubuntu by ID",
			release:        &OSRelease{ID: "ubuntu"},
			expectedFamily: FamilyDebian,
			expectedPkgMgr: PkgMgrApt,
		},
		{
			name:           "linuxmint by ID",
			release:        &OSRelease{ID: "linuxmint"},
			expectedFamily: FamilyDebian,
			expectedPkgMgr: PkgMgrApt,
		},
		{
			name:           "pop by ID",
			release:        &OSRelease{ID: "pop"},
			expectedFamily: FamilyDebian,
			expectedPkgMgr: PkgMgrApt,
		},
		{
			name:           "kali by ID",
			release:        &OSRelease{ID: "kali"},
			expectedFamily: FamilyDebian,
			expectedPkgMgr: PkgMgrApt,
		},
		{
			name:           "debian by ID_LIKE",
			release:        &OSRelease{ID: "custom", IDLike: "debian"},
			expectedFamily: FamilyDebian,
			expectedPkgMgr: PkgMgrApt,
		},
		{
			name:           "ubuntu derivative by ID_LIKE",
			release:        &OSRelease{ID: "custom", IDLike: "ubuntu debian"},
			expectedFamily: FamilyDebian,
			expectedPkgMgr: PkgMgrApt,
		},

		// Arch-based distributions
		{
			name:           "arch by ID",
			release:        &OSRelease{ID: "arch"},
			expectedFamily: FamilyArch,
			expectedPkgMgr: PkgMgrPacman,
		},
		{
			name:           "manjaro by ID",
			release:        &OSRelease{ID: "manjaro"},
			expectedFamily: FamilyArch,
			expectedPkgMgr: PkgMgrPacman,
		},
		{
			name:           "endeavouros by ID",
			release:        &OSRelease{ID: "endeavouros"},
			expectedFamily: FamilyArch,
			expectedPkgMgr: PkgMgrPacman,
		},
		{
			name:           "arch by ID_LIKE",
			release:        &OSRelease{ID: "custom", IDLike: "arch"},
			expectedFamily: FamilyArch,
			expectedPkgMgr: PkgMgrPacman,
		},

		// Unknown distributions
		{
			name:           "unknown distribution",
			release:        &OSRelease{ID: "unknown"},
			expectedFamily: FamilyUnknown,
			expectedPkgMgr: "",
		},
		{
			name:           "empty release",
			release:        &OSRelease{},
			expectedFamily: FamilyUnknown,
			expectedPkgMgr: "",
		},

		// Case insensitivity
		{
			name:           "case insensitive ID",
			release:        &OSRelease{ID: "Ubuntu"},
			expectedFamily: FamilyDebian,
			expectedPkgMgr: PkgMgrApt,
		},
		{
			name:           "case insensitive ID_LIKE",
			release:        &OSRelease{ID: "custom", IDLike: "DEBIAN"},
			expectedFamily: FamilyDebian,
			expectedPkgMgr: PkgMgrApt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			family, pkgMgr := detectFamilyAndPkgMgr(tt.release)
			if family != tt.expectedFamily {
				t.Errorf("detectFamilyAndPkgMgr() family = %q, want %q", family, tt.expectedFamily)
			}
			// Skip package manager check for RHEL since it depends on system state (dnf vs yum)
			if tt.expectedFamily != FamilyRHEL && pkgMgr != tt.expectedPkgMgr {
				t.Errorf("detectFamilyAndPkgMgr() pkgMgr = %q, want %q", pkgMgr, tt.expectedPkgMgr)
			}
		})
	}
}

func TestDetectRHELFamily(t *testing.T) {
	// Test RHEL family detection separately since package manager depends on system state
	tests := []struct {
		name    string
		release *OSRelease
	}{
		{
			name:    "fedora by ID",
			release: &OSRelease{ID: "fedora"},
		},
		{
			name:    "rhel by ID",
			release: &OSRelease{ID: "rhel"},
		},
		{
			name:    "centos by ID",
			release: &OSRelease{ID: "centos"},
		},
		{
			name:    "rocky by ID",
			release: &OSRelease{ID: "rocky"},
		},
		{
			name:    "almalinux by ID",
			release: &OSRelease{ID: "almalinux"},
		},
		{
			name:    "amazon by ID",
			release: &OSRelease{ID: "amazon"},
		},
		{
			name:    "rhel by ID_LIKE",
			release: &OSRelease{ID: "custom", IDLike: "rhel fedora"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			family, pkgMgr := detectFamilyAndPkgMgr(tt.release)
			if family != FamilyRHEL {
				t.Errorf("detectFamilyAndPkgMgr() family = %q, want %q", family, FamilyRHEL)
			}
			// Package manager should be either dnf or yum
			if pkgMgr != PkgMgrDnf && pkgMgr != PkgMgrYum {
				t.Errorf("detectFamilyAndPkgMgr() pkgMgr = %q, want %q or %q", pkgMgr, PkgMgrDnf, PkgMgrYum)
			}
		})
	}
}

func TestIsDebian(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		idLike   string
		expected bool
	}{
		{"debian id", "debian", "", true},
		{"ubuntu id", "ubuntu", "", true},
		{"linuxmint id", "linuxmint", "", true},
		{"pop id", "pop", "", true},
		{"elementary id", "elementary", "", true},
		{"zorin id", "zorin", "", true},
		{"kali id", "kali", "", true},
		{"raspbian id", "raspbian", "", true},
		{"neon id", "neon", "", true},
		{"deepin id", "deepin", "", true},
		{"debian in id_like", "custom", "debian", true},
		{"ubuntu in id_like", "custom", "ubuntu", true},
		{"debian and ubuntu in id_like", "custom", "debian ubuntu", true},
		{"unknown id", "unknown", "", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDebian(tt.id, tt.idLike)
			if got != tt.expected {
				t.Errorf("isDebian(%q, %q) = %v, want %v", tt.id, tt.idLike, got, tt.expected)
			}
		})
	}
}

func TestIsRHEL(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		idLike   string
		expected bool
	}{
		{"fedora id", "fedora", "", true},
		{"rhel id", "rhel", "", true},
		{"centos id", "centos", "", true},
		{"rocky id", "rocky", "", true},
		{"almalinux id", "almalinux", "", true},
		{"ol id", "ol", "", true},
		{"amazon id", "amazon", "", true},
		{"scientific id", "scientific", "", true},
		{"eurolinux id", "eurolinux", "", true},
		{"rhel in id_like", "custom", "rhel", true},
		{"fedora in id_like", "custom", "fedora", true},
		{"centos in id_like", "custom", "centos", true},
		{"unknown id", "unknown", "", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRHEL(tt.id, tt.idLike)
			if got != tt.expected {
				t.Errorf("isRHEL(%q, %q) = %v, want %v", tt.id, tt.idLike, got, tt.expected)
			}
		})
	}
}

func TestIsArch(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		idLike   string
		expected bool
	}{
		{"arch id", "arch", "", true},
		{"manjaro id", "manjaro", "", true},
		{"endeavouros id", "endeavouros", "", true},
		{"garuda id", "garuda", "", true},
		{"artix id", "artix", "", true},
		{"arcolinux id", "arcolinux", "", true},
		{"blackarch id", "blackarch", "", true},
		{"arch in id_like", "custom", "arch", true},
		{"unknown id", "unknown", "", false},
		{"empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isArch(tt.id, tt.idLike)
			if got != tt.expected {
				t.Errorf("isArch(%q, %q) = %v, want %v", tt.id, tt.idLike, got, tt.expected)
			}
		})
	}
}

func TestDistroMethods(t *testing.T) {
	tests := []struct {
		name     string
		distro   *Distro
		isDebian bool
		isRHEL   bool
		isArch   bool
	}{
		{
			name:     "debian family",
			distro:   &Distro{Family: FamilyDebian},
			isDebian: true,
			isRHEL:   false,
			isArch:   false,
		},
		{
			name:     "rhel family",
			distro:   &Distro{Family: FamilyRHEL},
			isDebian: false,
			isRHEL:   true,
			isArch:   false,
		},
		{
			name:     "arch family",
			distro:   &Distro{Family: FamilyArch},
			isDebian: false,
			isRHEL:   false,
			isArch:   true,
		},
		{
			name:     "unknown family",
			distro:   &Distro{Family: FamilyUnknown},
			isDebian: false,
			isRHEL:   false,
			isArch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.distro.IsDebian(); got != tt.isDebian {
				t.Errorf("Distro.IsDebian() = %v, want %v", got, tt.isDebian)
			}
			if got := tt.distro.IsRHEL(); got != tt.isRHEL {
				t.Errorf("Distro.IsRHEL() = %v, want %v", got, tt.isRHEL)
			}
			if got := tt.distro.IsArch(); got != tt.isArch {
				t.Errorf("Distro.IsArch() = %v, want %v", got, tt.isArch)
			}
		})
	}
}

func TestDetectWithTempFile(t *testing.T) {
	// Create a temporary os-release file and test Detect via ReadFile
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "os-release")

	content := `NAME="Ubuntu"
VERSION="22.04.3 LTS"
ID=ubuntu
ID_LIKE=debian
PRETTY_NAME="Ubuntu 22.04.3 LTS"
VERSION_ID="22.04"`

	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	release, err := ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	// Create distro manually since Detect uses system paths
	distro := &Distro{
		OSRelease: release,
	}
	distro.Family, distro.PkgManager = detectFamilyAndPkgMgr(release)

	if distro.ID != "ubuntu" {
		t.Errorf("distro.ID = %q, want %q", distro.ID, "ubuntu")
	}
	if distro.Family != FamilyDebian {
		t.Errorf("distro.Family = %q, want %q", distro.Family, FamilyDebian)
	}
	if distro.PkgManager != PkgMgrApt {
		t.Errorf("distro.PkgManager = %q, want %q", distro.PkgManager, PkgMgrApt)
	}
	if !distro.IsDebian() {
		t.Error("distro.IsDebian() = false, want true")
	}
}

func TestDistroEmbeddedOSRelease(t *testing.T) {
	// Test that embedded OSRelease fields are accessible
	release := &OSRelease{
		ID:         "ubuntu",
		Name:       "Ubuntu",
		PrettyName: "Ubuntu 22.04 LTS",
	}

	distro := &Distro{
		OSRelease:  release,
		Family:     FamilyDebian,
		PkgManager: PkgMgrApt,
	}

	// Access embedded fields
	if distro.ID != "ubuntu" {
		t.Errorf("distro.ID = %q, want %q", distro.ID, "ubuntu")
	}
	if distro.Name != "Ubuntu" {
		t.Errorf("distro.Name = %q, want %q", distro.Name, "Ubuntu")
	}
	if distro.PrettyName != "Ubuntu 22.04 LTS" {
		t.Errorf("distro.PrettyName = %q, want %q", distro.PrettyName, "Ubuntu 22.04 LTS")
	}
}

func TestConstants(t *testing.T) {
	// Verify constants are defined correctly
	if FamilyDebian != "debian" {
		t.Errorf("FamilyDebian = %q, want %q", FamilyDebian, "debian")
	}
	if FamilyRHEL != "rhel" {
		t.Errorf("FamilyRHEL = %q, want %q", FamilyRHEL, "rhel")
	}
	if FamilyArch != "arch" {
		t.Errorf("FamilyArch = %q, want %q", FamilyArch, "arch")
	}
	if FamilyUnknown != "unknown" {
		t.Errorf("FamilyUnknown = %q, want %q", FamilyUnknown, "unknown")
	}

	if PkgMgrApt != "apt" {
		t.Errorf("PkgMgrApt = %q, want %q", PkgMgrApt, "apt")
	}
	if PkgMgrDnf != "dnf" {
		t.Errorf("PkgMgrDnf = %q, want %q", PkgMgrDnf, "dnf")
	}
	if PkgMgrYum != "yum" {
		t.Errorf("PkgMgrYum = %q, want %q", PkgMgrYum, "yum")
	}
	if PkgMgrPacman != "pacman" {
		t.Errorf("PkgMgrPacman = %q, want %q", PkgMgrPacman, "pacman")
	}
}
