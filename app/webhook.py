import json
import logging
import uuid
from fastapi import APIRouter, Request, HTTPException

from app.agents_client import AgentsClient
from app.intent import Intent, UserState, classify
from app.line_client import LineClient
from app.state import StateStore, LockTimeoutError
from app.tasks_client import TasksDispatcher, ResearchJob

logger = logging.getLogger(__name__)


def build_webhook_router(
    *,
    store: StateStore,
    line: LineClient,
    tasks: TasksDispatcher,
    agents: AgentsClient,
) -> APIRouter:
    router = APIRouter()

    @router.post("/webhook")
    async def webhook(request: Request) -> dict:
        body = await request.body()
        signature = request.headers.get("X-Line-Signature", "")
        if not line.verify_signature(body, signature):
            raise HTTPException(status_code=401, detail="bad signature")

        payload = json.loads(body)
        for event in payload.get("events", []):
            try:
                _handle_event(event, store=store, line=line, tasks=tasks, agents=agents)
            except Exception:
                logger.exception("event handling failed: %s", event.get("type"))
                # swallow so we still return 200; LINE won't redeliver this batch
        return {"ok": True}

    return router


def _handle_event(
    event: dict,
    *,
    store: StateStore,
    line: LineClient,
    tasks: TasksDispatcher,
    agents: AgentsClient,
) -> None:
    if event.get("type") != "message":
        return
    msg = event["message"]
    reply_token = event["replyToken"]
    user_id = event["source"]["userId"]

    if msg.get("type") != "text":
        line.reply_text(reply_token=reply_token,
                        text="目前只支援文字題目，請用文字描述要研究的主題。")
        return

    text = msg["text"]

    # Ensure user exists; create environment lazily on first contact.
    user = store.get_or_create_user(
        user_id, environment_factory=lambda: agents.create_environment()
    )

    intent = classify(
        text, UserState(current_report_id=user.current_report_id,
                        pending_action=user.pending_action),
    )

    if intent == Intent.CHITCHAT:
        line.reply_text(reply_token=reply_token,
                        text="哈囉！傳一個研究主題給我吧，例如「研究 SOTA 開源向量資料庫」。")
        return

    if intent == Intent.RECALL:
        if user.current_report_id:
            report = store.get_report(user.current_report_id)
            line.reply_text(reply_token=reply_token,
                            text=f"上次的報告連結：\n{report.gcs_url}")
        else:
            line.reply_text(reply_token=reply_token,
                            text="目前沒有進行中的研究。")
        return

    if intent == Intent.RESET:
        store.set_current_report(user_id, None)
        store.set_last_interaction(user_id, "")
        store.set_pending_action(user_id, None)
        line.reply_text(reply_token=reply_token,
                        text="✅ 已清空目前研究。請傳新題目。")
        return

    # NEW / DEEPEN / RETRY → all enqueue a background task. Acquire lock first.
    task_id = f"t-{uuid.uuid4().hex[:12]}"
    try:
        store.acquire_lock(user_id, task_id=task_id, ttl_seconds=300)
    except LockTimeoutError:
        line.reply_text(reply_token=reply_token,
                        text="上一份研究還在跑，請稍候幾分鐘後再傳新題目。")
        return

    mode_map = {
        Intent.NEW: "new",
        Intent.DEEPEN: "deepen",
        Intent.RETRY: user.pending_action or "retry_write",  # retry_write|retry_publish
    }
    mode = mode_map[intent]
    report_id = user.current_report_id if intent == Intent.DEEPEN else None

    job = ResearchJob(
        line_user_id=user_id, topic=text, mode=mode, report_id=report_id,
    )
    try:
        tasks.enqueue(job, task_id=task_id)
    except Exception:
        # Roll back the lock so the user can retry; surface the error to LINE.
        store.release_lock(user_id)
        logger.exception("tasks.enqueue failed")
        line.reply_text(reply_token=reply_token,
                        text="排程失敗，請稍後再試。")
        return

    line.reply_text(reply_token=reply_token, text="📋 已收到題目，開始規劃…")
