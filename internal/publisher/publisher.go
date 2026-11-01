// Package publisher renders Markdown reports to HTML and uploads them to GCS.
package publisher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"time"

	"cloud.google.com/go/storage"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

const css = `
:root { color-scheme: light; }
body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans TC",
                 "PingFang TC", "Hiragino Sans GB", sans-serif;
    max-width: 760px; margin: 0 auto; padding: 32px 24px 80px;
    line-height: 1.7; color: #1f2937; background: #f9fafb;
}
.banner { background: #111827; color: #f9fafb; padding: 8px 16px; border-radius: 8px;
    font-size: 13px; margin-bottom: 24px; display: inline-block; }
h1 { font-size: 28px; margin-top: 24px; }
h2 { font-size: 22px; margin-top: 32px; border-bottom: 1px solid #e5e7eb; padding-bottom: 6px; }
h3 { font-size: 18px; margin-top: 24px; }
p { margin: 12px 0; }
ul, ol { padding-left: 28px; }
a { color: #2563eb; text-decoration: none; }
a:hover { text-decoration: underline; }
code { background: #f3f4f6; padding: 2px 6px; border-radius: 4px; font-size: 0.9em; }
pre code { display: block; padding: 12px 16px; overflow-x: auto; }
blockquote { border-left: 4px solid #d1d5db; padding-left: 12px; color: #4b5563; margin: 16px 0; }
table { border-collapse: collapse; width: 100%; margin: 16px 0; }
th, td { border: 1px solid #e5e7eb; padding: 8px 12px; text-align: left; }
th { background: #f3f4f6; }
.footnote-ref a, .footnote-backref { font-size: 0.85em; }
hr { border: 0; border-top: 1px solid #e5e7eb; margin: 32px 0; }
`

// md renders GFM (tables, strikethrough, autolinks) plus footnotes. Raw HTML
// in the model's output is not rendered (goldmark's safe default).
var md = goldmark.New(goldmark.WithExtensions(extension.GFM, extension.Footnote))

// RenderHTML converts the Markdown report into a standalone HTML page.
func RenderHTML(topic, reportMD string, version int, now time.Time) (string, error) {
	var body bytes.Buffer
	if err := md.Convert([]byte(reportMD), &body); err != nil {
		return "", err
	}
	ts := now.UTC().Format("2006-01-02 15:04 UTC")
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-TW"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>%s</style></head><body><div class="banner">v%d · 更新於 %s</div>
%s</body></html>`, html.EscapeString(topic), css, version, ts, body.String()), nil
}

func PublicURL(bucket, reportID string) string {
	return fmt.Sprintf("https://storage.googleapis.com/%s/%s/index.html", bucket, reportID)
}

func SnapshotURL(bucket, reportID string, version int) string {
	return fmt.Sprintf("https://storage.googleapis.com/%s/%s/snapshots/v%d.html", bucket, reportID, version)
}

type GCS struct {
	bucket string
	b      *storage.BucketHandle
}

func NewGCS(client *storage.Client, bucket string) *GCS {
	return &GCS{bucket: bucket, b: client.Bucket(bucket)}
}

// Publish renders reportMD and uploads it to gs://bucket/reportID/index.html.
// If snapshotPrevious > 0, the current index.html is first copied to
// snapshots/v{snapshotPrevious}.html. Returns the public URL.
func (g *GCS) Publish(ctx context.Context, reportID, topic, reportMD string, version, snapshotPrevious int) (string, error) {
	if snapshotPrevious > 0 {
		if err := g.snapshot(ctx, reportID, snapshotPrevious); err != nil {
			return "", fmt.Errorf("snapshot: %w", err)
		}
	}
	page, err := RenderHTML(topic, reportMD, version, time.Now())
	if err != nil {
		return "", err
	}
	w := g.b.Object(reportID + "/index.html").NewWriter(ctx)
	w.ContentType = "text/html; charset=utf-8"
	w.CacheControl = "no-cache, max-age=0"
	w.ContentLanguage = "zh-TW"
	if _, err := w.Write([]byte(page)); err != nil {
		_ = w.Close()
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	return PublicURL(g.bucket, reportID), nil
}

func (g *GCS) snapshot(ctx context.Context, reportID string, prev int) error {
	src := g.b.Object(reportID + "/index.html")
	if _, err := src.Attrs(ctx); errors.Is(err, storage.ErrObjectNotExist) {
		return nil // first version, nothing to snapshot
	} else if err != nil {
		return err
	}
	dst := g.b.Object(fmt.Sprintf("%s/snapshots/v%d.html", reportID, prev))
	_, err := dst.CopierFrom(src).Run(ctx)
	return err
}
