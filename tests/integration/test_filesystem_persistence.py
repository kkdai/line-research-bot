"""Hits the real Agents API. Requires gcloud ADC and GCP_PROJECT_ID env."""
import os
import pytest
from app.agents_client import AgentsClient

import google.auth
import google.auth.transport.requests


@pytest.fixture(scope="session")
def client() -> AgentsClient:
    project = os.environ["GCP_PROJECT_ID"]
    creds, _ = google.auth.default(
        scopes=["https://www.googleapis.com/auth/cloud-platform"]
    )
    auth_req = google.auth.transport.requests.Request()

    def _token() -> str:
        if not creds.valid:
            creds.refresh(auth_req)
        return creds.token

    return AgentsClient(
        project_id=project, location="global",
        agent_id=os.environ.get("AGENT_ID", "research-planner"),
        access_token_provider=_token,
    )


def test_filesystem_persists_across_interactions(client: AgentsClient) -> None:
    env_id = client.create_environment()
    r1 = client.interact(
        environment_id=env_id,
        instruction=(
            "Use code_execution to write the string 'hello-2026' to "
            "/workspace/marker.txt. Reply with the literal string 'OK'."
        ),
        previous_interaction_id=None,
    )
    assert "OK" in r1.text

    r2 = client.interact(
        environment_id=env_id,
        instruction=(
            "Use code_execution to cat /workspace/marker.txt and reply with "
            "exactly the file contents."
        ),
        previous_interaction_id=r1.interaction_id,
    )
    assert "hello-2026" in r2.text
