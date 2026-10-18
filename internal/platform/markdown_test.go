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
