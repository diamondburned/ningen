package discordmd

import (
	"io"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"go4.org/bytereplacer"
)

var unescaper = bytereplacer.New("\\\\", "\\", "\\", "")

// var doubleBackslash = []byte{'\\', '\\'}

// Unescape handles escape characters. This is a helper function for renderers.
func Unescape(src []byte) []byte {
	return unescaper.Replace(src)
}

type unescapeWriter struct {
	w io.Writer
}

func (w unescapeWriter) Write(b []byte) (int, error) {
	return w.w.Write(Unescape(b))
}

func UnescapeWriter(w io.Writer) io.Writer {
	return unescapeWriter{w}
}

// BasicRenderer renders the package's ast.Nodes into simple unformatted
// plain text. It serves as an implementation reference. However, this
// implementation does not have a state, which is required for Inline and
// Blockquote.
type BasicRenderer struct{}

var DefaultRenderer renderer.Renderer = &BasicRenderer{}

func (r *BasicRenderer) AddOptions(...renderer.Option) {}

func (r *BasicRenderer) Render(w io.Writer, source []byte, n ast.Node) error {
	// Wrap the current writer behind an unescaper.
	w = UnescapeWriter(w)

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
				io.WriteString(w, "> ")
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
			io.WriteString(w, "\n")
		}
	case *ast.FencedCodeBlock:
		if enter {
			// Write the body
			for i := 0; i < n.Lines().Len(); i++ {
				line := n.Lines().At(i)
				io.WriteString(w, "| "+string(line.Value(source)))
			}
		} else {
			io.WriteString(w, "\n")
		}
	case *ast.Link:
		if enter {
			io.WriteString(w, string(n.Title)+" ("+string(n.Destination)+")")
		}
	case *ast.AutoLink:
		if enter {
			io.WriteString(w, string(n.URL(source)))
		}
	case *Inline:
		// n.Attr should be used, but since we're in plaintext mode, there is no
		// formatting.
	case *Emoji:
		if enter {
			io.WriteString(w, ":"+string(n.Name)+":")
		}
	case *Mention:
		if enter {
			switch {
			case n.Channel != nil:
				io.WriteString(w, "#"+n.Channel.Name)
			case n.GuildUser != nil:
				io.WriteString(w, "@"+n.GuildUser.Username)
			case n.GuildRole != nil:
				io.WriteString(w, "@"+n.GuildRole.Name)
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
				io.WriteString(w, "\n\n")
			case n.SoftLineBreak():
				io.WriteString(w, "\n")
			}
		}
	}
	return ast.WalkContinue
}
