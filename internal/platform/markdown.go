package platform

import (
	"bytes"
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Markdown is the one renderer for all HTML notid serves or sends. Raw HTML in
// the source is dropped by goldmark, and bluemonday cleans the output again.
type Markdown struct {
	md     goldmark.Markdown
	policy *bluemonday.Policy
	strict *bluemonday.Policy
	// ImageURL, when set, rewrites every image source. The renderer for
	// readers sends remote images through the proxy; the one for peers keeps them.
	ImageURL func(src string) string
}

func NewMarkdown() *Markdown {
	policy := bluemonday.UGCPolicy()
	policy.RequireNoFollowOnLinks(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	m := &Markdown{policy: policy, strict: bluemonday.StrictPolicy()}
	m.md = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(imageSources{m}, 999))),
	)
	return m
}

// imageSources applies Markdown.ImageURL to each image in a document.
type imageSources struct{ m *Markdown }

func (t imageSources) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	if t.m.ImageURL == nil {
		return
	}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if img, ok := n.(*ast.Image); ok && entering {
			img.Destination = []byte(t.m.ImageURL(string(img.Destination)))
		}
		return ast.WalkContinue, nil
	})
}

// Text renders Markdown to plain text of at most max bytes, cut at a word, for
// excerpts. It holds no markup; the web app escapes it like any other text.
func (m *Markdown) Text(source string, max int) string {
	var buf bytes.Buffer
	if err := m.md.Convert([]byte(source), &buf); err != nil {
		panic(fmt.Sprintf("render markdown: %v", err))
	}
	// Block ends become spaces so words in adjacent paragraphs stay apart.
	spaced := strings.NewReplacer("</p>", " </p>", "<br>", " ", "</li>", " </li>", "</h", " </h").Replace(buf.String())
	text := strings.Join(strings.Fields(html.UnescapeString(m.strict.Sanitize(spaced))), " ")
	if len(text) <= max {
		return text
	}
	cut := text[:max]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndexByte(cut, ' '); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " .,;:") + "…"
}

// HTML renders Markdown to sanitized HTML.
func (m *Markdown) HTML(source string) string {
	var buf bytes.Buffer
	if err := m.md.Convert([]byte(source), &buf); err != nil {
		// goldmark only fails on writer errors, which a bytes.Buffer never returns.
		panic(fmt.Sprintf("render markdown: %v", err))
	}
	return m.policy.Sanitize(buf.String())
}

// FromHTML converts remote HTML without a Markdown source to Markdown. The
// HTML is sanitized first, so nothing unsafe survives into storage.
func (m *Markdown) FromHTML(html string) (string, error) {
	out, err := htmltomarkdown.ConvertString(m.policy.Sanitize(html))
	if err != nil {
		return "", fmt.Errorf("convert html: %w", err)
	}
	return out, nil
}
