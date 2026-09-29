package nodedist

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/skip"

	"github.com/circleci-functions/setup-node/internal/testing/fakedist"
)

func TestPlatform(t *testing.T) {
	tests := []struct {
		goos, goarch string
		wantFileID   string
		wantArchive  string
	}{
		{goos: "linux", goarch: "amd64", wantFileID: "linux-x64", wantArchive: "node-v22.1.0-linux-x64.tar.gz"},
		{goos: "linux", goarch: "arm64", wantFileID: "linux-arm64", wantArchive: "node-v22.1.0-linux-arm64.tar.gz"},
		{goos: "linux", goarch: "arm", wantFileID: "linux-armv7l", wantArchive: "node-v22.1.0-linux-armv7l.tar.gz"},
		{goos: "linux", goarch: "ppc64le", wantFileID: "linux-ppc64le", wantArchive: "node-v22.1.0-linux-ppc64le.tar.gz"},
		{goos: "linux", goarch: "s390x", wantFileID: "linux-s390x", wantArchive: "node-v22.1.0-linux-s390x.tar.gz"},
		{goos: "darwin", goarch: "arm64", wantFileID: "osx-arm64-tar", wantArchive: "node-v22.1.0-darwin-arm64.tar.gz"},
		{goos: "darwin", goarch: "amd64", wantFileID: "osx-x64-tar", wantArchive: "node-v22.1.0-darwin-x64.tar.gz"},
		{goos: "windows", goarch: "amd64", wantFileID: "win-x64-zip", wantArchive: "node-v22.1.0-win-x64.zip"},
		{goos: "windows", goarch: "arm64", wantFileID: "win-arm64-zip", wantArchive: "node-v22.1.0-win-arm64.zip"},
		{goos: "windows", goarch: "386", wantFileID: "win-x86-zip", wantArchive: "node-v22.1.0-win-x86.zip"},
	}
	for _, tc := range tests {
		t.Run(tc.wantFileID, func(t *testing.T) {
			p, err := platformFor(tc.goos, tc.goarch)
			assert.NilError(t, err)
			assert.Equal(t, p.FileID(), tc.wantFileID)
			assert.Equal(t, p.ArchiveName("v22.1.0"), tc.wantArchive)
		})
	}
}

func TestPlatform_Unsupported(t *testing.T) {
	_, err := platformFor("freebsd", "amd64")
	assert.ErrorContains(t, err, "unsupported operating system freebsd")
	_, err = platformFor("linux", "mips")
	assert.ErrorContains(t, err, "unsupported architecture mips")
}

func TestIndex(t *testing.T) {
	srv := fakedist.New(t,
		fakedist.Release{Version: "24.1.0", LTS: "Krypton"},
		fakedist.Release{Version: "25.0.0"},
	)
	releases, err := New(context.Background(), srv.URL, "").Index(context.Background())
	assert.NilError(t, err)
	assert.Equal(t, len(releases), 2)
	assert.Equal(t, releases[0].Version, "v24.1.0")
	assert.Equal(t, string(releases[0].LTS), "Krypton")
	assert.Equal(t, string(releases[1].LTS), "")
}

func TestInstall(t *testing.T) {
	tests := []struct {
		name     string
		platform Platform
		want     []string
	}{
		{name: "tar.gz", platform: Platform{OS: "linux", Arch: "x64"}, want: []string{"bin/node", "bin/npm", "bin/npx", "bin/corepack"}},
		{name: "darwin", platform: Platform{OS: "darwin", Arch: "arm64"}, want: []string{"bin/node", "bin/npm"}},
		{name: "zip", platform: Platform{OS: "win", Arch: "x64"}, want: []string{"node.exe", "npm.cmd", "npx.cmd", "corepack.cmd"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakedist.New(t, fakedist.Release{Version: "22.1.0"})
			dest := filepath.Join(t.TempDir(), "node")

			err := New(context.Background(), srv.URL+"/", "").Install(context.Background(), "22.1.0", tc.platform, dest)
			assert.NilError(t, err)
			for _, f := range tc.want {
				_, err := os.Stat(filepath.Join(dest, f))
				assert.NilError(t, err, f)
			}
			entries, err := os.ReadDir(filepath.Dir(dest))
			assert.NilError(t, err)
			assert.Equal(t, len(entries), 1, "download and staging leftovers: %v", entries)
		})
	}
}

func TestInstall_ReplacesIncompleteInstall(t *testing.T) {
	srv := fakedist.New(t, fakedist.Release{Version: "22.1.0"})
	dest := filepath.Join(t.TempDir(), "node")
	assert.NilError(t, os.MkdirAll(filepath.Join(dest, "lib"), 0750))
	assert.NilError(t, os.WriteFile(filepath.Join(dest, "lib", "leftover"), []byte("x"), 0600))

	assert.NilError(t, New(context.Background(), srv.URL, "").Install(context.Background(), "22.1.0", Platform{OS: "linux", Arch: "x64"}, dest))

	_, err := os.Stat(filepath.Join(dest, "bin", "node"))
	assert.NilError(t, err)
	_, err = os.Stat(filepath.Join(dest, "lib", "leftover"))
	assert.Assert(t, os.IsNotExist(err), "the incomplete install was not replaced")
}

func TestInstall_UnixLayoutRuns(t *testing.T) {
	skip.If(t, runtime.GOOS == "windows", "the fake build's node is a shell script")
	srv := fakedist.New(t, fakedist.Release{Version: "22.1.0"})
	dest := filepath.Join(t.TempDir(), "node")

	assert.NilError(t, New(context.Background(), srv.URL, "").Install(context.Background(), "v22.1.0", Platform{OS: "linux", Arch: "x64"}, dest))

	info, err := os.Stat(filepath.Join(dest, "bin", "node"))
	assert.NilError(t, err)
	assert.Assert(t, info.Mode().Perm()&0100 != 0, "bin/node is not executable: %v", info.Mode())
	target, err := os.Readlink(filepath.Join(dest, "bin", "npm"))
	assert.NilError(t, err)
	assert.Equal(t, target, "../lib/node_modules/npm/bin/npm-cli.js")
}

func TestInstall_Errors(t *testing.T) {
	tests := []struct {
		name    string
		release fakedist.Release
		version string
		corrupt bool
		wantErr string
	}{
		{name: "checksum mismatch", release: fakedist.Release{Version: "22.1.0"}, version: "22.1.0", corrupt: true, wantErr: "checksum mismatch"},
		{name: "no build for platform", release: fakedist.Release{Version: "22.1.0", Files: []string{"win-x64-zip"}}, version: "22.1.0", wantErr: "node-v22.1.0-linux-x64.tar.gz is not listed"},
		{name: "unknown version", release: fakedist.Release{Version: "22.1.0"}, version: "21.0.0", wantErr: "SHASUMS256.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakedist.New(t, tc.release)
			if tc.corrupt {
				srv.Corrupt()
			}
			dest := filepath.Join(t.TempDir(), "node")

			err := New(context.Background(), srv.URL, "").Install(context.Background(), tc.version, Platform{OS: "linux", Arch: "x64"}, dest)
			assert.ErrorContains(t, err, tc.wantErr)
			_, statErr := os.Stat(dest)
			assert.Assert(t, os.IsNotExist(statErr), "a failed install left %s behind", dest)
		})
	}
}

func TestMirrorToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{name: "sent verbatim", token: "Bearer s3cret", want: "Bearer s3cret"},
		{name: "omitted when empty", token: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakedist.New(t, fakedist.Release{Version: "22.1.0"})
			dest := filepath.Join(t.TempDir(), "node")
			d := New(context.Background(), srv.URL, tc.token)
			_, err := d.Index(context.Background())
			assert.NilError(t, err)
			assert.NilError(t, d.Install(context.Background(), "22.1.0", Platform{OS: "linux", Arch: "x64"}, dest))

			reqs := srv.Requests()
			assert.Equal(t, len(reqs), 3)
			for _, r := range reqs {
				assert.Equal(t, r.Header.Get("Authorization"), tc.want, r.URL.Path)
			}
		})
	}
}

func TestWithin(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "x")
	tests := []struct {
		name string
		ok   bool
	}{
		{name: "node-v1/bin/node", ok: true},
		{name: "node-v1/", ok: true},
		{name: "../escape", ok: false},
		{name: "node-v1/../../escape", ok: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := within(dest, tc.name)
			assert.Equal(t, err == nil, tc.ok, "%v", err)
		})
	}
}
