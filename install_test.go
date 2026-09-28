package main

import (
	"testing"

	"gotest.tools/v3/assert"
)

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
