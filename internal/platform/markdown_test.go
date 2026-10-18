package platform

import (
	"strings"
	"testing"
)

func TestInvariant8ScriptAndStyleNeverReachTheHTML(t *testing.T) {
	m := NewMarkdown()
	inputs := []string{
		"hello <script>alert(1)</script> <style>body{display:none}</style>",
		"[x](javascript:alert(1)) <img src=x onerror=alert(1)>",
		"<div style=\"position:fixed\">a</div>",
	}
	for _, in := range inputs {
		out := m.HTML(in)
		for _, bad := range []string{"<script", "<style", "javascript:", "onerror", "style="} {
			if strings.Contains(strings.ToLower(out), bad) {
				t.Errorf("%q rendered %q", in, out)
			}
		}
	}
	md, err := m.FromHTML(`<p>hi <b>there</b></p><script>alert(1)</script>`)
	if err != nil || strings.Contains(md, "alert") || !strings.Contains(md, "**there**") {
		t.Errorf("from html: %q %v", md, err)
	}
	if got := m.HTML("**bold**"); !strings.Contains(got, "<strong>bold</strong>") {
		t.Errorf("markdown: %q", got)
	}
}

func TestTextIsPlainAndCutAtAWord(t *testing.T) {
	m := NewMarkdown()
	got := m.Text("# Planes\n\nA **sharp** iron & a <script>alert(1)</script> flat sole.\n\n- one\n- two", 200)
	if got != "Planes A sharp iron & a alert(1) flat sole. one two" {
		t.Fatalf("text %q", got)
	}
	if got := m.Text("alpha beta gamma delta", 14); got != "alpha beta…" {
		t.Fatalf("cut %q", got)
	}
}

func TestImageSourcesAreRewrittenForReadersOnly(t *testing.T) {
	src := "Look ![bench](https://peer.example/a.png) here"
	m := NewMarkdown()
	if out := m.HTML(src); !strings.Contains(out, `src="https://peer.example/a.png"`) {
		t.Fatalf("a renderer without ImageURL changed the source: %s", out)
	}
	m.ImageURL = func(s string) string { return "/proxy?u=" + s }
	if out := m.HTML(src); !strings.Contains(out, `src="/proxy?u=https://peer.example/a.png"`) {
		t.Fatalf("the image was not rewritten: %s", out)
	}
}
