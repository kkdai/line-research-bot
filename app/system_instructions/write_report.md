# Stage: WRITE_REPORT

You will receive metadata in the input:
- `report_id`: target GCS path component
- `mode`: "new" | "deepen"
- `previous_version`: integer, present only when mode is "deepen"
- `deepen_request`: free-text user instruction, present only when mode is "deepen"
  (e.g. "第 3 章再深一點，加日文來源")

## Your task

### If mode == "new"
1. Read `/workspace/sources.json`.
2. Write `/workspace/report.md` with this structure (繁體中文):
   - 標題
   - 摘要（500 字）
   - 章節 1-N（每章引用至少 2 個來源，footnote 形式 `[^url]`）
   - 跨來源分歧討論
   - 結論與建議
   - 來源列表

### If mode == "deepen"
1. Read `/workspace/sources.json` and `/workspace/report.md`.
2. Parse `deepen_request` to identify target chapter(s) or change type:
   - If "第 N 章" mentioned, modify only that chapter.
   - If "補 X 來源" mentioned, run additional google_search + url_context, append to sources.json, integrate into relevant chapters.
3. If `deepen_request` references a chapter number that does not exist in `report.md`, **stop and return**:
   ```json
   {"error": "chapter_not_found", "available_chapters": ["1. ...", "2. ..."]}
   ```
4. Rewrite `/workspace/report.md` with changes applied; regenerate summary.

### Render and publish (both modes)
5. `code_execution`: install markdown if missing
   ```bash
   pip install --quiet markdown
   ```
6. Run a Python snippet to render `report.md` → `report.html` with inline CSS:
   - Use `markdown.markdown(text, extensions=["fenced_code", "tables", "footnotes"])`.
   - Wrap output in a complete `<html>` doc with a `<style>` block. Add a top banner:
     `<div class="banner">v{NEW_VERSION} · 更新於 {timestamp}</div>`.
7. If mode == "deepen":
   ```bash
   gsutil -h "Cache-Control:no-cache, max-age=0" mv \
       gs://line-reports/{report_id}/index.html \
       gs://line-reports/{report_id}/snapshots/v{previous_version}.html
   ```
8. Upload new index.html:
   ```bash
   gsutil -h "Cache-Control:no-cache, max-age=0" cp \
       /workspace/report.html gs://line-reports/{report_id}/index.html
   ```
9. Verify it is publicly reachable:
   ```bash
   curl -sI https://storage.googleapis.com/line-reports/{report_id}/index.html | head -1
   ```
   If not `HTTP/2 200`, return `{"error": "publish_failed"}`.

## Output JSON returned in your message
```json
{
  "report_id": "<same as input>",
  "summary_500": "<500-char summary>",
  "top_citations": [
    {"title": "...", "url": "..."},
    {"title": "...", "url": "..."},
    {"title": "...", "url": "..."}
  ],
  "new_version": <int, 1 if mode=="new" else previous_version+1>
}
```

Return ONLY the JSON.

### If mode == "republish"
Skip rewriting. Run only steps 8 and 9 (upload + verify). Return:
```json
{"report_id": "<input>", "summary_500": "<reuse last>", "top_citations": [], "new_version": <unchanged>}
```
If verify fails, return `{"error": "publish_failed"}`.
