"""Renders Markdown reports to HTML and publishes them to GCS.

The Pre-GA Managed Agents sandbox does not have real GCS credentials and the
on-host `gsutil` is mocked, so we cannot ask the Agent to upload. Instead the
Agent returns the full markdown via `report_md` in its JSON, and Cloud Run
(which runs as a real service account with `storage.objectAdmin`) handles
rendering and publishing.
"""

import datetime as dt
from typing import Final

import markdown
from google.cloud import storage


_MARKDOWN_EXTS: Final = ["fenced_code", "tables", "footnotes"]

_CSS: Final = """
:root { color-scheme: light; }
body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Noto Sans TC",
                 "PingFang TC", "Hiragino Sans GB", sans-serif;
    max-width: 760px;
    margin: 0 auto;
    padding: 32px 24px 80px;
    line-height: 1.7;
    color: #1f2937;
    background: #f9fafb;
}
.banner {
    background: #111827;
    color: #f9fafb;
    padding: 8px 16px;
    border-radius: 8px;
    font-size: 13px;
    margin-bottom: 24px;
    display: inline-block;
}
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
"""


def _wrap_html(*, topic: str, body_html: str, version: int) -> str:
    ts = dt.datetime.now(dt.timezone.utc).strftime("%Y-%m-%d %H:%M UTC")
    banner = f'<div class="banner">v{version} · 更新於 {ts}</div>'
    return (
        "<!DOCTYPE html>\n"
        f'<html lang="zh-TW"><head><meta charset="UTF-8">'
        f'<meta name="viewport" content="width=device-width, initial-scale=1">'
        f"<title>{topic}</title>"
        f"<style>{_CSS}</style></head>"
        f"<body>{banner}\n{body_html}</body></html>"
    )


class GcsPublisher:
    def __init__(self, *, bucket_name: str) -> None:
        self._client = storage.Client()
        self._bucket = self._client.bucket(bucket_name)

    def publish(
        self,
        *,
        report_id: str,
        topic: str,
        report_md: str,
        version: int,
        snapshot_previous: int | None = None,
    ) -> str:
        """Render `report_md` to HTML and upload to gs://{bucket}/{report_id}/index.html.

        If `snapshot_previous` is given, the current index.html is first copied
        to `snapshots/v{snapshot_previous}.html` before being overwritten.

        Returns the public HTTPS URL of the published report.
        """
        if snapshot_previous is not None:
            self._snapshot(report_id, snapshot_previous)

        body_html = markdown.markdown(report_md, extensions=_MARKDOWN_EXTS)
        full_html = _wrap_html(topic=topic, body_html=body_html, version=version)

        blob = self._bucket.blob(f"{report_id}/index.html")
        blob.cache_control = "no-cache, max-age=0"
        blob.content_language = "zh-TW"
        blob.upload_from_string(
            full_html, content_type="text/html; charset=utf-8"
        )
        return self.public_url(report_id)

    def public_url(self, report_id: str) -> str:
        return f"https://storage.googleapis.com/{self._bucket.name}/{report_id}/index.html"

    def _snapshot(self, report_id: str, previous_version: int) -> None:
        src = self._bucket.blob(f"{report_id}/index.html")
        if not src.exists():
            return  # nothing to snapshot (first version)
        dst_name = f"{report_id}/snapshots/v{previous_version}.html"
        self._bucket.copy_blob(src, self._bucket, dst_name)
