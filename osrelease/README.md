# osrelease

A Go package to parse `/etc/os-release` and detect Linux distribution information.

## Installation

```bash
go get github.com/guilhermebr/gox/osrelease
```

## Usage

### Parse os-release

```go
package main

import (
    "fmt"
    "github.com/guilhermebr/gox/osrelease"
)

func main() {
    // Read and parse os-release
    release, err := osrelease.Read()
    if err != nil {
        panic(err)
    }

    fmt.Printf("ID: %s\n", release.ID)
    fmt.Printf("Name: %s\n", release.Name)
    fmt.Printf("Version: %s\n", release.VersionID)
}
```

### Detect Distribution with Package Manager

```go
package main

import (
    "fmt"
    "github.com/guilhermebr/gox/osrelease"
)

func main() {
    // Detect distribution info including package manager
    distro, err := osrelease.Detect()
    if err != nil {
        panic(err)
    }

    fmt.Printf("OS: %s\n", distro.PrettyName)
    fmt.Printf("Family: %s\n", distro.Family)
    fmt.Printf("Package Manager: %s\n", distro.PkgManager)

    if distro.IsDebian() {
        fmt.Println("This is a Debian-based system")
    }
}
```

## API

### Types

```go
// OSRelease contains parsed /etc/os-release data
type OSRelease struct {
    ID              string // e.g., "ubuntu", "fedora", "arch"
    IDLike          string // e.g., "debian ubuntu"
    Name            string // e.g., "Ubuntu"
    Version         string // e.g., "22.04.3 LTS"
    VersionID       string // e.g., "22.04"
    PrettyName      string // e.g., "Ubuntu 22.04.3 LTS"
    VersionCodename string // e.g., "jammy"
}

// Distro extends OSRelease with family and package manager info
type Distro struct {
    *OSRelease
    Family     string // debian, rhel, arch, unknown
    PkgManager string // apt, dnf, yum, pacman
}
```

### Functions

- `Read() (*OSRelease, error)` - Parse os-release from standard location
- `ReadFile(path string) (*OSRelease, error)` - Parse os-release from specific path
- `Detect() (*Distro, error)` - Detect full distribution info

### Distro Methods

- `IsDebian() bool` - Returns true for Debian-based distros
- `IsRHEL() bool` - Returns true for RHEL-based distros
- `IsArch() bool` - Returns true for Arch-based distros

## Supported Distributions

### Debian Family (apt)
Ubuntu, Debian, Linux Mint, Pop!_OS, elementary OS, Zorin, Kali, Raspbian, KDE Neon, Deepin

### RHEL Family (dnf/yum)
Fedora, RHEL, CentOS, Rocky Linux, AlmaLinux, Oracle Linux, Amazon Linux

### Arch Family (pacman)
Arch Linux, Manjaro, EndeavourOS, Garuda, Artix, ArcoLinux, BlackArch
