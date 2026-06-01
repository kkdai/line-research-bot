import json
from dataclasses import dataclass, asdict
from typing import Literal

from google.cloud import tasks_v2


Mode = Literal["new", "deepen", "retry_write", "retry_publish"]


@dataclass
class ResearchJob:
    line_user_id: str
    topic: str
    mode: Mode
    report_id: str | None


class TasksDispatcher:
    def __init__(
        self,
        *,
        project_id: str,
        location: str,
        queue: str,
        target_url: str,
        service_account_email: str,
    ) -> None:
        self._client = tasks_v2.CloudTasksClient()
        self._queue_path = self._client.queue_path(project_id, location, queue)
        self._target_url = target_url
        self._sa_email = service_account_email

    def enqueue(self, job: ResearchJob, *, task_id: str) -> None:
        payload = {**asdict(job), "task_id": task_id}
        task = {
            "http_request": {
                "http_method": tasks_v2.HttpMethod.POST,
                "url": self._target_url,
                "headers": {"Content-Type": "application/json"},
                "body": json.dumps(payload).encode("utf-8"),
                "oidc_token": {"service_account_email": self._sa_email},
            }
        }
        self._client.create_task(parent=self._queue_path, task=task)
