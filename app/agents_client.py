import time
from dataclasses import dataclass
from typing import Any, Callable

from google import genai


class AgentsAPIError(Exception):
    pass


class EnvironmentNotFoundError(AgentsAPIError):
    pass


@dataclass
class AgentResponse:
    interaction_id: str
    environment_id: str | None
    text: str
    raw: Any = None


class AgentsClient:
    """Wrapper over google-genai SDK for the Pre-GA Managed Agents API.

    The SDK requires `enterprise=True`; interactions must be created with
    `background=True` (synchronous mode is not supported). This wrapper
    issues the create, then polls `interactions.get` until the status reaches
    a terminal state.
    """

    def __init__(
        self,
        *,
        project_id: str,
        location: str,
        agent_id: str,
        access_token_provider: Callable[[], str] | None = None,
        timeout_seconds: float = 240.0,
        poll_interval_seconds: float = 2.0,
    ) -> None:
        # access_token_provider kept for backward compatibility with main.py wiring
        # but unused: the SDK reads ADC directly.
        del access_token_provider
        self._project = project_id
        self._location = location
        self._agent_id = agent_id
        self._timeout = timeout_seconds
        self._poll_interval = poll_interval_seconds
        self._client = genai.Client(
            enterprise=True, project=project_id, location=location
        )

    @property
    def agent_resource(self) -> str:
        return (
            f"projects/{self._project}/locations/{self._location}"
            f"/agents/{self._agent_id}"
        )

    def create_environment(self) -> str:
        """Returns an empty sentinel; the real environment is created on the
        first `interact()` call when `environment_id` is empty.

        Kept for backward compatibility with `StateStore.get_or_create_user`
        which expects an `environment_factory` callable.
        """
        return ""

    def interact(
        self,
        *,
        environment_id: str,
        instruction: str,
        previous_interaction_id: str | None,
        agent_resource: str | None = None,
    ) -> AgentResponse:
        del agent_resource  # SDK takes the agent id directly
        env_arg: Any = environment_id if environment_id else {"type": "remote"}

        kwargs: dict[str, Any] = {
            "agent": self._agent_id,
            "input": instruction,
            "environment": env_arg,
            "stream": False,
            "background": True,
            "store": True,
        }
        if previous_interaction_id:
            kwargs["previous_interaction_id"] = previous_interaction_id

        try:
            created = self._client.interactions.create(**kwargs)
        except Exception as e:
            msg = str(e).lower()
            if "not found" in msg or "404" in msg or "environment" in msg and "invalid" in msg:
                raise EnvironmentNotFoundError(str(e)) from e
            raise AgentsAPIError(str(e)) from e

        deadline = time.monotonic() + self._timeout
        last_status = created.status
        while time.monotonic() < deadline:
            try:
                polled = self._client.interactions.get(created.id, include_input=False)
            except Exception as e:
                msg = str(e).lower()
                if "not found" in msg or "404" in msg:
                    raise EnvironmentNotFoundError(str(e)) from e
                raise AgentsAPIError(str(e)) from e

            last_status = polled.status
            if polled.status == "completed":
                return AgentResponse(
                    interaction_id=polled.id,
                    environment_id=polled.environment_id,
                    text=polled.output_text or "",
                    raw=polled,
                )
            if polled.status in ("failed", "errored", "cancelled"):
                raise AgentsAPIError(f"interaction {polled.status}: {polled.id}")
            time.sleep(self._poll_interval)

        raise AgentsAPIError(
            f"interaction timed out after {self._timeout}s (last status: {last_status})"
        )
