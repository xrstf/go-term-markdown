package markdown

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	text "github.com/MichaelMure/go-term-text"
	md "github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
)

/*

Here are the possible cases for the AST. You can render it using PlantUML.

@startuml

(*) --> Document
BlockQuote --> BlockQuote
BlockQuote --> CodeBlock
BlockQuote --> List
BlockQuote --> Paragraph
Del --> Emph
Del --> Strong
Del --> Text
Document --> BlockQuote
Document --> CodeBlock
Document --> Heading
Document --> HorizontalRule
Document --> HTMLBlock
Document --> List
Document --> Paragraph
Document --> Table
Emph --> Text
Heading --> Code
Heading --> Del
Heading --> Emph
Heading --> HTMLSpan
Heading --> Image
Heading --> Link
Heading --> Strong
Heading --> Text
Image --> Text
Link --> Image
Link --> Text
ListItem --> List
ListItem --> Paragraph
List --> ListItem
Paragraph --> Code
Paragraph --> Del
Paragraph --> Emph
Paragraph --> Hardbreak
Paragraph --> HTMLSpan
Paragraph --> Image
Paragraph --> Link
Paragraph --> Strong
Paragraph --> Text
Strong --> Emph
Strong --> Text
TableBody --> TableRow
TableCell --> Code
TableCell --> Del
TableCell --> Emph
TableCell --> HTMLSpan
TableCell --> Image
TableCell --> Link
TableCell --> Strong
TableCell --> Text
TableHeader --> TableRow
TableRow --> TableCell
Table --> TableBody
Table --> TableHeader

@enduml

*/

var _ md.Renderer = &renderer{}

type renderer struct {
	// maximum line width allowed
	lineWidth int
	// constant left padding to apply
	leftPad int

	// all the custom left paddings, without the fixed space from leftPad
	padAccumulator []string

	// one-shot indent for the first line of the inline content
	indent string

	// for Heading, Paragraph, HTMLBlock and TableCell, accumulate the content of
	// the child nodes (Link, Text, Image, formatting ...). The result
	// is then rendered appropriately when exiting the node.
	inlineAccumulator strings.Builder

	// record and render the heading numbering
	headingNumbering headingNumbering
	headingShade     levelShadeFmt

	blockQuoteLevel int
	blockQuoteShade levelShadeFmt

	table *tableRenderer
}

// / NewRenderer creates a new instance of the console renderer
func NewRenderer(lineWidth int, leftPad int, opts ...Options) *renderer {
	r := &renderer{
		lineWidth:       lineWidth,
		leftPad:         leftPad,
		padAccumulator:  make([]string, 0, 10),
		headingShade:    shade(defaultHeadingShades),
		blockQuoteShade: shade(defaultQuoteShades),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func (r *renderer) pad() string {
	return strings.Repeat(" ", r.leftPad) + strings.Join(r.padAccumulator, "")
}

func (r *renderer) addPad(pad string) {
	r.padAccumulator = append(r.padAccumulator, pad)
}

func (r *renderer) popPad() {
	r.padAccumulator = r.padAccumulator[:len(r.padAccumulator)-1]
}

func (r *renderer) RenderNode(w io.Writer, node ast.Node, entering bool) ast.WalkStatus {
	// TODO: remove
	// if node.AsLeaf() != nil {
	// 	fmt.Printf("%T, %v (%s)\n", node, entering, string(node.AsLeaf().Literal))
	// } else {
	// 	fmt.Printf("%T, %v\n", node, entering)
	// }

	switch node := node.(type) {
	case *ast.Document:
		// Nothing to do

	case *ast.BlockQuote:
		// set and remove a colored bar on the left
		if entering {
			r.blockQuoteLevel++
			r.addPad(r.blockQuoteShade(r.blockQuoteLevel)("┃ "))
		} else {
			r.blockQuoteLevel--
			r.popPad()
		}

	case *ast.List:
		// extra new line at the end of a list *if* next is not a list
		if next := ast.GetNextNode(node); !entering && next != nil {
			_, parentIsListItem := node.GetParent().(*ast.ListItem)
			_, nextIsList := next.(*ast.List)
			if !nextIsList && !parentIsListItem {
				_, _ = fmt.Fprintln(w)
			}
		}

	case *ast.ListItem:
		// write the prefix, add a padding if needed, and let Paragraph handle the rest
		if entering {
			switch {
			// numbered list
			case node.ListFlags&ast.ListTypeOrdered != 0:
				itemNumber := 1
				siblings := node.GetParent().GetChildren()
				for _, sibling := range siblings {
					if sibling == node {
						break
					}
					itemNumber++
				}
				prefix := fmt.Sprintf("%d. ", itemNumber)
				r.indent = r.pad() + Green(prefix)
				r.addPad(strings.Repeat(" ", text.Len(prefix)))

			// header of a definition
			case node.ListFlags&ast.ListTypeTerm != 0:
				r.inlineAccumulator.WriteString(greenOn)

			// content of a definition
			case node.ListFlags&ast.ListTypeDefinition != 0:
				r.addPad("  ")

			// no flags means it's the normal bullet point list
			default:
				r.indent = r.pad() + Green("• ")
				r.addPad("  ")
			}
		} else {
			switch {
			// numbered list
			case node.ListFlags&ast.ListTypeOrdered != 0:
				r.popPad()

			// header of a definition
			case node.ListFlags&ast.ListTypeTerm != 0:
				r.inlineAccumulator.WriteString(colorOff)

			// content of a definition
			case node.ListFlags&ast.ListTypeDefinition != 0:
				r.popPad()
				_, _ = fmt.Fprintln(w)

			// no flags means it's the normal bullet point list
			default:
				r.popPad()
			}
		}

	case *ast.Paragraph:
		// on exiting, collect and format the accumulated content
		if !entering {
			content := r.inlineAccumulator.String()
			r.inlineAccumulator.Reset()

			var out string
			if r.indent != "" {
				out, _ = text.WrapWithPadIndent(content, r.lineWidth, r.indent, r.pad())
				r.indent = ""
			} else {
				out, _ = text.WrapWithPad(content, r.lineWidth, r.pad())
			}
			_, _ = fmt.Fprint(w, out, "\n")

			// extra line break in some cases
			if next := ast.GetNextNode(node); next != nil {
				switch next.(type) {
				case *ast.Paragraph, *ast.Heading, *ast.HorizontalRule,
					*ast.CodeBlock, *ast.HTMLBlock:
					_, _ = fmt.Fprintln(w)
				}
			}
		}

	case *ast.Heading:
		if !entering {
			r.renderHeading(w, node.Level)
		}

	case *ast.HorizontalRule:
		r.renderHorizontalRule(w)

	case *ast.Emph:
		if entering {
			r.inlineAccumulator.WriteString(italicOn)
		} else {
			r.inlineAccumulator.WriteString(italicOff)
		}

	case *ast.Strong:
		if entering {
			r.inlineAccumulator.WriteString(boldOn)
		} else {
			// This is super silly but some terminals, instead of having
			// the ANSI code SGR 21 do "bold off" like the logic would guide,
			// do "double underline" instead. This is madness.

			// To resolve that problem, we take a snapshot of the escape state,
			// remove the bold, then output "reset all" + snapshot
			es := text.EscapeState{}
			es.Witness(r.inlineAccumulator.String())
			es.Bold = false
			r.inlineAccumulator.WriteString(resetAll)
			r.inlineAccumulator.WriteString(es.FormatString())
		}

	case *ast.Del:
		if entering {
			r.inlineAccumulator.WriteString(crossedOutOn)
		} else {
			r.inlineAccumulator.WriteString(crossedOutOff)
		}

	case *ast.Link:
		if entering {
			r.inlineAccumulator.WriteString("[")
			r.inlineAccumulator.WriteString(string(ast.GetFirstChild(node).AsLeaf().Literal))
			r.inlineAccumulator.WriteString("](")
			r.inlineAccumulator.WriteString(Blue(string(node.Destination)))
			if len(node.Title) > 0 {
				r.inlineAccumulator.WriteString(" ")
				r.inlineAccumulator.WriteString(string(node.Title))
			}
			r.inlineAccumulator.WriteString(")")
			return ast.SkipChildren
		}

	case *ast.Image:
		if entering {
			// the alt text/title is weirdly parsed and is actually
			// a child text of this node
			var title string
			if len(node.Children) == 1 {
				if t, ok := node.Children[0].(*ast.Text); ok {
					title = string(t.Literal)
				}
			}

			str, rendered := r.renderImage(
				string(node.Destination), title,
				r.lineWidth-r.leftPad,
			)

			if rendered {
				r.inlineAccumulator.WriteString("\n")
				r.inlineAccumulator.WriteString(str)
				r.inlineAccumulator.WriteString("\n\n")
			} else {
				r.inlineAccumulator.WriteString(str)
				r.inlineAccumulator.WriteString("\n")
			}

			return ast.SkipChildren
		}

	case *ast.Text:
		if string(node.Literal) == "\n" {
			break
		}
		content := string(node.Literal)
		if shouldCleanText(node) {
			content = removeLineBreak(content)
		}
		r.inlineAccumulator.WriteString(content)

	case *ast.HTMLBlock:
		r.renderHTMLBlock(w, node)

	case *ast.CodeBlock:
		r.renderCodeBlock(w, node)

	case *ast.Softbreak:
		// not actually implemented in gomarkdown
		r.inlineAccumulator.WriteString("\n")

	case *ast.Hardbreak:
		r.inlineAccumulator.WriteString("\n")

	case *ast.Code:
		r.inlineAccumulator.WriteString(BlueBgItalic(string(node.Literal)))

	case *ast.HTMLSpan:
		r.inlineAccumulator.WriteString(Red(string(node.Literal)))

	case *ast.Table:
		if entering {
			r.table = newTableRenderer()
		} else {
			r.table.Render(w, r.leftPad, r.lineWidth)
			r.table = nil
		}

	case *ast.TableCell:
		if !entering {
			content := r.inlineAccumulator.String()
			r.inlineAccumulator.Reset()

			align := CellAlignLeft
			switch node.Align {
			case ast.TableAlignmentRight:
				align = CellAlignRight
			case ast.TableAlignmentCenter:
				align = CellAlignCenter
			}

			if node.IsHeader {
				r.table.AddHeaderCell(content, align)
			} else {
				r.table.AddBodyCell(content, CellAlignCopyHeader)
			}
		}

	case *ast.TableHeader, *ast.TableBody, *ast.TableFooter:
		// nothing to do

	case *ast.TableRow:
		if _, ok := node.Parent.(*ast.TableBody); ok && entering {
			r.table.NextBodyRow()
		}
		if _, ok := node.Parent.(*ast.TableFooter); ok && entering {
			r.table.NextBodyRow()
		}

	default:
		panic(fmt.Sprintf("Unknown node type %T", node))
	}

	return ast.GoToNext
}

func (*renderer) RenderHeader(w io.Writer, node ast.Node) {}

func (*renderer) RenderFooter(w io.Writer, node ast.Node) {}

func (r *renderer) renderHorizontalRule(w io.Writer) {
	_, _ = fmt.Fprintf(w, "%s%s\n\n", r.pad(), strings.Repeat("─", r.lineWidth-r.leftPad))
}

func (r *renderer) renderHeading(w io.Writer, level int) {
	content := r.inlineAccumulator.String()
	r.inlineAccumulator.Reset()

	// render the full line with the headingNumbering
	r.headingNumbering.Observe(level)
	content = fmt.Sprintf("%s %s", r.headingNumbering.Render(), content)
	content = r.headingShade(level)(content)

	// wrap if needed
	wrapped, _ := text.WrapWithPad(content, r.lineWidth, r.pad())
	_, _ = fmt.Fprintln(w, wrapped)

	// render the underline, if any
	if level == 1 {
		_, _ = fmt.Fprintf(w, "%s%s\n", r.pad(), strings.Repeat("─", r.lineWidth-r.leftPad))
	}

	_, _ = fmt.Fprintln(w)
}

func (r *renderer) renderCodeBlock(w io.Writer, node *ast.CodeBlock) {
	code := string(node.Literal)
	r.renderFormattedCodeBlock(w, code)
}

func (r *renderer) renderFormattedCodeBlock(w io.Writer, code string) {
	// remove the trailing line break
	code = strings.TrimRight(code, "\n")

	r.addPad(GreenBold("┃ "))
	output, _ := text.WrapWithPad(code, r.lineWidth, r.pad())
	r.popPad()

	_, _ = fmt.Fprint(w, output)

	_, _ = fmt.Fprintf(w, "\n\n")
}

func (r *renderer) renderHTMLBlock(w io.Writer, node *ast.HTMLBlock) {
	// remove the trailing line break
	code := string(node.Literal)
	code = strings.TrimRight(code, "\n")

	r.addPad(GreenBold("┃ "))
	output, _ := text.WrapWithPad(code, r.lineWidth, r.pad())
	r.popPad()

	_, _ = fmt.Fprint(w, output)

	_, _ = fmt.Fprintf(w, "\n\n")
}

func (r *renderer) renderImage(dest string, title string, lineWidth int) (result string, rendered bool) {
	title = strings.ReplaceAll(title, "\n", "")
	title = strings.TrimSpace(title)
	dest = strings.ReplaceAll(dest, "\n", "")
	dest = strings.TrimSpace(dest)

	return fmt.Sprintf("![%s](%s)", title, Blue(dest)), false
}

func imageFromDestination(dest string) (io.ReadCloser, error) {
	client := http.Client{
		Timeout: 5 * time.Second,
	}

	if strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://") {
		res, err := client.Get(dest)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("http: %v", http.StatusText(res.StatusCode))
		}

		return res.Body, nil
	}

	return os.Open(dest)
}

func removeLineBreak(text string) string {
	lines := strings.Split(text, "\n")

	if len(lines) <= 1 {
		return text
	}

	for i, l := range lines {
		switch i {
		case 0:
			lines[i] = strings.TrimRightFunc(l, unicode.IsSpace)
		case len(lines) - 1:
			lines[i] = strings.TrimLeftFunc(l, unicode.IsSpace)
		default:
			lines[i] = strings.TrimFunc(l, unicode.IsSpace)
		}
	}
	return strings.Join(lines, " ")
}

func shouldCleanText(node ast.Node) bool {
	for node != nil {
		switch node.(type) {
		case *ast.BlockQuote:
			return false

		case *ast.Heading, *ast.Image, *ast.Link,
			*ast.TableCell, *ast.Document, *ast.ListItem:
			return true
		}

		node = node.GetParent()
	}

	panic("bad markdown document or missing case")
}
