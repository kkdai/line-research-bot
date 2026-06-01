from typing import Literal


ProgressStage = Literal["planning", "plan_done", "search_done", "writing"]


def build_progress_text(
    stage: ProgressStage,
    *,
    source_count: int = 0,
    disagreement_count: int = 0,
) -> str:
    if stage == "planning":
        return "📋 已收到題目，開始規劃…"
    if stage == "plan_done":
        return f"🔍 計畫完成：將比對 {source_count} 類來源"
    if stage == "search_done":
        return (
            f"📊 已比對 {source_count} 份來源，"
            f"發現 {disagreement_count} 處重要分歧"
        )
    if stage == "writing":
        return "📝 正在撰寫報告…"
    raise ValueError(f"unknown stage: {stage}")


def build_report_card(
    *,
    topic: str,
    summary: str,
    report_url: str,
    citations: list[dict],
    version: int,
) -> dict:
    summary_short = summary if len(summary) <= 400 else summary[:400] + "…"
    badge = f"v{version}"

    citation_blocks = []
    for c in citations[:3]:
        citation_blocks.append({
            "type": "text",
            "text": f"• {c['title']}",
            "wrap": True,
            "size": "xs",
            "color": "#666666",
            "action": {"type": "uri", "uri": c["url"]},
        })

    body_contents = [
        {
            "type": "box",
            "layout": "horizontal",
            "contents": [
                {"type": "text", "text": topic, "weight": "bold", "size": "lg",
                 "wrap": True, "flex": 5},
                {"type": "text", "text": badge, "size": "sm", "color": "#888888",
                 "align": "end", "flex": 1},
            ],
        },
        {"type": "separator", "margin": "md"},
        {"type": "text", "text": summary_short, "wrap": True, "size": "sm",
         "margin": "md"},
    ]
    if citation_blocks:
        body_contents.append({"type": "separator", "margin": "md"})
        body_contents.append({"type": "text", "text": "主要來源",
                              "size": "xs", "color": "#888888", "margin": "md"})
        body_contents.extend(citation_blocks)

    return {
        "type": "flex",
        "altText": f"📝 {topic} 研究報告已完成",
        "contents": {
            "type": "bubble",
            "body": {
                "type": "box",
                "layout": "vertical",
                "contents": body_contents,
            },
            "footer": {
                "type": "box",
                "layout": "vertical",
                "contents": [
                    {
                        "type": "button",
                        "style": "primary",
                        "action": {
                            "type": "uri",
                            "label": "閱讀完整報告",
                            "uri": report_url,
                        },
                    }
                ],
            },
        },
    }
