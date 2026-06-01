import json
from unittest.mock import MagicMock, call

import pytest
from google.cloud import firestore

from app.agents_client import AgentResponse, EnvironmentNotFoundError
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
