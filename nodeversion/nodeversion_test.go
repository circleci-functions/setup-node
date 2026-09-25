package nodeversion

import (
	"encoding/json"
	"os"
	"testing"

	"gotest.tools/v3/assert"
)

func loadIndex(t *testing.T) []Release {
	t.Helper()
	data, err := os.ReadFile("testdata/index.json")
	assert.NilError(t, err)
	var releases []Release
	assert.NilError(t, json.Unmarshal(data, &releases))
	return releases
}

func TestResolve(t *testing.T) {
	releases := loadIndex(t)

	tests := []struct {
		name   string
		spec   string
		fileID string
		want   string
	}{
		{name: "exact", spec: "22.23.2", want: "v22.23.2"},
		{name: "exact with v", spec: "v22.23.2", want: "v22.23.2"},
		{name: "major", spec: "22", want: "v22.23.3"},
		{name: "major.x", spec: "20.x", want: "v20.20.2"},
		{name: "major.minor", spec: "25.8", want: "v25.8.2"},
		{name: "range", spec: ">=20 <23", want: "v22.23.3"},
		{name: "caret", spec: "^18", want: "v18.20.8"},
		{name: "or", spec: "18 || 20", want: "v20.20.2"},
		{name: "whitespace", spec: "  24\n", want: "v24.21.0"},
		{name: "latest", spec: "latest", want: "v26.10.0"},
		{name: "current", spec: "current", want: "v26.10.0"},
		{name: "node", spec: "node", want: "v26.10.0"},
		{name: "lts star", spec: "lts/*", want: "v24.21.0"},
		{name: "lts codename", spec: "lts/jod", want: "v22.23.3"},
		{name: "lts codename mixed case", spec: "LTS/Iron", want: "v20.20.2"},
		{name: "lts previous", spec: "lts/-1", want: "v22.23.3"},
		{name: "lts offset zero", spec: "lts/-0", want: "v24.21.0"},
		{name: "platform skips releases without the build", spec: "22", fileID: "win-x86-zip", want: "v22.23.3"},
		{name: "platform filters latest", spec: "latest", fileID: "linux-armv7l", want: "v23.11.1"},
		{name: "platform filters within an lts line", spec: "lts/iron", fileID: "linux-armv7l", want: "v20.20.2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.spec, releases, tc.fileID)
			assert.NilError(t, err)
			assert.Equal(t, got.Version, tc.want)
		})
	}
}

func TestResolve_Errors(t *testing.T) {
	releases := loadIndex(t)

	tests := []struct {
		name    string
		spec    string
		fileID  string
		wantErr string
	}{
		{name: "empty", spec: " ", wantErr: "empty version spec"},
		{name: "no match", spec: "19", wantErr: `no release matches "19"`},
		{name: "invalid", spec: "not-a-version", wantErr: `invalid version spec "not-a-version"`},
		{name: "unknown codename", spec: "lts/argon", wantErr: `unknown LTS codename "argon"`},
		{name: "offset too far", spec: "lts/-9", wantErr: "only 4 LTS lines exist"},
		{name: "bad offset", spec: "lts/-x", wantErr: `invalid LTS offset "-x"`},
		{name: "lts line has no build for platform", spec: "lts/*", fileID: "win-x86-zip", wantErr: `no release matching "lts/*" has a win-x86-zip build (newest match: v24.21.0)`},
		{name: "no build for platform", spec: "25", fileID: "linux-armv7l", wantErr: `no release matching "25" has a linux-armv7l build (newest match: v25.9.0)`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(tc.spec, releases, tc.fileID)
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestRelease_Number(t *testing.T) {
	assert.Equal(t, Release{Version: "v22.1.0"}.Number(), "22.1.0")
}
