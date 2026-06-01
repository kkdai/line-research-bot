import os
import pytest
from app.config import Settings


def test_settings_reads_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("USE_FIRESTORE_EMULATOR", raising=False)
    monkeypatch.setenv("GCP_PROJECT_ID", "demo-proj")
    monkeypatch.setenv("AGENT_ID", "research-planner")
    monkeypatch.setenv("GCS_BUCKET", "line-reports")
    monkeypatch.setenv("CLOUD_TASKS_QUEUE", "research-jobs")
    monkeypatch.setenv("CLOUD_TASKS_LOCATION", "asia-east1")
    monkeypatch.setenv("CLOUD_RUN_SERVICE_URL", "https://x.run.app")
    monkeypatch.setenv("LINE_CHANNEL_SECRET", "secret123")
    monkeypatch.setenv("LINE_CHANNEL_ACCESS_TOKEN", "token123")

    s = Settings()

    assert s.gcp_project_id == "demo-proj"
    assert s.gcp_location == "global"  # default
    assert s.agent_id == "research-planner"
    assert s.gcs_bucket == "line-reports"
    assert s.use_firestore_emulator is False  # default


def test_settings_missing_required_raises(monkeypatch: pytest.MonkeyPatch) -> None:
    for k in [
        "GCP_PROJECT_ID", "AGENT_ID", "GCS_BUCKET", "CLOUD_TASKS_QUEUE",
        "CLOUD_TASKS_LOCATION", "CLOUD_RUN_SERVICE_URL",
        "LINE_CHANNEL_SECRET", "LINE_CHANNEL_ACCESS_TOKEN",
    ]:
        monkeypatch.delenv(k, raising=False)
    with pytest.raises(ValueError):
        Settings()
