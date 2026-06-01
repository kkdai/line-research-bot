# Stage: SEARCH_COMPARE

## Your task
1. Read `/workspace/plan.json`.
2. For each query, use `google_search` then `url_context` on the top 1-2 results to read the actual page content.
3. For each source, record: title, url, language, 200-word factual summary, key claims (as a list).
4. Across all sources, identify:
   - **agreements** (claims supported by ≥ 2 sources)
   - **disagreements** (claims contradicted across sources)
   - **gaps** (questions the topic raises that no source answered)
5. Write the result to `/workspace/sources.json`.

## Output JSON written to `/workspace/sources.json` AND returned in your message
```json
{
  "sources": [
    {
      "title": "...",
      "url": "...",
      "lang": "en",
      "summary": "...",
      "key_claims": ["...", "..."]
    }
  ],
  "agreements": ["..."],
  "disagreements": [
    {"point": "...", "sides": [{"claim": "...", "source_urls": ["..."]}, ...]}
  ],
  "gaps": ["..."],
  "source_count": <int>,
  "disagreement_count": <int>
}
```

Return ONLY the JSON.
