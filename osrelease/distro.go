package osrelease

import (
	"os/exec"
	"strings"
)

// OS family constants
const (
	FamilyDebian  = "debian"
	FamilyRHEL    = "rhel"
	FamilyArch    = "arch"
	FamilyUnknown = "unknown"
)

// Package manager constants
const (
	PkgMgrApt    = "apt"
	PkgMgrDnf    = "dnf"
	PkgMgrYum    = "yum"
	PkgMgrPacman = "pacman"
)

// Distro contains detected distribution information.
type Distro struct {
	*OSRelease
	Family     string // debian, rhel, arch, unknown
	PkgManager string // apt, dnf, yum, pacman
}

// Detect reads the os-release file and determines the OS family
// and package manager.
func Detect() (*Distro, error) {
	release, err := Read()
	if err != nil {
		return nil, err
	}

	distro := &Distro{
		OSRelease: release,
	}

	distro.Family, distro.PkgManager = detectFamilyAndPkgMgr(release)

	return distro, nil
}

// detectFamilyAndPkgMgr determines the OS family and package manager
// based on the os-release information.
func detectFamilyAndPkgMgr(release *OSRelease) (family, pkgMgr string) {
	id := strings.ToLower(release.ID)
	idLike := strings.ToLower(release.IDLike)

	// Check for Debian-based distributions
	if isDebian(id, idLike) {
		return FamilyDebian, PkgMgrApt
	}

	// Check for RHEL-based distributions
	if isRHEL(id, idLike) {
		// Determine if dnf or yum should be used
		if hasDnf() {
			return FamilyRHEL, PkgMgrDnf
		}
		return FamilyRHEL, PkgMgrYum
	}

	// Check for Arch-based distributions
	if isArch(id, idLike) {
		return FamilyArch, PkgMgrPacman
	}

	return FamilyUnknown, ""
}

// isDebian checks if the distribution is Debian-based.
func isDebian(id, idLike string) bool {
	debianIDs := []string{
		"debian", "ubuntu", "linuxmint", "pop", "elementary",
		"zorin", "kali", "raspbian", "neon", "deepin",
	}

	for _, did := range debianIDs {
		if id == did {
			return true
		}
	}

	return strings.Contains(idLike, "debian") || strings.Contains(idLike, "ubuntu")
}

// isRHEL checks if the distribution is RHEL-based.
func isRHEL(id, idLike string) bool {
	rhelIDs := []string{
		"fedora", "rhel", "centos", "rocky", "almalinux",
		"ol", "amazon", "scientific", "eurolinux",
	}

	for _, rid := range rhelIDs {
		if id == rid {
			return true
		}
	}

	return strings.Contains(idLike, "rhel") || strings.Contains(idLike, "fedora") || strings.Contains(idLike, "centos")
}

// isArch checks if the distribution is Arch-based.
func isArch(id, idLike string) bool {
	archIDs := []string{
		"arch", "manjaro", "endeavouros", "garuda", "artix",
		"arcolinux", "blackarch",
	}

	for _, aid := range archIDs {
		if id == aid {
			return true
		}
	}

	return strings.Contains(idLike, "arch")
}

// hasDnf checks if dnf is available on the system.
func hasDnf() bool {
	_, err := exec.LookPath("dnf")
	return err == nil
}

// IsDebian returns true if the distribution is Debian-based.
func (d *Distro) IsDebian() bool {
	return d.Family == FamilyDebian
}

// IsRHEL returns true if the distribution is RHEL-based.
func (d *Distro) IsRHEL() bool {
	return d.Family == FamilyRHEL
}

// IsArch returns true if the distribution is Arch-based.
func (d *Distro) IsArch() bool {
	return d.Family == FamilyArch
}
