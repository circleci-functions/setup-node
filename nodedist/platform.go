// Package nodedist downloads, verifies and unpacks official Node.js builds
// from a server laid out like https://nodejs.org/dist.
package nodedist

import (
	"fmt"
	"runtime"
	"strings"
)

// Platform is a Node.js build target.
type Platform struct {
	// OS is Node's name for the operating system: linux, darwin or win.
	OS string
	// Arch is Node's name for the architecture, e.g. x64 or arm64.
	Arch string
}

var nodeOS = map[string]string{
	"linux":   "linux",
	"darwin":  "darwin",
	"windows": "win",
}

var nodeArch = map[string]string{
	"amd64":   "x64",
	"arm64":   "arm64",
	"386":     "x86",
	"arm":     "armv7l",
	"ppc64le": "ppc64le",
	"s390x":   "s390x",
}

// Host returns the platform setup-node is running on.
func Host() (Platform, error) {
	return platformFor(runtime.GOOS, runtime.GOARCH)
}

func platformFor(goos, goarch string) (Platform, error) {
	os, ok := nodeOS[goos]
	if !ok {
		return Platform{}, fmt.Errorf("unsupported operating system %s", goos)
	}
	arch, ok := nodeArch[goarch]
	if !ok {
		return Platform{}, fmt.Errorf("unsupported architecture %s", goarch)
	}
	return Platform{OS: os, Arch: arch}, nil
}

// FileID returns the platform's name in index.json's files lists.
func (p Platform) FileID() string {
	switch p.OS {
	case "darwin":
		return "osx-" + p.Arch + "-tar"
	case "win":
		return "win-" + p.Arch + "-zip"
	}
	return p.name()
}

// ArchiveName returns the file name of the build of version for p, e.g.
// node-v22.11.0-linux-x64.tar.gz.
func (p Platform) ArchiveName(version string) string {
	ext := ".tar.gz"
	if p.OS == "win" {
		ext = ".zip"
	}
	return p.DirName(version) + ext
}

// DirName returns the single top-level directory inside the build's archive.
func (p Platform) DirName(version string) string {
	return "node-v" + strings.TrimPrefix(version, "v") + "-" + p.name()
}

func (p Platform) String() string {
	return p.name()
}

func (p Platform) name() string {
	return p.OS + "-" + p.Arch
}
