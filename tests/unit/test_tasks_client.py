import json
from unittest.mock import MagicMock
from app.tasks_client import TasksDispatcher, ResearchJob


def test_enqueue_creates_http_task(mocker) -> None:
    fake_client = MagicMock()
    mocker.patch("app.tasks_client.tasks_v2.CloudTasksClient", return_value=fake_client)
    fake_client.queue_path.return_value = "projects/p/locations/asia-east1/queues/q"

    dispatcher = TasksDispatcher(
        project_id="p",
        location="asia-east1",
        queue="q",
        target_url="https://svc.run.app/tasks/run-research",
        service_account_email="svc@p.iam.gserviceaccount.com",
    )
    job = ResearchJob(
        line_user_id="U1",
        topic="向量資料庫",
        mode="new",
        report_id=None,
    )
    dispatcher.enqueue(job, task_id="t-abc")

    fake_client.create_task.assert_called_once()
    kwargs = fake_client.create_task.call_args.kwargs
    task = kwargs["task"]
    assert task["http_request"]["url"] == "https://svc.run.app/tasks/run-research"
    body = json.loads(task["http_request"]["body"])
    assert body == {
        "line_user_id": "U1",
        "topic": "向量資料庫",
        "mode": "new",
        "report_id": None,
        "task_id": "t-abc",
    }
    assert task["http_request"]["oidc_token"]["service_account_email"] == \
        "svc@p.iam.gserviceaccount.com"
