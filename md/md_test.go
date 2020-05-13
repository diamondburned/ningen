package md

import (
	"testing"
)

func strcmp(t *testing.T, name, got, expected string) {
	t.Helper()
	if got != expected {
		t.Fatal("Mismatch", name, "<expected/got>:\n", expected, "\n", got)
	}
}

func TestParses(t *testing.T) {
	var tests = []string{
		">\n```\r",
		"```\n\n",
	}

	for _, test := range tests {
		Parse([]byte(test))
	}
}
