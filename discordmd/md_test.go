package discordmd

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/yuin/goldmark/ast"
)

func strcmp(t *testing.T, name, got, expected string) {
	t.Helper()
	if got != expected {
		t.Errorf("Mismatch %s:\n"+
			"expected %q\n"+
			"got      %q", name, expected, got)
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

func dump(n ast.Node, src []byte) string {
	// goldmark is a dogshit library with a god awful API.
	// To work around this joke, we will just hijack os.Stdout and os.Stderr to
	// get the output of the Dump function.
	oldStdout := os.Stdout
	oldStderr := os.Stderr
	defer func() {
		os.Stdout = oldStdout
		os.Stderr = oldStderr
	}()

	r, w, _ := os.Pipe()

	os.Stdout = w
	os.Stderr = w

	go func() {
		n.Dump(src, 0)
		w.Close()
	}()

	var buf strings.Builder
	io.Copy(&buf, r)
	r.Close()

	return buf.String()
}
