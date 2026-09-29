package nodeversion

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		assert.NilError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0600))
	}
	return dir
}

func TestFromFile(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		file  string
		want  string
	}{
		{name: "nvmrc", files: map[string]string{".nvmrc": "22.11.0\n"}, file: ".nvmrc", want: "22.11.0"},
		{name: "nvmrc with v", files: map[string]string{".nvmrc": "v20\n"}, file: ".nvmrc", want: "20"},
		{name: "nvmrc alias", files: map[string]string{".nvmrc": "lts/iron\n"}, file: ".nvmrc", want: "lts/iron"},
		{name: "nvmrc with comment", files: map[string]string{".nvmrc": "# pinned for CI\n22\n"}, file: ".nvmrc", want: "22"},
		{name: "nvmrc crlf", files: map[string]string{".nvmrc": "22.11.0\r\n"}, file: ".nvmrc", want: "22.11.0"},
		{name: "nvmrc crlf with comment", files: map[string]string{".nvmrc": "# pinned for CI\r\n22\r\n"}, file: ".nvmrc", want: "22"},
		{name: "node-version", files: map[string]string{".node-version": "24.1.0"}, file: ".node-version", want: "24.1.0"},
		{name: "tool-versions", files: map[string]string{".tool-versions": "python 3.12.1\nnodejs 22.3.0\n"}, file: ".tool-versions", want: "22.3.0"},
		{name: "tool-versions node", files: map[string]string{".tool-versions": "node 20.1.0\n"}, file: ".tool-versions", want: "20.1.0"},
		{name: "tool-versions crlf", files: map[string]string{".tool-versions": "nodejs 20.1.0\r\npython 3.11\r\n"}, file: ".tool-versions", want: "20.1.0"},
		{name: "mise string", files: map[string]string{"mise.toml": "[tools]\nnode = \"22\"\n"}, file: "mise.toml", want: "22"},
		{name: "mise table", files: map[string]string{"mise.toml": "[tools]\nnode = { version = \"24.2.0\" }\n"}, file: "mise.toml", want: "24.2.0"},
		{
			name:  "package.json volta wins",
			files: map[string]string{"package.json": `{"volta":{"node":"20.1.0"},"engines":{"node":">=18"}}`},
			file:  "package.json", want: "20.1.0",
		},
		{
			name:  "package.json devEngines object",
			files: map[string]string{"package.json": `{"devEngines":{"runtime":{"name":"node","version":"^22"}},"engines":{"node":">=18"}}`},
			file:  "package.json", want: "^22",
		},
		{
			name:  "package.json devEngines list",
			files: map[string]string{"package.json": `{"devEngines":{"runtime":[{"name":"bun","version":"1"},{"name":"Node","version":"24"}]}}`},
			file:  "package.json", want: "24",
		},
		{
			name:  "package.json engines",
			files: map[string]string{"package.json": `{"engines":{"node":">=20 <23"}}`},
			file:  "package.json", want: ">=20 <23",
		},
		{
			name: "package.json volta extends",
			files: map[string]string{
				"package.json": `{"volta":{"extends":"base.json"}}`,
				"base.json":    `{"volta":{"node":"22.4.0"}}`,
			},
			file: "package.json", want: "22.4.0",
		},
		{
			name: "package.json volta extends chain",
			files: map[string]string{
				"package.json": `{"volta":{"extends":"middle.json"}}`,
				"middle.json":  `{"volta":{"extends":"base.json"}}`,
				"base.json":    `{"volta":{"node":"22.4.0"}}`,
			},
			file: "package.json", want: "22.4.0",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeFiles(t, tc.files)
			got, err := FromFile(filepath.Join(dir, tc.file))
			assert.NilError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestFromFile_Errors(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string]string
		file    string
		wantErr string
	}{
		{name: "missing", file: ".nvmrc", wantErr: "no such file"},
		{name: "empty", files: map[string]string{".nvmrc": "\n\n"}, file: ".nvmrc", wantErr: "no Node.js version found"},
		{name: "package.json without node", files: map[string]string{"package.json": `{"name":"app"}`}, file: "package.json", wantErr: "no Node.js version found"},
		{name: "mise without version", files: map[string]string{"mise.toml": "[tools.node]\npostinstall = \"x\"\n"}, file: "mise.toml", wantErr: "no Node.js version found"},
		{name: "volta extends itself", files: map[string]string{"package.json": `{"volta":{"extends":"package.json"}}`}, file: "package.json", wantErr: "volta.extends cycle"},
		{
			name: "volta extends cycle",
			files: map[string]string{
				"package.json": `{"volta":{"extends":"base.json"}}`,
				"base.json":    `{"volta":{"extends":"package.json"}}`,
			},
			file: "package.json", wantErr: "volta.extends cycle",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeFiles(t, tc.files)
			_, err := FromFile(filepath.Join(dir, tc.file))
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestDefault(t *testing.T) {
	tests := []struct {
		name       string
		files      map[string]string
		wantSpec   string
		wantSource string
	}{
		{name: "nvmrc", files: map[string]string{".nvmrc": "22"}, wantSpec: "22", wantSource: ".nvmrc"},
		{name: "node-version", files: map[string]string{".node-version": "20"}, wantSpec: "20", wantSource: ".node-version"},
		{name: "nvmrc before node-version", files: map[string]string{".nvmrc": "22", ".node-version": "20"}, wantSpec: "22", wantSource: ".nvmrc"},
		{name: "nothing", wantSpec: "lts/*"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := writeFiles(t, tc.files)
			spec, source, err := Default(dir)
			assert.NilError(t, err)
			assert.Equal(t, spec, tc.wantSpec)
			if tc.wantSource == "" {
				assert.Equal(t, source, "")
			} else {
				assert.Equal(t, source, filepath.Join(dir, tc.wantSource))
			}
		})
	}
}
