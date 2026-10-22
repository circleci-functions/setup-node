package node

import (
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestYarnWindowsCacheDir(t *testing.T) {
	home := filepath.Join("C:", "Users", "circleci")

	t.Run("under LOCALAPPDATA", func(t *testing.T) {
		localAppData := filepath.Join("D:", "LocalAppData")
		assert.Equal(t, yarnWindowsCacheDir(localAppData, home), filepath.Join(localAppData, "Yarn", "Cache"))
	})

	t.Run("under home when LOCALAPPDATA is unset", func(t *testing.T) {
		assert.Equal(t, yarnWindowsCacheDir("", home), filepath.Join(home, "AppData", "Local", "Yarn", "Cache"))
	})
}
