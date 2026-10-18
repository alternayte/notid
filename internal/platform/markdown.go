package platform

import (
	"bytes"
	"fmt"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// Markdown is the one renderer for all HTML notid serves or sends. Raw HTML in
// the source is dropped by goldmark, and bluemonday cleans the output again.
type Markdown struct {
	md     goldmark.Markdown
	policy *bluemonday.Policy
}

func NewMarkdown() *Markdown {
	policy := bluemonday.UGCPolicy()
	policy.RequireNoFollowOnLinks(true)
	policy.AddTargetBlankToFullyQualifiedLinks(true)
	return &Markdown{
		md:     goldmark.New(goldmark.WithExtensions(extension.GFM)),
		policy: policy,
	}
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
