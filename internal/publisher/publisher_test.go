package publisher

import (
	"strings"
	"testing"
	"time"
)

func TestRenderHTML(t *testing.T) {
	md := "# 標題\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n見註腳[^1]\n\n[^1]: https://example.com\n\n<script>alert(1)</script>\n"
	out, err := RenderHTML("<主題>", md, 3, time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<title>&lt;主題&gt;</title>", "<table>", "footnote", "v3 · 更新於 2026-11-02 08:00 UTC", "<h1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "<script>alert") {
		t.Error("raw HTML from model must not be rendered")
	}
}

func TestURLs(t *testing.T) {
	if got := PublicURL("b", "r"); got != "https://storage.googleapis.com/b/r/index.html" {
		t.Fatal(got)
	}
	if got := SnapshotURL("b", "r", 2); got != "https://storage.googleapis.com/b/r/snapshots/v2.html" {
		t.Fatal(got)
	}
}
