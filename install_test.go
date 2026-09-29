package main

import (
	"os"
	"testing"

	"github.com/alecthomas/kong"
	"gotest.tools/v3/assert"

	"github.com/circleci-functions/setup-node/nodedist"
)

func TestMirrorDefaultIsOfficialServer(t *testing.T) {
	t.Setenv("NODE_MIRROR", "")
	assert.NilError(t, os.Unsetenv("NODE_MIRROR"))

	var flags cli
	parser, err := kong.New(&flags)
	assert.NilError(t, err)
	_, err = parser.Parse(nil)
	assert.NilError(t, err)
	assert.Equal(t, flags.Install.Mirror, nodedist.DefaultURL)
}

func TestIsOfficialServer(t *testing.T) {
	tests := []struct {
		mirror string
		want   bool
	}{
		{mirror: "https://nodejs.org/dist", want: true},
		{mirror: "https://nodejs.org/dist/", want: true},
		{mirror: "http://NodeJS.org:80/download/release", want: true},
		{mirror: "https://mirror.example.com/node"},
		{mirror: "https://nodejs.org.example.com/dist"},
		{mirror: ""},
	}
	for _, tc := range tests {
		t.Run(tc.mirror, func(t *testing.T) {
			assert.Equal(t, isOfficialServer(tc.mirror), tc.want)
		})
	}
}
