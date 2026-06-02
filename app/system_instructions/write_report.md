# Stage: WRITE_REPORT

You will receive metadata in the input:
- `report_id`: target identifier (echo it back in the output)
- `mode`: "new" | "deepen" | "republish"
- `previous_version`: integer, present only when mode is "deepen"
- `deepen_request`: free-text user instruction, present only when mode is "deepen"
  (e.g. "第 3 章再深一點，加日文來源")

## Your task

### If mode == "new"
1. Read `/workspace/sources.json` using `code_execution`.
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

### If mode == "republish"
Just re-read `/workspace/report.md` (do not modify) and return it.

## Output

After writing/updating `/workspace/report.md`, use `code_execution` to read its
full contents back, then return a SINGLE JSON object:

```json
{
  "report_id": "<echo back the input report_id>",
  "report_md": "<the FULL contents of /workspace/report.md as a single string>",
  "summary_500": "<a 500-character summary suitable for messaging>",
  "top_citations": [
    {"title": "...", "url": "..."},
    {"title": "...", "url": "..."},
    {"title": "...", "url": "..."}
  ],
  "new_version": <int, 1 if mode=="new" else previous_version+1>
}
```

**DO NOT** attempt to upload to GCS. **DO NOT** run gsutil. **DO NOT** run curl
against `storage.googleapis.com`. The host service handles rendering and
publishing. Your only job is to write the Markdown into the sandbox and echo
its contents back in the JSON.

Return ONLY the JSON. No prose, no markdown fences.
