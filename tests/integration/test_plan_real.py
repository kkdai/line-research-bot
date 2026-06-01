"""Integration test for PLAN command hitting the real Agents API."""
import json
import os
import pytest
from tests.integration.test_filesystem_persistence import client


def test_plan_returns_valid_json(client) -> None:
    env_id = client.create_environment()
    resp = client.interact(
        environment_id=env_id,
        instruction="PLAN\n\ntopic: SOTA 開源向量資料庫的選型",
        previous_interaction_id=None,
    )
    data = json.loads(resp.text)
    assert "queries" in data
    assert isinstance(data["queries"], list)
    assert 4 <= len(data["queries"]) <= 8
    assert data["source_count"] == len(data["queries"])
    for q in data["queries"]:
        assert "q" in q and "source_category" in q and "lang" in q
