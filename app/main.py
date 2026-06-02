import os
import sys
from fastapi import FastAPI, Request
from fastapi.concurrency import run_in_threadpool

import google.auth
import google.auth.transport.requests
from google.cloud import firestore

from app.agents_client import AgentsClient
from app.config import get_settings
from app.line_client import LineClient
from app.state import StateStore
from app.tasks_client import TasksDispatcher
from app.webhook import build_webhook_router
from app.worker import ResearchWorker, JobPayload


def _build_token_provider() -> "callable":
    creds = None
    auth_req = google.auth.transport.requests.Request()

    def _provider() -> str:
        nonlocal creds
        if creds is None:
            creds, _ = google.auth.default(
                scopes=["https://www.googleapis.com/auth/cloud-platform"]
            )
        if not creds.valid:
            creds.refresh(auth_req)
        return creds.token

    return _provider


def create_app() -> FastAPI:
    s = get_settings()

    if s.use_firestore_emulator:
        os.environ["FIRESTORE_EMULATOR_HOST"] = s.firestore_emulator_host

    fs = firestore.Client(project=s.gcp_project_id)
    store = StateStore(fs)
    line = LineClient(
        access_token=s.line_channel_access_token,
        channel_secret=s.line_channel_secret,
    )
    agents = AgentsClient(
        project_id=s.gcp_project_id,
        location=s.gcp_location,
        agent_id=s.agent_id,
        access_token_provider=_build_token_provider(),
    )
    sa_email = os.environ.get(
        "SERVICE_ACCOUNT_EMAIL",
        f"{s.gcp_project_id}-compute@developer.gserviceaccount.com",
    )
    if not s.cloud_run_service_url:
        print(
            "WARNING: CLOUD_RUN_SERVICE_URL is not set; using placeholder URL. "
            "Re-deploy after the first deploy to set the real URL.",
            file=sys.stderr,
        )
    _tasks_target_url = (
        f"{s.cloud_run_service_url}/tasks/run-research"
        if s.cloud_run_service_url
        else "http://placeholder/tasks/run-research"
    )
    tasks = TasksDispatcher(
        project_id=s.gcp_project_id,
        location=s.cloud_tasks_location,
        queue=s.cloud_tasks_queue,
        target_url=_tasks_target_url,
        service_account_email=sa_email,
    )
    worker = ResearchWorker(
        store=store, agents=agents, line=line, gcs_bucket=s.gcs_bucket,
    )

    app = FastAPI()
    app.include_router(
        build_webhook_router(store=store, line=line, tasks=tasks, agents=agents)
    )

    @app.post("/tasks/run-research")
    async def run_research(req: Request) -> dict:
        data = await req.json()
        job = JobPayload(
            line_user_id=data["line_user_id"],
            topic=data["topic"],
            mode=data["mode"],
            report_id=data.get("report_id"),
            task_id=data["task_id"],
        )
        try:
            await run_in_threadpool(worker.run, job)
        finally:
            await run_in_threadpool(store.release_lock, job.line_user_id)
        return {"ok": True}

    @app.get("/healthz")
    async def healthz() -> dict:
        return {"status": "ok"}

    return app


app = create_app()
