# Stage: PLAN

You will receive a research topic in 繁體中文.

## Your task
1. Decide 4-8 specific Google search queries that together cover the topic from multiple angles (official docs, benchmarks, community reviews, recent news, opposing viewpoints).
2. For each query, name the source category (e.g. "official_docs", "benchmark", "community_review", "news", "academic").
3. Decide languages to include (default: zh-TW and en; add others only if the topic clearly requires).
4. Write the plan to `/workspace/plan.json` using `code_execution`.

## Output JSON written to `/workspace/plan.json` AND returned in your message
```json
{
  "topic": "<original topic>",
  "queries": [
    {"q": "...", "source_category": "official_docs", "lang": "en"},
    ...
  ],
  "source_count": <int, equal to len(queries)>
}
```

Return ONLY the JSON. No prose, no markdown fences.
