// Package nodeversion resolves a Node.js version spec, such as "22", "lts/*"
// or ">=20 <23", to a concrete release from a Node.js dist index, and reads
// such specs from the version files projects commonly keep them in.
package nodeversion

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Release is one entry in a Node.js dist index.json.
type Release struct {
	Version string   `json:"version"`
	Files   []string `json:"files"`
	LTS     LTS      `json:"lts"`
}

// LTS is a release's long-term-support codename, empty for a release that is
// not LTS. index.json encodes that case as false rather than a string.
type LTS string

func (l *LTS) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*l = LTS(s)
	}
	return nil
}

// Number returns the version without its leading "v", e.g. "22.11.0".
func (r Release) Number() string {
	return strings.TrimPrefix(r.Version, "v")
}

// HasFile reports whether the release ships the build named by an index.json
// files entry, such as "linux-x64" or "win-arm64-zip".
func (r Release) HasFile(fileID string) bool {
	return slices.Contains(r.Files, fileID)
}

type parsed struct {
	Release
	v *semver.Version
}

// Resolve returns the newest release matching spec that ships fileID. An empty
// fileID matches any release.
//
// spec is an exact or partial version ("22.11.0", "22", "22.x"), an npm-style
// range (">=20 <23", "^20", "20 || 22"), "latest", "current" or "node" for the
// newest release, "lts/*" for the newest LTS, "lts/<codename>", or "lts/-n"
// for the LTS line n before the newest.
func Resolve(spec string, releases []Release, fileID string) (Release, error) {
	sorted := sortNewestFirst(releases)
	s := strings.ToLower(strings.TrimSpace(spec))
	if s == "" {
		return Release{}, fmt.Errorf("empty version spec")
	}

	switch {
	case s == "latest" || s == "current" || s == "node":
		return pick(sorted, spec, fileID, func(parsed) bool { return true })
	case strings.HasPrefix(s, "lts/"):
		codename, err := ltsCodename(strings.TrimPrefix(s, "lts/"), sorted)
		if err != nil {
			return Release{}, fmt.Errorf("resolve %q: %w", spec, err)
		}
		return pick(sorted, spec, fileID, func(r parsed) bool { return strings.EqualFold(string(r.LTS), codename) })
	}

	c, err := semver.NewConstraint(s)
	if err != nil {
		return Release{}, fmt.Errorf("invalid version spec %q: %w", spec, err)
	}
	return pick(sorted, spec, fileID, func(r parsed) bool { return c.Check(r.v) })
}

// Exact returns the release version, such as "v22.11.0", that spec names
// outright, and whether it names one. Such a spec resolves to the same release
// whatever the index lists, so it can be looked for locally without the index.
func Exact(spec string) (string, bool) {
	s := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(spec)), "v")
	v, err := semver.StrictNewVersion(s)
	if err != nil || v.Prerelease() != "" || v.Metadata() != "" {
		return "", false
	}
	return "v" + v.String(), true
}

// pick returns the first of sorted that match accepts and that ships fileID,
// distinguishing "nothing matches" from "matches, but not for this platform".
func pick(sorted []parsed, spec, fileID string, match func(parsed) bool) (Release, error) {
	var matched *parsed
	for i, r := range sorted {
		if !match(r) {
			continue
		}
		if fileID == "" || r.HasFile(fileID) {
			return r.Release, nil
		}
		if matched == nil {
			matched = &sorted[i]
		}
	}
	if matched != nil {
		return Release{}, fmt.Errorf("no release matching %q has a %s build (newest match: %s)", spec, fileID, matched.Version)
	}
	return Release{}, fmt.Errorf("no release matches %q", spec)
}

// ltsCodename resolves the part of an lts/ alias after the slash to a codename.
// LTS lines are counted across all releases, not just those for one platform,
// so lts/-1 means the same line everywhere.
func ltsCodename(alias string, sorted []parsed) (string, error) {
	var lines []string
	for _, r := range sorted {
		if r.LTS != "" && !slices.ContainsFunc(lines, func(l string) bool { return strings.EqualFold(l, string(r.LTS)) }) {
			lines = append(lines, string(r.LTS))
		}
	}

	switch {
	case alias == "*":
		if len(lines) == 0 {
			return "", fmt.Errorf("no LTS releases")
		}
		return lines[0], nil
	case strings.HasPrefix(alias, "-"):
		n, err := strconv.Atoi(alias[1:])
		if err != nil || n < 0 {
			return "", fmt.Errorf("invalid LTS offset %q", alias)
		}
		if n >= len(lines) {
			return "", fmt.Errorf("only %d LTS lines exist", len(lines))
		}
		return lines[n], nil
	}

	for _, l := range lines {
		if strings.EqualFold(l, alias) {
			return l, nil
		}
	}
	return "", fmt.Errorf("unknown LTS codename %q", alias)
}

func sortNewestFirst(releases []Release) []parsed {
	out := make([]parsed, 0, len(releases))
	for _, r := range releases {
		v, err := semver.NewVersion(r.Version)
		if err != nil {
			continue
		}
		out = append(out, parsed{Release: r, v: v})
	}
	slices.SortStableFunc(out, func(a, b parsed) int { return b.v.Compare(a.v) })
	return out
}
