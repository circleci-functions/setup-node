package nodedist

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	hc "github.com/circleci-functions/toolkit/httpcl"

	"github.com/circleci-functions/setup-node/nodeversion"
)

// DefaultURL is the official Node.js distribution server.
const DefaultURL = "https://nodejs.org/dist"

// Dist is a Node.js distribution server: the official one or a mirror of it.
type Dist struct {
	url    string
	client *hc.Client
}

// New returns a Dist for the server at url. token, when set, is sent verbatim
// as the Authorization header on every request.
func New(ctx context.Context, url, token string) *Dist {
	url = strings.TrimSuffix(url, "/")
	cfg := hc.Config{
		Name:    "nodejs-dist",
		BaseURL: url,
		Timeout: 15 * time.Minute,
	}
	if token != "" {
		cfg.AuthHeader = "Authorization"
		cfg.AuthToken = token
	}
	return &Dist{url: url, client: hc.New(ctx, cfg)}
}

// Index returns every release the server lists.
func (d *Dist) Index(ctx context.Context) ([]nodeversion.Release, error) {
	var releases []nodeversion.Release
	err := d.client.Call(ctx, hc.NewRequest(http.MethodGet, "/index.json",
		hc.JSONDecoder(&releases),
		hc.Timeout(time.Minute),
	))
	if err != nil {
		return nil, fmt.Errorf("fetch %s/index.json: %w", d.url, err)
	}
	return releases, nil
}

// Install downloads the build of version for p, checks it against the
// release's SHASUMS256.txt, and unpacks it so that dest is the root of the
// Node.js install (holding bin/node, or node.exe on Windows). Anything already
// at dest, such as an incomplete install, is replaced.
func (d *Dist) Install(ctx context.Context, version string, p Platform, dest string) error {
	version = "v" + strings.TrimPrefix(version, "v")
	name := p.ArchiveName(version)

	want, err := d.checksum(ctx, version, name)
	if err != nil {
		return err
	}

	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0750); err != nil {
		return err
	}
	archive, err := os.CreateTemp(parent, name+".*.download")
	if err != nil {
		return err
	}
	defer func() {
		_ = archive.Close()
		_ = os.Remove(archive.Name())
	}()

	if err := d.download(ctx, version, name, archive); err != nil {
		return err
	}

	if err := verify(archive, want); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	staging, err := os.MkdirTemp(parent, filepath.Base(dest)+".*.extract")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	if p.OS == "win" {
		err = extractZip(archive.Name(), staging)
	} else {
		err = extractTarGz(archive.Name(), staging)
	}
	if err != nil {
		return fmt.Errorf("extract %s: %w", name, err)
	}

	root := filepath.Join(staging, p.DirName(version))
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("%s has no %s directory", name, p.DirName(version))
	}
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return os.Rename(root, dest)
}

// download writes the archive name from version's directory to f, starting
// over on each retry.
func (d *Dist) download(ctx context.Context, version, name string, f *os.File) error {
	err := d.client.Call(ctx, hc.NewRequest(http.MethodGet, "/%s/%s",
		hc.RouteParams(version, name),
		hc.SuccessDecoder(func(r io.Reader) error {
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return err
			}
			if err := f.Truncate(0); err != nil {
				return err
			}
			_, err := io.Copy(f, r)
			return err
		}),
		hc.Timeout(10*time.Minute),
	))
	if err != nil {
		return fmt.Errorf("download %s/%s/%s: %w", d.url, version, name, err)
	}
	return nil
}

func (d *Dist) checksum(ctx context.Context, version, name string) (string, error) {
	var sums []byte
	err := d.client.Call(ctx, hc.NewRequest(http.MethodGet, "/%s/SHASUMS256.txt",
		hc.RouteParams(version),
		hc.BytesDecoder(&sums),
		hc.Timeout(time.Minute),
	))
	if err != nil {
		return "", fmt.Errorf("fetch %s/%s/SHASUMS256.txt: %w", d.url, version, err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("%s is not listed in %s/%s/SHASUMS256.txt", name, d.url, version)
}

func verify(f *os.File, want string) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch: got sha256 %s, want %s", got, want)
	}
	return nil
}
