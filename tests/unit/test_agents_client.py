from unittest.mock import MagicMock

import pytest

from app.agents_client import AgentsAPIError, AgentsClient, EnvironmentNotFoundError


class _Interaction:
    """Stand-in for the SDK's Interaction model used in tests."""

    def __init__(self, *, id: str, status: str, environment_id: str | None = None,
                 output_text: str | None = None) -> None:
        self.id = id
        self.status = status
        self.environment_id = environment_id
        self.output_text = output_text


@pytest.fixture
def fake_genai(mocker):
    """Patches the SDK Client constructor so AgentsClient instantiation
    doesn't try to acquire real credentials. Returns the MagicMock that
    stands in for the SDK client; tests configure `interactions.create`
    and `interactions.get` on it.
    """
    sdk_client = MagicMock()
    mocker.patch("app.agents_client.genai.Client", return_value=sdk_client)
    return sdk_client


@pytest.fixture
def client(fake_genai) -> AgentsClient:
    return AgentsClient(
        project_id="p",
        location="global",
        agent_id="research-planner",
        poll_interval_seconds=0,  # don't sleep in tests
    )


def test_create_environment_returns_empty_sentinel(client: AgentsClient) -> None:
    # In the SDK model the real env is created on first interact;
    # AgentsClient.create_environment is now a no-op sentinel.
    assert client.create_environment() == ""


def test_interact_returns_parsed_response(client: AgentsClient, fake_genai) -> None:
    fake_genai.interactions.create.return_value = _Interaction(
        id="int-1", status="in_progress",
    )
    fake_genai.interactions.get.return_value = _Interaction(
        id="int-1", status="completed", environment_id="env-xyz",
        output_text='{"topic":"x","queries":[],"source_count":0}',
    )

    resp = client.interact(
        environment_id="env-xyz",
        instruction="PLAN\n\ntopic: x",
        previous_interaction_id=None,
    )
    assert resp.interaction_id == "int-1"
    assert resp.environment_id == "env-xyz"
    assert resp.text.startswith('{"topic"')


def test_interact_404_raises_environment_not_found(
    client: AgentsClient, fake_genai,
) -> None:
    fake_genai.interactions.create.side_effect = Exception("404 environment not found")
    with pytest.raises(EnvironmentNotFoundError):
        client.interact(
            environment_id="env-dead",
            instruction="PLAN",
            previous_interaction_id=None,
        )


def test_interact_failed_status_raises_api_error(
    client: AgentsClient, fake_genai,
) -> None:
    fake_genai.interactions.create.return_value = _Interaction(
        id="int-9", status="in_progress",
    )
    fake_genai.interactions.get.return_value = _Interaction(
        id="int-9", status="failed",
    )
    with pytest.raises(AgentsAPIError):
        client.interact(
            environment_id="env-x",
            instruction="PLAN",
            previous_interaction_id=None,
        )


def test_interact_chains_previous_interaction(
    client: AgentsClient, fake_genai,
) -> None:
    fake_genai.interactions.create.return_value = _Interaction(
        id="int-2", status="in_progress",
    )
    fake_genai.interactions.get.return_value = _Interaction(
        id="int-2", status="completed", environment_id="env-xyz", output_text="ok",
    )

    client.interact(
        environment_id="env-xyz",
        instruction="SEARCH_COMPARE",
        previous_interaction_id="int-1",
    )

    kwargs = fake_genai.interactions.create.call_args.kwargs
    assert kwargs["previous_interaction_id"] == "int-1"
    assert kwargs["environment"] == "env-xyz"
    assert kwargs["agent"] == "research-planner"
    assert kwargs["background"] is True


def test_interact_with_empty_env_requests_new_remote(
    client: AgentsClient, fake_genai,
) -> None:
    fake_genai.interactions.create.return_value = _Interaction(
        id="int-3", status="in_progress",
    )
    fake_genai.interactions.get.return_value = _Interaction(
        id="int-3", status="completed", environment_id="env-new", output_text="ok",
    )

    resp = client.interact(
        environment_id="",
        instruction="PLAN",
        previous_interaction_id=None,
    )

    kwargs = fake_genai.interactions.create.call_args.kwargs
    assert kwargs["environment"] == {"type": "remote"}
    assert resp.environment_id == "env-new"
