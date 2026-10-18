package discussions

import (
	"net/url"
	"strings"
	"testing"
)

func TestAPreviewReadsOpenGraphAndFallsBackToTheTitle(t *testing.T) {
	base, _ := url.Parse("https://news.example/2026/story")
	cases := map[string]struct {
		page string
		want pagePreview
	}{
		"open graph": {
			page: `<html><head><title>Site</title><meta property="og:title" content="A  lathe
			restored"><meta property="og:description" content="Six months of work."><meta property="og:image" content="/img/lathe.jpg"></head><body><meta property="og:title" content="late"></body></html>`,
			want: pagePreview{title: "A lathe restored", description: "Six months of work.", image: "https://news.example/img/lathe.jpg"},
		},
		"plain head": {
			page: `<!doctype html><title>Plain page</title><meta name="description" content="About it"><meta name="twitter:image" content="https://cdn.example/t.png">`,
			want: pagePreview{title: "Plain page", description: "About it", image: "https://cdn.example/t.png"},
		},
		"script image": {
			page: `<head><meta property="og:image" content="javascript:alert(1)"></head>`,
			want: pagePreview{},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := parsePreview(strings.NewReader(c.page), base); got != c.want {
				t.Fatalf("got %+v", got)
			}
		})
	}
	long := parsePreview(strings.NewReader("<title>"+strings.Repeat("ä", 400)+"</title>"), base)
	if n := len([]rune(long.title)); n != previewTitleRunes {
		t.Fatalf("title has %d runes", n)
	}
}
