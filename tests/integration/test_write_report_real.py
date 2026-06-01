"""Integration test for full WRITE_REPORT cycle hitting the real Agents API."""
import json
import os
import uuid
import pytest
import httpx
from tests.integration.test_filesystem_persistence import client


@pytest.mark.skipif(
    not os.environ.get("RUN_FULL_WRITE_TEST"),
    reason="Set RUN_FULL_WRITE_TEST=1 (this test takes ~2 minutes and writes to GCS)",
)
def test_full_three_stage_writes_publicly_reachable_html(client) -> None:
    env_id = client.create_environment()
    report_id = uuid.uuid4().hex

    r1 = client.interact(
        environment_id=env_id,
        instruction="PLAN\n\ntopic: 開源向量資料庫選型 (測試)",
        previous_interaction_id=None,
    )
    r2 = client.interact(
        environment_id=env_id,
        instruction="SEARCH_COMPARE",
        previous_interaction_id=r1.interaction_id,
    )
    r3 = client.interact(
        environment_id=env_id,
        instruction=(
            "WRITE_REPORT\n\n"
            f"report_id: {report_id}\n"
            "mode: new"
        ),
        previous_interaction_id=r2.interaction_id,
    )
    out = json.loads(r3.text)
    assert out["report_id"] == report_id
    assert out["new_version"] == 1

    url = f"https://storage.googleapis.com/{os.environ['GCS_BUCKET']}/{report_id}/index.html"
    head = httpx.head(url, timeout=10.0)
    assert head.status_code == 200
    body = httpx.get(url, timeout=10.0).text
    assert "<html" in body.lower()
