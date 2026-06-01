import json
import uuid
from dataclasses import dataclass
from typing import Literal

from app.agents_client import AgentsClient, AgentResponse, EnvironmentNotFoundError
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

        if job.mode == "new":
            self._run_new(user.line_user_id, user.environment_id, job.topic)
        elif job.mode == "deepen":
            self._run_deepen(user.line_user_id, user.environment_id, job.report_id, job.topic)
        else:
            raise NotImplementedError(f"mode {job.mode} not yet implemented")

    # ---------------- new research ----------------

    def _run_new(self, user_id: str, env_id: str, topic: str) -> None:
        # Stage 1: PLAN
        plan_resp = self._agents.interact(
            environment_id=env_id,
            instruction=f"PLAN\n\ntopic: {topic}",
            previous_interaction_id=None,
        )
        plan_json = json.loads(plan_resp.text)
        self._store.set_last_interaction(user_id, plan_resp.interaction_id)
        self._line.push_text(
            user_id=user_id,
            text=build_progress_text("plan_done", source_count=plan_json["source_count"]),
        )

        # Stage 2: SEARCH_COMPARE
        search_resp = self._agents.interact(
            environment_id=env_id,
            instruction="SEARCH_COMPARE",
            previous_interaction_id=plan_resp.interaction_id,
        )
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

        # Stage 3: WRITE_REPORT
        report_id = uuid.uuid4().hex
        write_resp = self._agents.interact(
            environment_id=env_id,
            instruction=(
                "WRITE_REPORT\n\n"
                f"report_id: {report_id}\n"
                "mode: new"
            ),
            previous_interaction_id=search_resp.interaction_id,
        )
        write_json = json.loads(write_resp.text)
        self._store.set_last_interaction(user_id, write_resp.interaction_id)

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

    def _public_url(self, report_id: str) -> str:
        return f"https://storage.googleapis.com/{self._bucket}/{report_id}/index.html"

    def _run_deepen(self, user_id: str, env_id: str, report_id: str | None, instruction: str) -> None:
        raise NotImplementedError("see Task 11")
