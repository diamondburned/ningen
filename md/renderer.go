package md

import (
	"io"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
)

// BasicRenderer renders the package's ast.Nodes into simple unformatted
// plain text. It serves as an implementation reference. However, this
// implementation does not have a state, which is required for Inline and
// Blockquote.
type BasicRenderer struct{}

var DefaultRenderer renderer.Renderer = &BasicRenderer{}

func (r *BasicRenderer) AddOptions(...renderer.Option) {}

func (r *BasicRenderer) Render(w io.Writer, source []byte, n ast.Node) error {
	return ast.Walk(n, func(node ast.Node, enter bool) (ast.WalkStatus, error) {
		return r.walker(w, source, node, enter), nil
	})
}

func (r *BasicRenderer) walker(w io.Writer, source []byte, n ast.Node, enter bool) ast.WalkStatus {
	switch n := n.(type) {
	case *ast.Document:
		// noop
	case *ast.Blockquote:
		if enter {
			// A blockquote contains a paragraph each line. Because Discord.
			for child := n.FirstChild(); child != nil; child = child.NextSibling() {
				write(w, "> ")
				ast.Walk(child, func(node ast.Node, enter bool) (ast.WalkStatus, error) {
					// We only call when entering, since we don't want to trigger a
					// hard new line after each paragraph.
					if enter {
						return r.walker(w, source, node, enter), nil
					}
					return ast.WalkContinue, nil
				})
			}
		}
		// We've already walked over children ourselves.
		return ast.WalkSkipChildren

	case *ast.Paragraph:
		if !enter {
			write(w, "\n")
		}
	case *ast.FencedCodeBlock:
		if enter {
			// Write the body
			for i := 0; i < n.Lines().Len(); i++ {
				line := n.Lines().At(i)
				write(w, "▏▕  "+string(line.Value(source)))
			}
		}
	case *ast.Link:
		if enter {
			write(w, string(n.Title)+" ("+string(n.Destination)+")")
		}
	case *ast.AutoLink:
		if enter {
			write(w, string(n.URL(source)))
		}
	case *Inline:
		// n.Attr should be used, but since we're in plaintext mode, there is no
		// formatting.
	case *Emoji:
		if enter {
			write(w, ":"+string(n.Name)+":")
		}
	case *Mention:
		if enter {
			switch {
			case n.Channel != nil:
				write(w, "#"+n.Channel.Name)
			case n.GuildUser != nil:
				write(w, "@"+n.GuildUser.Username)
			case n.GuildRole != nil:
				write(w, "@"+n.GuildRole.Name)
			}
		}
	case *ast.String:
		if enter {
			w.Write(n.Value)
		}
	case *ast.Text:
		if enter {
			w.Write(n.Segment.Value(source))
			switch {
			case n.HardLineBreak():
				write(w, "\n\n")
			case n.SoftLineBreak():
				write(w, "\n")
			}
		}
	}
	return ast.WalkContinue
}

func write(w io.Writer, str string) {
	w.Write([]byte(str))
}
