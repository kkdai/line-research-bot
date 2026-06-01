from dataclasses import dataclass
from typing import Callable

import httpx


class AgentsAPIError(Exception):
    pass


class EnvironmentNotFoundError(AgentsAPIError):
    pass


@dataclass
class AgentResponse:
    interaction_id: str
    environment_id: str
    text: str
    raw: dict


class AgentsClient:
    """Thin REST wrapper for the Managed Agents API (Pre-GA, v1beta1).

    Args:
        access_token_provider: callable returning a valid OAuth2 access token.
            In Cloud Run, use google.auth's default credentials and refresh.
    """

    BASE = "https://aiplatform.googleapis.com/v1beta1"

    def __init__(
        self,
        *,
        project_id: str,
        location: str,
        agent_id: str,
        access_token_provider: Callable[[], str],
        timeout_seconds: float = 240.0,
    ) -> None:
        self._project = project_id
        self._location = location
        self._agent_id = agent_id
        self._token_provider = access_token_provider
        self._timeout = timeout_seconds

    @property
    def agent_resource(self) -> str:
        return (
            f"projects/{self._project}/locations/{self._location}"
            f"/agents/{self._agent_id}"
        )

    def _headers(self) -> dict:
        return {
            "Authorization": f"Bearer {self._token_provider()}",
            "Content-Type": "application/json",
        }

    def create_environment(self) -> str:
        url = f"{self.BASE}/projects/{self._project}/locations/{self._location}/environments"
        r = httpx.post(url, headers=self._headers(), json={}, timeout=self._timeout)
        r.raise_for_status()
        data = r.json()
        return data["id"]

    def interact(
        self,
        *,
        environment_id: str,
        instruction: str,
        previous_interaction_id: str | None,
        agent_resource: str | None = None,
    ) -> AgentResponse:
        url = f"{self.BASE}/projects/{self._project}/locations/{self._location}/interactions"
        payload = {
            "agent": agent_resource or self.agent_resource,
            "environment": environment_id,
            "input": [
                {
                    "type": "user_input",
                    "content": [{"type": "text", "text": instruction}],
                }
            ],
        }
        if previous_interaction_id:
            payload["previous_interaction_id"] = previous_interaction_id

        r = httpx.post(url, headers=self._headers(), json=payload, timeout=self._timeout)
        if r.status_code == 404:
            raise EnvironmentNotFoundError(r.text)
        if r.status_code >= 400:
            raise AgentsAPIError(f"{r.status_code}: {r.text}")

        data = r.json()
        text = self._extract_text(data)
        return AgentResponse(
            interaction_id=data["id"],
            environment_id=data.get("environment_id", environment_id),
            text=text,
            raw=data,
        )

    @staticmethod
    def _extract_text(data: dict) -> str:
        for item in data.get("output", []):
            if item.get("type") == "agent_response":
                for part in item.get("content", []):
                    if part.get("type") == "text":
                        return part["text"]
        return ""
