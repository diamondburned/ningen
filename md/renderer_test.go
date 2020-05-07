package md

import (
	"bytes"
	"fmt"
	"testing"
)

const message = `**this is a test.** https://google.com strictly URL.
> be me
> wacky **blockquote**
> fml
> >>> bruh
` + "```" + `go
package main

func main() {
	fmt.Println("Bruh moment.")
}
` + "```" + `
`

func TestRenderer(t *testing.T) {
	node := Parse([]byte(message))
	buff := bytes.Buffer{}
	DefaultRenderer.Render(&buff, []byte(message), node)

	fmt.Println(buff.String())
}
