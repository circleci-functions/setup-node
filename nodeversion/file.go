package nodeversion

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultSpec is used when neither a version nor a version file is given and
// no default version file exists.
const DefaultSpec = "lts/*"

// defaultFiles are checked, in order, when no version is given.
var defaultFiles = []string{".nvmrc", ".node-version"}

var plainVersionRe = regexp.MustCompile(`(?m)^(?:node(?:js)?\s+)?v?(\S+)$`)

// FromFile returns the version spec recorded in the file at path. It
// understands package.json (volta.node, devEngines.runtime, engines.node, then
// volta.extends), mise.toml (tools.node), .tool-versions (a node or nodejs
// line) and files holding just a version, such as .nvmrc and .node-version.
func FromFile(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // reading the caller's chosen version file is the point
	if err != nil {
		return "", err
	}

	var manifest map[string]any
	if json.Unmarshal(data, &manifest) == nil && manifest != nil {
		return fromPackageJSON(path, manifest)
	}

	var mise struct {
		Tools map[string]any `toml:"tools"`
	}
	if _, err := toml.Decode(string(data), &mise); err == nil {
		if node, ok := mise.Tools["node"]; ok {
			return fromMiseTool(path, node)
		}
	}

	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if m := plainVersionRe.FindSubmatch(data); m != nil {
		return string(m[1]), nil
	}
	if spec := strings.TrimSpace(string(data)); spec != "" {
		return spec, nil
	}
	return "", fmt.Errorf("no Node.js version found in %s", path)
}

func fromPackageJSON(path string, manifest map[string]any) (string, error) {
	if v := str(dig(manifest, "volta", "node")); v != "" {
		return v, nil
	}
	if v := devEnginesNode(dig(manifest, "devEngines", "runtime")); v != "" {
		return v, nil
	}
	if v := str(dig(manifest, "engines", "node")); v != "" {
		return v, nil
	}
	if extends := str(dig(manifest, "volta", "extends")); extends != "" {
		return FromFile(filepath.Join(filepath.Dir(path), extends))
	}
	return "", fmt.Errorf("no Node.js version found in %s", path)
}

// devEnginesNode returns the version of the node entry in a devEngines.runtime
// value, which npm allows to be a single object or a list of them.
func devEnginesNode(runtime any) string {
	entries, ok := runtime.([]any)
	if !ok {
		entries = []any{runtime}
	}
	for _, e := range entries {
		entry, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(str(entry["name"]), "node") {
			if v := str(entry["version"]); v != "" {
				return v
			}
		}
	}
	return ""
}

func fromMiseTool(path string, node any) (string, error) {
	switch n := node.(type) {
	case string:
		return n, nil
	case map[string]any:
		if v := str(n["version"]); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("no Node.js version found in %s", path)
}

// Default returns the version spec to use when none is given, and where it
// came from: the first of .nvmrc and .node-version found in dir, else
// DefaultSpec.
func Default(dir string) (spec, source string, err error) {
	for _, name := range defaultFiles {
		path := filepath.Join(dir, name)
		spec, err := FromFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", "", err
		}
		return spec, path, nil
	}
	return DefaultSpec, "", nil
}

func dig(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		next, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = next[k]
	}
	return cur
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}
