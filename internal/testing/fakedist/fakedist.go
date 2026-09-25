// Package fakedist serves a stand-in for https://nodejs.org/dist, so tests can
// resolve, download and unpack Node.js builds without the network.
package fakedist

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Release is one Node.js release the server lists, with a build for every
// platform in Files.
type Release struct {
	Version string
	LTS     string
	Files   []string
}

// DefaultFiles are the builds a Release ships when it sets no Files.
var DefaultFiles = []string{
	"linux-x64", "linux-arm64", "linux-armv7l", "linux-ppc64le", "linux-s390x",
	"osx-x64-tar", "osx-arm64-tar",
	"win-x64-zip", "win-arm64-zip", "win-x86-zip",
}

// Server is a running fake distribution server.
type Server struct {
	URL string

	releases []Release
	mu       sync.Mutex
	requests []*http.Request
	corrupt  bool
}

// New starts a server listing releases, and stops it when t ends.
func New(t *testing.T, releases ...Release) *Server {
	t.Helper()
	s := &Server{releases: releases}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	s.URL = srv.URL
	return s
}

// Corrupt makes the server send archives that do not match their checksums.
func (s *Server) Corrupt() {
	s.corrupt = true
}

// Requests returns the requests served so far.
func (s *Server) Requests() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.requests...)
}

// Downloads returns how many archives have been served.
func (s *Server) Downloads() int {
	n := 0
	for _, r := range s.Requests() {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") || strings.HasSuffix(r.URL.Path, ".zip") {
			n++
		}
	}
	return n
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Clone(r.Context()))
	s.mu.Unlock()

	if r.URL.Path == "/index.json" {
		s.serveIndex(w)
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	rel, ok := s.release(parts[0])
	if !ok {
		http.NotFound(w, r)
		return
	}
	if parts[1] == "SHASUMS256.txt" {
		s.serveSums(w, rel)
		return
	}
	data, ok := buildArchive(rel, parts[1])
	if !ok {
		http.NotFound(w, r)
		return
	}
	if s.corrupt {
		data = append(data, 0)
	}
	_, _ = w.Write(data)
}

func (s *Server) serveIndex(w http.ResponseWriter) {
	type entry struct {
		Version string   `json:"version"`
		Files   []string `json:"files"`
		LTS     any      `json:"lts"`
	}
	entries := make([]entry, 0, len(s.releases))
	for _, rel := range s.releases {
		var lts any = false
		if rel.LTS != "" {
			lts = rel.LTS
		}
		entries = append(entries, entry{Version: "v" + rel.Version, Files: files(rel), LTS: lts})
	}
	_ = json.NewEncoder(w).Encode(entries)
}

func (s *Server) serveSums(w http.ResponseWriter, rel Release) {
	for _, id := range files(rel) {
		name := archiveName(rel.Version, id)
		data, _ := buildArchive(rel, name)
		_, _ = fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(data), name)
	}
}

func (s *Server) release(dir string) (Release, bool) {
	for _, rel := range s.releases {
		if "v"+rel.Version == dir {
			return rel, true
		}
	}
	return Release{}, false
}

func files(rel Release) []string {
	if rel.Files != nil {
		return rel.Files
	}
	return DefaultFiles
}

// archiveName maps an index.json files entry to the archive it names.
func archiveName(version, fileID string) string {
	id := strings.TrimSuffix(strings.TrimSuffix(fileID, "-tar"), "-zip")
	id = strings.Replace(id, "osx-", "darwin-", 1)
	ext := ".tar.gz"
	if strings.HasPrefix(id, "win-") {
		ext = ".zip"
	}
	return "node-v" + version + "-" + id + ext
}

func buildArchive(rel Release, name string) ([]byte, bool) {
	for _, id := range files(rel) {
		if archiveName(rel.Version, id) != name {
			continue
		}
		root := strings.TrimSuffix(strings.TrimSuffix(name, ".tar.gz"), ".zip")
		if strings.HasSuffix(name, ".zip") {
			return windowsZip(root, rel.Version), true
		}
		return unixTarGz(root, rel.Version), true
	}
	return nil, false
}

// unixTarGz mirrors the layout of a real Unix build: bin/node plus npm, npx and
// corepack as relative symlinks into lib/node_modules. bin/node is a shell
// script that answers --version, so a test can run it.
func unixTarGz(root, version string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	dir := func(name string) {
		_ = tw.WriteHeader(&tar.Header{Name: root + "/" + name + "/", Typeflag: tar.TypeDir, Mode: 0755})
	}
	file := func(name, content string, mode int64) {
		_ = tw.WriteHeader(&tar.Header{Name: root + "/" + name, Typeflag: tar.TypeReg, Mode: mode, Size: int64(len(content))})
		_, _ = tw.Write([]byte(content))
	}
	link := func(name, target string) {
		_ = tw.WriteHeader(&tar.Header{Name: root + "/" + name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0777})
	}

	dir("")
	dir("bin")
	file("bin/node", "#!/bin/sh\necho v"+version+"\n", 0755)
	for _, tool := range []string{"npm", "npx", "corepack"} {
		pkg := tool
		if tool == "npx" {
			pkg = "npm"
		}
		file("lib/node_modules/"+pkg+"/bin/"+tool+"-cli.js", "#!/bin/sh\necho "+tool+"\n", 0755)
		link("bin/"+tool, "../lib/node_modules/"+pkg+"/bin/"+tool+"-cli.js")
	}

	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// windowsZip mirrors the layout of a real Windows build, where node.exe and the
// npm, npx and corepack .cmd shims sit at the top level. Entries are written in
// a fixed order so the archive, and so its checksum, is the same every time.
func windowsZip(root, version string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct{ name, content string }{
		{"node.exe", "v" + version},
		{"npm.cmd", "@echo npm"},
		{"npx.cmd", "@echo npx"},
		{"corepack.cmd", "@echo corepack"},
		{"node_modules/npm/bin/npm-cli.js", ""},
		{"node_modules/corepack/package.json", "{}"},
	} {
		w, _ := zw.Create(root + "/" + f.name)
		_, _ = w.Write([]byte(f.content))
	}
	_ = zw.Close()
	return buf.Bytes()
}
