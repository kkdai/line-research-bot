from app.flex import build_report_card, build_progress_text


def test_progress_text_planning() -> None:
    assert build_progress_text("plan_done", source_count=7) == (
        "🔍 計畫完成：將比對 7 類來源"
    )


def test_progress_text_search_done() -> None:
    text = build_progress_text("search_done", source_count=7, disagreement_count=3)
    assert "已比對 7 份來源" in text
    assert "3" in text


def test_progress_text_starting() -> None:
    assert build_progress_text("planning") == "📋 已收到題目，開始規劃…"


def test_report_card_has_expected_shape() -> None:
    card = build_report_card(
        topic="向量資料庫選型",
        summary="一段 500 字的摘要…",
        report_url="https://storage.googleapis.com/x/r1/index.html",
        citations=[
            {"title": "Milvus 官方文件", "url": "https://milvus.io"},
            {"title": "Pinecone benchmark", "url": "https://pinecone.io/blog/b"},
        ],
        version=1,
    )
    assert card["type"] == "flex"
    assert card["altText"].startswith("📝")
    contents = card["contents"]
    assert contents["type"] == "bubble"
    # Footer button URL must match
    footer_buttons = contents["footer"]["contents"]
    assert footer_buttons[0]["action"]["uri"] == "https://storage.googleapis.com/x/r1/index.html"


def test_report_card_v2_shows_version_badge() -> None:
    card = build_report_card(
        topic="x", summary="y",
        report_url="https://example.com/r/index.html",
        citations=[],
        version=2,
    )
    # find "v2" string anywhere in the bubble
    import json
    assert "v2" in json.dumps(card, ensure_ascii=False)
