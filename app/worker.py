import json
import uuid
from dataclasses import dataclass
from typing import Literal

from tenacity import retry, stop_after_attempt, wait_fixed, retry_if_exception_type

from app.agents_client import AgentsClient, AgentResponse, EnvironmentNotFoundError, AgentsAPIError
from app.flex import build_progress_text, build_report_card
from app.line_client import LineClient
from app.state import StateStore


Mode = Literal["new", "deepen", "retry_write", "retry_publish"]


@dataclass
class JobPayload:
    line_user_id: str
    topic: str
    mode: Mode
    report_id: str | None
    task_id: str


class ResearchWorker:
    def __init__(
        self,
        *,
        store: StateStore,
        agents: AgentsClient,
        line: LineClient,
        gcs_bucket: str,
    ) -> None:
        self._store = store
        self._agents = agents
        self._line = line
        self._bucket = gcs_bucket

    def run(self, job: JobPayload) -> None:
        user = self._store.get_user(job.line_user_id)
        assert user is not None, "user must exist by the time worker runs"

        try:
            if job.mode == "new":
                self._store._db.collection("users").document(user.line_user_id).update(
                    {"last_attempt_topic": job.topic}
                )
                self._run_new(user.line_user_id, user.environment_id, job.topic)
            elif job.mode == "deepen":
                self._run_deepen(
                    user.line_user_id,
                    user.environment_id,
                    job.report_id,
                    job.topic,
                )
            elif job.mode == "retry_write":
                self._run_retry_write(user.line_user_id, user.environment_id)
            elif job.mode == "retry_publish":
                self._run_retry_publish(user.line_user_id, user.environment_id)
            else:
                raise ValueError(f"unknown mode: {job.mode}")
        except EnvironmentNotFoundError:
            # Sandbox expired: recreate and (for deepen) fall back to NEW mode.
            new_env = self._agents.create_environment()
            self._store.set_environment(user.line_user_id, new_env)
            self._line.push_text(
                user_id=user.line_user_id,
                text="上次研究檔案已過期，重新從頭研究 🔄",
            )
            # In retry_publish there is no sources.json on a fresh sandbox either;
            # treat as new research using the original topic.
            topic = job.topic if job.mode == "new" else self._get_last_topic(user.line_user_id)
            self._run_new(user.line_user_id, new_env, topic)

    # ---------------- new research ----------------

    def _run_new(self, user_id: str, env_id: str, topic: str) -> None:
        # Stage 1: PLAN (1 attempt only; surface failure immediately)
        try:
            plan_resp = self._agents.interact(
                environment_id=env_id,
                instruction=f"PLAN\n\ntopic: {topic}",
                previous_interaction_id=None,
            )
        except AgentsAPIError:
            self._line.push_text(user_id=user_id, text="規劃失敗，請換個說法重試。")
            return
        plan_json = json.loads(plan_resp.text)
        self._store.set_last_interaction(user_id, plan_resp.interaction_id)
        self._line.push_text(
            user_id=user_id,
            text=build_progress_text("plan_done", source_count=plan_json["source_count"]),
        )

        # Stage 2: SEARCH_COMPARE (2 attempts max)
        try:
            search_resp = self._interact_with_retry(
                max_attempts=2,
                environment_id=env_id,
                instruction="SEARCH_COMPARE",
                previous_interaction_id=plan_resp.interaction_id,
            )
        except AgentsAPIError:
            self._line.push_text(user_id=user_id, text="搜尋比對失敗，請稍後再試。")
            return
        search_json = json.loads(search_resp.text)
        self._store.set_last_interaction(user_id, search_resp.interaction_id)
        self._line.push_text(
            user_id=user_id,
            text=build_progress_text(
                "search_done",
                source_count=search_json["source_count"],
                disagreement_count=search_json["disagreement_count"],
            ),
        )

        # Stage 3: WRITE_REPORT (2 attempts max; on final failure set pending_action and push)
        report_id = uuid.uuid4().hex
        try:
            write_resp = self._interact_with_retry(
                max_attempts=2,
                environment_id=env_id,
                instruction=(
                    "WRITE_REPORT\n\n"
                    f"report_id: {report_id}\n"
                    "mode: new"
                ),
                previous_interaction_id=search_resp.interaction_id,
            )
        except AgentsAPIError:
            self._store.set_pending_action(user_id, "retry_write")
            self._line.push_text(
                user_id=user_id,
                text="資料已找齊，組稿失敗，回『再試一次』可再試。",
            )
            return
        write_json = json.loads(write_resp.text)
        self._store.set_last_interaction(user_id, write_resp.interaction_id)

        if write_json.get("error") == "publish_failed":
            # Sandbox has report.html but couldn't upload. Seed state so user can retry.
            gcs_url = self._public_url(report_id)
            self._store.create_report(
                user_id=user_id, topic=topic,
                summary="(尚未發佈成功)", gcs_url=gcs_url, report_id=report_id,
            )
            self._store.set_current_report(user_id, report_id)
            self._store.set_pending_action(user_id, "retry_publish")
            self._line.push_text(
                user_id=user_id,
                text="報告寫好但發佈失敗，回『再發佈一次』可再試。",
            )
            return

        gcs_url = self._public_url(report_id)
        report = self._store.create_report(
            user_id=user_id,
            topic=topic,
            summary=write_json["summary_500"],
            gcs_url=gcs_url,
            report_id=report_id,
        )
        self._store.set_current_report(user_id, report.report_id)

        # Push final card
        card = build_report_card(
            topic=topic,
            summary=write_json["summary_500"],
            report_url=gcs_url,
            citations=write_json["top_citations"],
            version=1,
        )
        self._line.push_flex(user_id=user_id, flex_message=card)

    def _interact_with_retry(self, *, max_attempts: int, **kwargs) -> AgentResponse:
        @retry(
            stop=stop_after_attempt(max_attempts),
            wait=wait_fixed(2),
            retry=retry_if_exception_type(AgentsAPIError),
            reraise=True,
        )
        def _call() -> AgentResponse:
            return self._agents.interact(**kwargs)

        return _call()

    def _public_url(self, report_id: str) -> str:
        return f"https://storage.googleapis.com/{self._bucket}/{report_id}/index.html"

    # ---------------- helpers ----------------

    def _get_last_topic(self, user_id: str) -> str:
        snap = self._store._db.collection("users").document(user_id).get()
        return (snap.to_dict() or {}).get("last_attempt_topic", "")

    # ---------------- deepen ----------------

    def _run_deepen(
        self,
        user_id: str,
        env_id: str,
        report_id: str | None,
        deepen_request: str,
    ) -> None:
        assert report_id is not None, "deepen requires an existing report_id"
        existing = self._store.get_report(report_id)
        assert existing is not None
        previous_version = existing.version

        user = self._store.get_user(user_id)
        write_resp = self._agents.interact(
            environment_id=env_id,
            instruction=(
                "WRITE_REPORT\n\n"
                f"report_id: {report_id}\n"
                "mode: deepen\n"
                f"previous_version: {previous_version}\n"
                f"deepen_request: {deepen_request}\n"
            ),
            previous_interaction_id=user.last_interaction_id,
        )
        write_json = json.loads(write_resp.text)
        self._store.set_last_interaction(user_id, write_resp.interaction_id)

        if write_json.get("error") == "chapter_not_found":
            chapters = "\n".join(write_json.get("available_chapters", []))
            self._line.push_text(
                user_id=user_id,
                text=f"找不到指定章節。目前報告章節如下：\n{chapters}",
            )
            return

        snapshot_url = (
            f"https://storage.googleapis.com/{self._bucket}/"
            f"{report_id}/snapshots/v{previous_version}.html"
        )
        self._store.deepen_report(
            report_id,
            new_summary=write_json["summary_500"],
            snapshot_url=snapshot_url,
        )

        card = build_report_card(
            topic=existing.topic,
            summary=write_json["summary_500"],
            report_url=existing.gcs_url,
            citations=write_json["top_citations"],
            version=write_json["new_version"],
        )
        self._line.push_flex(user_id=user_id, flex_message=card)

    # ---------------- retry_write ----------------

    def _run_retry_write(self, user_id: str, env_id: str) -> None:
        user = self._store.get_user(user_id)
        topic = self._get_last_topic(user_id)
        report_id = uuid.uuid4().hex
        write_resp = self._agents.interact(
            environment_id=env_id,
            instruction=(
                "WRITE_REPORT\n\n"
                f"report_id: {report_id}\n"
                "mode: new\n"
            ),
            previous_interaction_id=user.last_interaction_id,
        )
        write_json = json.loads(write_resp.text)
        self._store.set_last_interaction(user_id, write_resp.interaction_id)
        self._store.set_pending_action(user_id, None)

        gcs_url = self._public_url(report_id)
        self._store.create_report(
            user_id=user_id,
            topic=topic,
            summary=write_json["summary_500"],
            gcs_url=gcs_url,
            report_id=report_id,
        )
        self._store.set_current_report(user_id, report_id)

        card = build_report_card(
            topic=topic,
            summary=write_json["summary_500"],
            report_url=gcs_url,
            citations=write_json["top_citations"],
            version=1,
        )
        self._line.push_flex(user_id=user_id, flex_message=card)

    # ---------------- retry_publish ----------------

    def _run_retry_publish(self, user_id: str, env_id: str) -> None:
        """Retry only the upload step. The sandbox already has report.html.
        We instruct the Agent to run only the publish commands and report success."""
        user = self._store.get_user(user_id)
        report = None
        if user.current_report_id:
            report = self._store.get_report(user.current_report_id)
        assert report is not None, "retry_publish requires an existing report"

        resp = self._agents.interact(
            environment_id=env_id,
            instruction=(
                "WRITE_REPORT\n\n"
                f"report_id: {report.report_id}\n"
                "mode: republish\n"
            ),
            previous_interaction_id=user.last_interaction_id,
        )
        data = json.loads(resp.text)
        if data.get("error") == "publish_failed":
            self._line.push_text(user_id=user_id, text="再次發佈失敗，請稍後重試。")
            return
        self._store.set_pending_action(user_id, None)
        self._line.push_text(
            user_id=user_id,
            text=f"✅ 已重新發佈：{report.gcs_url}",
        )
