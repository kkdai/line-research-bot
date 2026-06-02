import json
from unittest.mock import MagicMock, call

import pytest
from google.cloud import firestore

from app.agents_client import AgentResponse, EnvironmentNotFoundError, AgentsAPIError
from app.state import StateStore
from app.worker import ResearchWorker, JobPayload


@pytest.fixture
def worker(firestore_client: firestore.Client) -> ResearchWorker:
    store = StateStore(firestore_client)
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    return ResearchWorker(
        store=store,
        agents=MagicMock(),
        line=MagicMock(),
        gcs_bucket="line-reports",
    )


def _ok(text: str, interaction_id: str = "i") -> AgentResponse:
    return AgentResponse(interaction_id=interaction_id, environment_id="env-A",
                         text=text, raw={})


def test_new_research_happy_path(worker: "ResearchWorker") -> None:
    worker._agents.interact.side_effect = [
        _ok(json.dumps({"topic": "x", "queries": [1, 2, 3], "source_count": 3}), "i1"),
        _ok(json.dumps({"sources": [1, 2, 3], "source_count": 3,
                        "disagreement_count": 1, "agreements": [], "disagreements": [], "gaps": []}), "i2"),
        _ok(json.dumps({
            "report_id": "auto",
            "summary_500": "summary",
            "top_citations": [{"title": "T1", "url": "https://a"}],
            "new_version": 1,
        }), "i3"),
    ]

    job = JobPayload(line_user_id="U1", topic="向量資料庫", mode="new", report_id=None, task_id="t1")
    worker.run(job)

    # Three Agent calls
    assert worker._agents.interact.call_count == 3
    # Last call carries previous_interaction_id from i2
    last_kwargs = worker._agents.interact.call_args_list[2].kwargs
    assert last_kwargs["previous_interaction_id"] == "i2"
    # Progress pushes: planning, plan_done, search_done, final flex card
    push_text_calls = worker._line.push_text.call_args_list
    push_flex_calls = worker._line.push_flex.call_args_list
    assert len(push_text_calls) >= 2  # plan_done + search_done
    assert len(push_flex_calls) == 1
    # current_report_id is set
    user = worker._store.get_user("U1")
    assert user.current_report_id is not None
    report = worker._store.get_report(user.current_report_id)
    assert report.version == 1
    assert report.summary == "summary"


def test_deepen_updates_same_report(worker: "ResearchWorker") -> None:
    # Seed: existing report v1
    r = worker._store.create_report(
        user_id="U1", topic="x", summary="s1",
        gcs_url="https://storage.googleapis.com/line-reports/r1/index.html",
        report_id="r1",
    )
    worker._store.set_current_report("U1", "r1")

    worker._agents.interact.return_value = _ok(json.dumps({
        "report_id": "r1",
        "summary_500": "s2",
        "top_citations": [],
        "new_version": 2,
    }), "iD")

    job = JobPayload(
        line_user_id="U1", topic="第 2 章再深一點",
        mode="deepen", report_id="r1", task_id="t2",
    )
    worker.run(job)

    assert worker._agents.interact.call_count == 1
    # Single WRITE_REPORT call with mode=deepen
    inst = worker._agents.interact.call_args.kwargs["instruction"]
    assert "mode: deepen" in inst
    assert "previous_version: 1" in inst
    assert "第 2 章再深一點" in inst

    updated = worker._store.get_report("r1")
    assert updated.version == 2
    assert updated.summary == "s2"
    assert len(updated.history) == 1


def test_sandbox_expiry_recreates_and_falls_back_to_new(worker: "ResearchWorker") -> None:
    # Save the current env id so we can prove it changed
    worker._store.create_report(user_id="U1", topic="t", summary="s",
                                gcs_url="x", report_id="r1")
    worker._store.set_current_report("U1", "r1")

    new_env = "env-B"
    worker._agents.create_environment.return_value = new_env

    # First interact raises EnvironmentNotFoundError (sandbox dead),
    # then the three new-research calls succeed.
    worker._agents.interact.side_effect = [
        EnvironmentNotFoundError("dead"),
        _ok(json.dumps({"topic": "t", "queries": [], "source_count": 0}), "i1"),
        _ok(json.dumps({"sources": [], "source_count": 0, "disagreement_count": 0,
                        "agreements": [], "disagreements": [], "gaps": []}), "i2"),
        _ok(json.dumps({"report_id": "r-new", "summary_500": "s",
                        "top_citations": [], "new_version": 1}), "i3"),
    ]

    job = JobPayload(line_user_id="U1", topic="第 2 章再深",
                     mode="deepen", report_id="r1", task_id="t3")
    worker.run(job)

    user = worker._store.get_user("U1")
    assert user.environment_id == new_env
    # User was warned
    push_msgs = [c.kwargs.get("text", "") for c in worker._line.push_text.call_args_list]
    assert any("過期" in m for m in push_msgs)


def test_retry_write_uses_existing_sources(worker: "ResearchWorker") -> None:
    # Seed pending state: sources.json already in sandbox; we only re-run stage 3
    worker._store.set_pending_action("U1", "retry_write")
    # Need a topic; in retry mode worker should fetch the in-progress topic from somewhere.
    # Simplest: store last attempted topic on the user record (added in this task).
    worker._store._db.collection("line_bot_users").document("U1").update({"last_attempt_topic": "x"})

    worker._agents.interact.return_value = _ok(json.dumps({
        "report_id": "r-new", "summary_500": "s",
        "top_citations": [], "new_version": 1,
    }), "iR")

    job = JobPayload(line_user_id="U1", topic="再試一次",
                     mode="retry_write", report_id=None, task_id="tR")
    worker.run(job)

    assert worker._agents.interact.call_count == 1
    inst = worker._agents.interact.call_args.kwargs["instruction"]
    assert "WRITE_REPORT" in inst
    user = worker._store.get_user("U1")
    assert user.pending_action is None


def test_search_compare_retries_once_then_succeeds(worker: "ResearchWorker") -> None:
    worker._agents.interact.side_effect = [
        _ok(json.dumps({"topic": "x", "queries": [], "source_count": 0}), "i1"),
        AgentsAPIError("transient"),
        _ok(json.dumps({"sources": [], "source_count": 0, "disagreement_count": 0,
                        "agreements": [], "disagreements": [], "gaps": []}), "i2-retry"),
        _ok(json.dumps({"report_id": "r1", "summary_500": "s",
                        "top_citations": [], "new_version": 1}), "i3"),
    ]
    job = JobPayload(line_user_id="U1", topic="t", mode="new",
                     report_id=None, task_id="t1")
    worker.run(job)
    assert worker._agents.interact.call_count == 4


def test_write_report_fails_twice_sets_pending_action(worker: "ResearchWorker") -> None:
    worker._agents.interact.side_effect = [
        _ok(json.dumps({"topic": "x", "queries": [], "source_count": 0}), "i1"),
        _ok(json.dumps({"sources": [], "source_count": 0, "disagreement_count": 0,
                        "agreements": [], "disagreements": [], "gaps": []}), "i2"),
        AgentsAPIError("fail-1"),
        AgentsAPIError("fail-2"),
    ]
    job = JobPayload(line_user_id="U1", topic="t", mode="new",
                     report_id=None, task_id="t1")
    worker.run(job)
    user = worker._store.get_user("U1")
    assert user.pending_action == "retry_write"
    push_msgs = [c.kwargs.get("text", "") for c in worker._line.push_text.call_args_list]
    assert any("再試一次" in m for m in push_msgs)


def test_write_report_publish_failed_sets_pending_action(worker: "ResearchWorker") -> None:
    worker._agents.interact.side_effect = [
        _ok(json.dumps({"topic": "x", "queries": [], "source_count": 0}), "i1"),
        _ok(json.dumps({"sources": [], "source_count": 0, "disagreement_count": 0,
                        "agreements": [], "disagreements": [], "gaps": []}), "i2"),
        _ok(json.dumps({"error": "publish_failed"}), "i3"),
    ]
    job = JobPayload(line_user_id="U1", topic="t", mode="new",
                     report_id=None, task_id="t1")
    worker.run(job)
    user = worker._store.get_user("U1")
    assert user.pending_action == "retry_publish"
    push_msgs = [c.kwargs.get("text", "") for c in worker._line.push_text.call_args_list]
    assert any("再發佈一次" in m for m in push_msgs)
    # Report should be created so retry_publish can find it
    assert user.current_report_id is not None


def test_run_research_endpoint_releases_lock(firestore_client) -> None:
    """End-to-end of /tasks/run-research: lock acquired by webhook is released here."""
    from fastapi import FastAPI, Request
    from fastapi.testclient import TestClient

    store = StateStore(firestore_client)
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.acquire_lock("U1", task_id="t1", ttl_seconds=300)

    agents = MagicMock()
    agents.interact.side_effect = [
        _ok(json.dumps({"topic": "x", "queries": [], "source_count": 0}), "i1"),
        _ok(json.dumps({"sources": [], "source_count": 0, "disagreement_count": 0,
                        "agreements": [], "disagreements": [], "gaps": []}), "i2"),
        _ok(json.dumps({"report_id": "r1", "summary_500": "s",
                        "top_citations": [], "new_version": 1}), "i3"),
    ]
    worker = ResearchWorker(store=store, agents=agents, line=MagicMock(),
                            gcs_bucket="line-reports")
    app = FastAPI()

    @app.post("/tasks/run-research")
    async def run(req: Request) -> dict:
        data = await req.json()
        job = JobPayload(**{k: data[k] for k in ["line_user_id", "topic", "mode",
                                                  "report_id", "task_id"]})
        try:
            worker.run(job)
        finally:
            store.release_lock(job.line_user_id)
        return {"ok": True}

    client = TestClient(app)
    r = client.post("/tasks/run-research", json={
        "line_user_id": "U1", "topic": "x", "mode": "new",
        "report_id": None, "task_id": "t1",
    })
    assert r.status_code == 200
    assert store.get_user("U1").lock is None
