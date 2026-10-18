package federation

import (
	"encoding/json"
	"testing"
)

func TestARemotePageKeepsItsLinkAndKnowsWhenItIsAnImage(t *testing.T) {
	cases := map[string]struct {
		page         string
		url, preview string
		image        bool
	}{
		"lemmy image post": {
			page:  `{"attachment":[{"type":"Link","href":"https://lemmy.example/pictrs/image/a.jpeg","mediaType":"image/jpeg"}],"image":{"type":"Image","url":"https://lemmy.example/pictrs/image/a.jpeg"}}`,
			url:   "https://lemmy.example/pictrs/image/a.jpeg",
			image: true,
		},
		"lemmy image post without a media type": {
			page:  `{"attachment":[{"type":"Link","href":"https://lemmy.example/pictrs/image/b.webp"}]}`,
			url:   "https://lemmy.example/pictrs/image/b.webp",
			image: true,
		},
		"lemmy link post with a thumbnail": {
			page:    `{"attachment":[{"type":"Link","href":"https://news.example/story","mediaType":"text/html; charset=utf-8"}],"image":{"type":"Image","url":"https://lemmy.example/pictrs/image/t.jpeg"}}`,
			url:     "https://news.example/story",
			preview: "https://lemmy.example/pictrs/image/t.jpeg",
		},
		"piefed image post": {
			page:  `{"attachment":[{"type":"Image","url":"https://piefed.example/static/media/posts/c.png","name":"a lathe"}]}`,
			url:   "https://piefed.example/static/media/posts/c.png",
			image: true,
		},
		"document with a url list": {
			page:  `{"attachment":[{"type":"Document","mediaType":"image/png","url":[{"type":"Link","href":"https://masto.example/d.png"}]}]}`,
			url:   "https://masto.example/d.png",
			image: true,
		},
		"text post": {page: `{"attachment":[]}`},
		"script link": {
			page: `{"attachment":[{"type":"Link","href":"javascript:alert(1)"}],"image":{"type":"Image","url":"data:image/png;base64,AA"}}`,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var o objectDoc
			if err := json.Unmarshal([]byte(c.page), &o); err != nil {
				t.Fatal(err)
			}
			p := remotePost(o, "")
			if p.URL != c.url || p.URLImage != c.image || p.PreviewImage != c.preview {
				t.Fatalf("got url %q image %v preview %q", p.URL, p.URLImage, p.PreviewImage)
			}
		})
	}
}
