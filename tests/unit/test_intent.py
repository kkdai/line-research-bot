from app.intent import classify, Intent, UserState


BASE_STATE = UserState(current_report_id=None, pending_action=None)
WITH_REPORT = UserState(current_report_id="r1", pending_action=None)
WITH_RETRY = UserState(current_report_id="r1", pending_action="retry_write")


def test_new_research_when_no_report() -> None:
    assert classify("研究 SOTA 開源向量資料庫", BASE_STATE) == Intent.NEW


def test_new_research_keyword() -> None:
    assert classify("幫我查 LLM 評測", WITH_REPORT) == Intent.NEW
    assert classify("找一下 RAG 框架比較", WITH_REPORT) == Intent.NEW


def test_deepen_keywords() -> None:
    for msg in ["深化第 2 章", "補日文來源", "改第三章", "第 2 章再深一點"]:
        assert classify(msg, WITH_REPORT) == Intent.DEEPEN


def test_reset() -> None:
    for msg in ["重新開始", "換題目", "清除"]:
        assert classify(msg, WITH_REPORT) == Intent.RESET


def test_recall() -> None:
    for msg in ["上次的", "我之前的研究", "連結"]:
        assert classify(msg, WITH_REPORT) == Intent.RECALL


def test_retry_only_when_pending_action() -> None:
    assert classify("再試一次", WITH_RETRY) == Intent.RETRY
    assert classify("再發佈一次", WITH_RETRY) == Intent.RETRY
    # No pending action → falls through to chitchat/new
    assert classify("再試一次", WITH_REPORT) != Intent.RETRY


def test_chitchat_short_greeting() -> None:
    assert classify("你好", WITH_REPORT) == Intent.CHITCHAT
    assert classify("嗨", WITH_REPORT) == Intent.CHITCHAT


def test_new_when_no_match_and_no_report() -> None:
    assert classify("Hello world", BASE_STATE) == Intent.NEW
