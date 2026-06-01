import json
import pytest
import respx
import httpx
from app.agents_client import AgentsClient, AgentResponse, EnvironmentNotFoundError


@pytest.fixture
def client() -> AgentsClient:
    return AgentsClient(
        project_id="p",
        location="global",
        agent_id="research-planner",
        access_token_provider=lambda: "fake-token",
    )


@respx.mock
def test_create_environment_returns_id(client: AgentsClient) -> None:
    respx.post(
        "https://aiplatform.googleapis.com/v1beta1/projects/p/locations/global/environments"
    ).mock(return_value=httpx.Response(200, json={"name": "projects/p/locations/global/environments/env-xyz", "id": "env-xyz"}))

    env_id = client.create_environment()
    assert env_id == "env-xyz"


@respx.mock
def test_interact_returns_parsed_response(client: AgentsClient) -> None:
    respx.post(
        "https://aiplatform.googleapis.com/v1beta1/projects/p/locations/global/interactions"
    ).mock(return_value=httpx.Response(200, json={
        "id": "int-1",
        "environment_id": "env-xyz",
        "output": [{"type": "agent_response", "content": [
            {"type": "text", "text": '{"topic":"x","queries":[],"source_count":0}'}
        ]}],
    }))

    resp = client.interact(
        environment_id="env-xyz",
        instruction="PLAN\n\ntopic: x",
        previous_interaction_id=None,
        agent_resource="projects/p/locations/global/agents/research-planner",
    )
    assert resp.interaction_id == "int-1"
    assert resp.environment_id == "env-xyz"
    assert json.loads(resp.text)["topic"] == "x"


@respx.mock
def test_interact_404_raises_environment_not_found(client: AgentsClient) -> None:
    respx.post(
        "https://aiplatform.googleapis.com/v1beta1/projects/p/locations/global/interactions"
    ).mock(return_value=httpx.Response(404, json={"error": {"message": "environment not found"}}))

    with pytest.raises(EnvironmentNotFoundError):
        client.interact(
            environment_id="env-dead",
            instruction="PLAN",
            previous_interaction_id=None,
            agent_resource="projects/p/locations/global/agents/research-planner",
        )


@respx.mock
def test_interact_chains_previous_interaction(client: AgentsClient) -> None:
    captured = {}
    def handler(request: httpx.Request) -> httpx.Response:
        captured["body"] = json.loads(request.content)
        return httpx.Response(200, json={
            "id": "int-2",
            "environment_id": "env-xyz",
            "output": [{"type": "agent_response", "content": [{"type": "text", "text": "ok"}]}],
        })
    respx.post(
        "https://aiplatform.googleapis.com/v1beta1/projects/p/locations/global/interactions"
    ).mock(side_effect=handler)

    client.interact(
        environment_id="env-xyz",
        instruction="SEARCH_COMPARE",
        previous_interaction_id="int-1",
        agent_resource="projects/p/locations/global/agents/research-planner",
    )
    assert captured["body"]["previous_interaction_id"] == "int-1"
    assert captured["body"]["environment"] == "env-xyz"
