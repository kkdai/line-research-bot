import json
from unittest.mock import MagicMock
import pytest
from fastapi.testclient import TestClient
from google.cloud import firestore

from app.webhook import build_webhook_router
from app.state import StateStore


def _sign(secret: str, body: bytes) -> str:
    import base64, hashlib, hmac
    return base64.b64encode(
        hmac.new(secret.encode(), body, hashlib.sha256).digest()
    ).decode()


@pytest.fixture
def app_and_mocks(firestore_client: firestore.Client):
    from fastapi import FastAPI
    store = StateStore(firestore_client)
    line = MagicMock()
    line.verify_signature.side_effect = lambda body, sig: sig == _sign("secret", body)
    tasks = MagicMock()
    agents = MagicMock()
    agents.create_environment.return_value = "env-A"

    router = build_webhook_router(
        store=store, line=line, tasks=tasks, agents=agents,
    )
    app = FastAPI()
    app.include_router(router)
    return TestClient(app), store, line, tasks, agents


def test_webhook_bad_signature_returns_401(app_and_mocks) -> None:
    client, *_ = app_and_mocks
    r = client.post("/webhook",
                    json={"events": []},
                    headers={"X-Line-Signature": "bad"})
    assert r.status_code == 401


def test_webhook_valid_signature_acks_immediately(app_and_mocks) -> None:
    client, store, line, tasks, agents = app_and_mocks
    body = json.dumps({"events": [{
        "type": "message",
        "replyToken": "rep1",
        "source": {"type": "user", "userId": "U1"},
        "message": {"type": "text", "text": "研究 SOTA 向量資料庫"},
    }]}).encode()

    r = client.post("/webhook", content=body,
                    headers={"X-Line-Signature": _sign("secret", body),
                             "Content-Type": "application/json"})
    assert r.status_code == 200
    line.reply_text.assert_called_once()
    assert "已收到題目" in line.reply_text.call_args.kwargs["text"]
    tasks.enqueue.assert_called_once()


def test_webhook_non_text_message_replies_only(app_and_mocks) -> None:
    client, store, line, tasks, agents = app_and_mocks
    body = json.dumps({"events": [{
        "type": "message",
        "replyToken": "rep1",
        "source": {"type": "user", "userId": "U1"},
        "message": {"type": "sticker", "stickerId": "1"},
    }]}).encode()

    r = client.post("/webhook", content=body,
                    headers={"X-Line-Signature": _sign("secret", body),
                             "Content-Type": "application/json"})
    assert r.status_code == 200
    line.reply_text.assert_called_once()
    assert "只支援文字" in line.reply_text.call_args.kwargs["text"]
    tasks.enqueue.assert_not_called()


def test_webhook_deepen_routes_to_existing_report(app_and_mocks) -> None:
    client, store, line, tasks, agents = app_and_mocks
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.create_report(user_id="U1", topic="x", summary="s",
                        gcs_url="g", report_id="r1")
    store.set_current_report("U1", "r1")

    body = json.dumps({"events": [{
        "type": "message",
        "replyToken": "rep1",
        "source": {"type": "user", "userId": "U1"},
        "message": {"type": "text", "text": "第 2 章再深一點"},
    }]}).encode()

    r = client.post("/webhook", content=body,
                    headers={"X-Line-Signature": _sign("secret", body),
                             "Content-Type": "application/json"})
    assert r.status_code == 200
    job = tasks.enqueue.call_args.args[0]
    assert job.mode == "deepen"
    assert job.report_id == "r1"


def test_webhook_locked_user_replies_busy(app_and_mocks) -> None:
    import datetime as dt
    client, store, line, tasks, agents = app_and_mocks
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.acquire_lock("U1", task_id="t-prev", ttl_seconds=300)

    body = json.dumps({"events": [{
        "type": "message",
        "replyToken": "rep1",
        "source": {"type": "user", "userId": "U1"},
        "message": {"type": "text", "text": "新題目"},
    }]}).encode()

    r = client.post("/webhook", content=body,
                    headers={"X-Line-Signature": _sign("secret", body),
                             "Content-Type": "application/json"})
    assert r.status_code == 200
    msg = line.reply_text.call_args.kwargs["text"]
    assert "還在跑" in msg
    tasks.enqueue.assert_not_called()
