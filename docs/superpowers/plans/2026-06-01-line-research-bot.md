# LINE Research Planner Bot — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a LINE Bot demo that uses Google Gemini Enterprise Agent Platform (Managed Agents API) to research a user-given topic by cross-comparing multiple Google sources and publishing a Markdown report as a public HTML page, with progressive deepening that updates the same report in the same sandbox across turns.

**Architecture:** Cloud Run (FastAPI) accepts LINE webhooks, validates signatures, immediately ACKs with reply token, and enqueues a background task to Cloud Tasks. The worker calls a single shared Managed Agent (`research-planner`) in three stages (PLAN → SEARCH_COMPARE → WRITE_REPORT) over the same `environment_id` per LINE user. Reports are written as Markdown in the sandbox, rendered to HTML via `code_execution`, uploaded to a public GCS bucket, and shared back to LINE as a Flex Message with a URL. State (user → environment, current report, lock, pending_action) lives in Firestore.

**Tech Stack:** Python 3.11, FastAPI, line-bot-sdk v3, google-cloud-firestore, google-cloud-storage, google-cloud-tasks, google-auth (for Agents API REST), httpx, pytest, pytest-snapshot, Firestore Emulator, Cloud Run, Cloud Storage, Cloud Tasks, Secret Manager.

**Reference:** `docs/superpowers/specs/2026-06-01-line-research-bot-design.md`

---

## Task 1: Project scaffold

**Files:**
- Create: `pyproject.toml`
- Create: `.gitignore`
- Create: `.env.example`
- Create: `Makefile`
- Create: `app/__init__.py`
- Create: `tests/__init__.py`
- Create: `tests/unit/__init__.py`
- Create: `tests/integration/__init__.py`

- [ ] **Step 1: Create `pyproject.toml`**

```toml
[project]
name = "line-research-bot"
version = "0.1.0"
requires-python = ">=3.11,<3.12"
dependencies = [
    "fastapi>=0.115",
    "uvicorn[standard]>=0.32",
    "line-bot-sdk>=3.13",
    "google-cloud-firestore>=2.18",
    "google-cloud-storage>=2.18",
    "google-cloud-tasks>=2.16",
    "google-auth>=2.35",
    "httpx>=0.27",
    "pydantic-settings>=2.6",
    "tenacity>=9.0",
]

[project.optional-dependencies]
dev = [
    "pytest>=8.3",
    "pytest-asyncio>=0.24",
    "pytest-snapshot>=0.9",
    "pytest-mock>=3.14",
    "respx>=0.21",
    "ruff>=0.7",
]

[tool.pytest.ini_options]
asyncio_mode = "auto"
testpaths = ["tests"]

[tool.ruff]
line-length = 100
target-version = "py311"
```

- [ ] **Step 2: Create `.gitignore`**

```
__pycache__/
*.pyc
.venv/
.env
.env.local
.pytest_cache/
.ruff_cache/
.DS_Store
dist/
build/
*.egg-info/
.coverage
htmlcov/
```

- [ ] **Step 3: Create `.env.example`**

```
GCP_PROJECT_ID=
GCP_LOCATION=global
AGENT_ID=research-planner
GCS_BUCKET=line-reports
CLOUD_TASKS_QUEUE=research-jobs
CLOUD_TASKS_LOCATION=asia-east1
CLOUD_RUN_SERVICE_URL=
LINE_CHANNEL_SECRET=
LINE_CHANNEL_ACCESS_TOKEN=
USE_FIRESTORE_EMULATOR=false
FIRESTORE_EMULATOR_HOST=localhost:8081
```

- [ ] **Step 4: Create `Makefile`**

```makefile
.PHONY: install test test-integration lint run emulator

install:
	pip install -e ".[dev]"

emulator:
	firebase emulators:start --only firestore

test:
	USE_FIRESTORE_EMULATOR=true FIRESTORE_EMULATOR_HOST=localhost:8081 \
		pytest tests/unit -v

test-integration:
	pytest tests/integration -v -s

lint:
	ruff check app tests
	ruff format --check app tests

run:
	uvicorn app.main:app --reload --port 8080
```

- [ ] **Step 5: Create empty `__init__.py` files**

Run:
```bash
touch app/__init__.py tests/__init__.py tests/unit/__init__.py tests/integration/__init__.py
```

- [ ] **Step 6: Verify scaffold compiles**

Run: `pip install -e ".[dev]"`
Expected: install succeeds.

- [ ] **Step 7: Commit**

```bash
git add pyproject.toml .gitignore .env.example Makefile app tests
git commit -m "chore: project scaffold with dependencies and tooling"
```

---

## Task 2: Settings module (`app/config.py`)

**Files:**
- Create: `app/config.py`
- Create: `tests/unit/test_config.py`

- [ ] **Step 1: Write the failing test**

`tests/unit/test_config.py`:
```python
import os
import pytest
from app.config import Settings


def test_settings_reads_env(monkeypatch: pytest.MonkeyPatch) -> None:
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/unit/test_config.py -v`
Expected: FAIL (`app.config` does not exist).

- [ ] **Step 3: Implement `app/config.py`**

```python
from functools import lru_cache
from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    gcp_project_id: str = Field(...)
    gcp_location: str = Field(default="global")
    agent_id: str = Field(...)
    gcs_bucket: str = Field(...)
    cloud_tasks_queue: str = Field(...)
    cloud_tasks_location: str = Field(...)
    cloud_run_service_url: str = Field(...)
    line_channel_secret: str = Field(...)
    line_channel_access_token: str = Field(...)
    use_firestore_emulator: bool = Field(default=False)
    firestore_emulator_host: str = Field(default="localhost:8081")


@lru_cache
def get_settings() -> Settings:
    return Settings()
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/unit/test_config.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add app/config.py tests/unit/test_config.py
git commit -m "feat: typed settings module reading env vars"
```

---

## Task 3: Intent classifier (`app/intent.py`)

**Files:**
- Create: `app/intent.py`
- Create: `tests/unit/test_intent.py`

- [ ] **Step 1: Write the failing test**

`tests/unit/test_intent.py`:
```python
from app.intent import classify, Intent, UserState


BASE_STATE = UserState(current_report_id=None, pending_action=None)
WITH_REPORT = UserState(current_report_id="r1", pending_action=None)
WITH_RETRY = UserState(current_report_id="r1", pending_action="retry_write")


def test_new_research_when_no_report() -> None:
    assert classify("研究 SOTA 開源向量資料庫", BASE_STATE) == Intent.NEW


def test_new_research_keyword() -> None:
    assert classify("幫我查 LLM 評測", WITH_REPORT) == Intent.NEW
    assert classify("找一下 RAG 框架比較", WITH_REPORT) == Intent.NEW


def test_deepen_keywords() -> None:
    for msg in ["深化第 2 章", "補日文來源", "改第三章", "第 2 章再深一點"]:
        assert classify(msg, WITH_REPORT) == Intent.DEEPEN


def test_reset() -> None:
    for msg in ["重新開始", "換題目", "清除"]:
        assert classify(msg, WITH_REPORT) == Intent.RESET


def test_recall() -> None:
    for msg in ["上次的", "我之前的研究", "連結"]:
        assert classify(msg, WITH_REPORT) == Intent.RECALL


def test_retry_only_when_pending_action() -> None:
    assert classify("再試一次", WITH_RETRY) == Intent.RETRY
    assert classify("再發佈一次", WITH_RETRY) == Intent.RETRY
    # No pending action → falls through to chitchat/new
    assert classify("再試一次", WITH_REPORT) != Intent.RETRY


def test_chitchat_short_greeting() -> None:
    assert classify("你好", WITH_REPORT) == Intent.CHITCHAT
    assert classify("嗨", WITH_REPORT) == Intent.CHITCHAT


def test_new_when_no_match_and_no_report() -> None:
    assert classify("Hello world", BASE_STATE) == Intent.NEW
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/unit/test_intent.py -v`
Expected: FAIL (module not found).

- [ ] **Step 3: Implement `app/intent.py`**

```python
from dataclasses import dataclass
from enum import Enum
from typing import Literal


class Intent(str, Enum):
    NEW = "new"
    DEEPEN = "deepen"
    RESET = "reset"
    RECALL = "recall"
    RETRY = "retry"
    CHITCHAT = "chitchat"


PendingAction = Literal["retry_write", "retry_publish"] | None


@dataclass(frozen=True)
class UserState:
    current_report_id: str | None
    pending_action: PendingAction


_NEW_KEYWORDS = ("研究", "幫我查", "找一下")
_DEEPEN_KEYWORDS = ("深化", "補", "改", "第")
_RESET_KEYWORDS = ("重新開始", "換題目", "清除")
_RECALL_KEYWORDS = ("上次", "我之前", "連結")
_RETRY_KEYWORDS = ("再試一次", "再發佈一次")
_CHITCHAT_GREETINGS = ("你好", "嗨", "hi", "hello", "哈囉")


def classify(message: str, state: UserState) -> Intent:
    m = message.strip()

    if state.pending_action is not None and any(k in m for k in _RETRY_KEYWORDS):
        return Intent.RETRY

    if any(m.startswith(k) for k in _RESET_KEYWORDS):
        return Intent.RESET

    if any(k in m for k in _RECALL_KEYWORDS):
        return Intent.RECALL

    if state.current_report_id is not None and any(m.startswith(k) for k in _DEEPEN_KEYWORDS):
        return Intent.DEEPEN

    if any(m.startswith(k) for k in _NEW_KEYWORDS):
        return Intent.NEW

    if len(m) < 5 and any(g in m.lower() for g in _CHITCHAT_GREETINGS):
        return Intent.CHITCHAT

    if state.current_report_id is None:
        return Intent.NEW

    return Intent.CHITCHAT
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/unit/test_intent.py -v`
Expected: PASS (all 8 cases).

- [ ] **Step 5: Commit**

```bash
git add app/intent.py tests/unit/test_intent.py
git commit -m "feat: keyword-based intent classifier"
```

---

## Task 4: Firestore emulator fixture + state DAO (`app/state.py`)

**Files:**
- Create: `tests/conftest.py`
- Create: `app/state.py`
- Create: `tests/unit/test_state.py`

- [ ] **Step 1: Create `tests/conftest.py` with emulator fixture**

```python
import os
import uuid
import pytest
from google.cloud import firestore


@pytest.fixture
def firestore_client() -> firestore.Client:
    """Yields a Firestore client pointed at the local emulator.
    Each test gets a unique project id so collections don't collide.
    """
    project = f"test-{uuid.uuid4().hex[:8]}"
    os.environ["FIRESTORE_EMULATOR_HOST"] = os.environ.get(
        "FIRESTORE_EMULATOR_HOST", "localhost:8081"
    )
    return firestore.Client(project=project)
```

- [ ] **Step 2: Write failing tests for state DAO**

`tests/unit/test_state.py`:
```python
import datetime as dt
import pytest
from google.cloud import firestore

from app.state import StateStore, UserRecord, ReportRecord, LockTimeoutError


@pytest.fixture
def store(firestore_client: firestore.Client) -> StateStore:
    return StateStore(firestore_client)


def test_get_or_create_user_creates(store: StateStore) -> None:
    user = store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    assert user.line_user_id == "U1"
    assert user.environment_id == "env-A"
    assert user.current_report_id is None
    assert user.pending_action is None


def test_get_or_create_user_returns_existing(store: StateStore) -> None:
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    user = store.get_or_create_user("U1", environment_factory=lambda: "env-B")
    assert user.environment_id == "env-A"  # factory not called second time


def test_acquire_lock_succeeds(store: StateStore) -> None:
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.acquire_lock("U1", task_id="t1", ttl_seconds=300)
    user = store.get_user("U1")
    assert user.lock is not None
    assert user.lock["task_id"] == "t1"


def test_acquire_lock_when_locked_raises(store: StateStore) -> None:
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.acquire_lock("U1", task_id="t1", ttl_seconds=300)
    with pytest.raises(LockTimeoutError):
        store.acquire_lock("U1", task_id="t2", ttl_seconds=300)


def test_lock_expires_then_can_reacquire(store: StateStore) -> None:
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.acquire_lock("U1", task_id="t1", ttl_seconds=-1)  # already expired
    store.acquire_lock("U1", task_id="t2", ttl_seconds=300)  # should succeed


def test_release_lock(store: StateStore) -> None:
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.acquire_lock("U1", task_id="t1", ttl_seconds=300)
    store.release_lock("U1")
    user = store.get_user("U1")
    assert user.lock is None


def test_set_pending_action(store: StateStore) -> None:
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.set_pending_action("U1", "retry_write")
    user = store.get_user("U1")
    assert user.pending_action == "retry_write"


def test_clear_pending_action(store: StateStore) -> None:
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.set_pending_action("U1", "retry_write")
    store.set_pending_action("U1", None)
    user = store.get_user("U1")
    assert user.pending_action is None


def test_create_and_get_report(store: StateStore) -> None:
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    report = store.create_report(
        user_id="U1",
        topic="向量資料庫",
        summary="...",
        gcs_url="https://storage.googleapis.com/x/r1/index.html",
    )
    fetched = store.get_report(report.report_id)
    assert fetched.topic == "向量資料庫"
    assert fetched.version == 1


def test_deepen_report_bumps_version_and_appends_history(store: StateStore) -> None:
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    r = store.create_report(
        user_id="U1", topic="x", summary="s1",
        gcs_url="https://storage.googleapis.com/b/r/index.html",
    )
    store.deepen_report(
        r.report_id,
        new_summary="s2",
        snapshot_url="https://storage.googleapis.com/b/r/snapshots/v1.html",
    )
    updated = store.get_report(r.report_id)
    assert updated.version == 2
    assert updated.summary == "s2"
    assert len(updated.history) == 1
    assert updated.history[0]["version"] == 1


def test_set_current_report_and_last_interaction(store: StateStore) -> None:
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.set_current_report("U1", "r1")
    store.set_last_interaction("U1", "int-xyz")
    u = store.get_user("U1")
    assert u.current_report_id == "r1"
    assert u.last_interaction_id == "int-xyz"
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `pytest tests/unit/test_state.py -v`
Expected: FAIL (module not found).

- [ ] **Step 4: Implement `app/state.py`**

```python
import datetime as dt
import uuid
from dataclasses import dataclass, field
from typing import Callable, Literal

from google.cloud import firestore

PendingAction = Literal["retry_write", "retry_publish"] | None


class LockTimeoutError(Exception):
    pass


@dataclass
class UserRecord:
    line_user_id: str
    environment_id: str
    current_report_id: str | None
    last_interaction_id: str | None
    last_active_at: dt.datetime
    lock: dict | None
    pending_action: PendingAction


@dataclass
class ReportRecord:
    report_id: str
    user_id: str
    topic: str
    summary: str
    gcs_url: str
    version: int
    history: list[dict] = field(default_factory=list)
    created_at: dt.datetime | None = None
    updated_at: dt.datetime | None = None


def _now() -> dt.datetime:
    return dt.datetime.now(dt.timezone.utc)


class StateStore:
    def __init__(self, client: firestore.Client) -> None:
        self._db = client

    # ---------- users ----------

    def get_user(self, line_user_id: str) -> UserRecord | None:
        snap = self._db.collection("users").document(line_user_id).get()
        if not snap.exists:
            return None
        d = snap.to_dict() or {}
        return UserRecord(
            line_user_id=line_user_id,
            environment_id=d["environment_id"],
            current_report_id=d.get("current_report_id"),
            last_interaction_id=d.get("last_interaction_id"),
            last_active_at=d.get("last_active_at", _now()),
            lock=d.get("lock"),
            pending_action=d.get("pending_action"),
        )

    def get_or_create_user(
        self,
        line_user_id: str,
        environment_factory: Callable[[], str],
    ) -> UserRecord:
        existing = self.get_user(line_user_id)
        if existing:
            return existing
        env_id = environment_factory()
        doc = {
            "environment_id": env_id,
            "current_report_id": None,
            "last_interaction_id": None,
            "last_active_at": _now(),
            "lock": None,
            "pending_action": None,
        }
        self._db.collection("users").document(line_user_id).set(doc)
        return UserRecord(
            line_user_id=line_user_id,
            environment_id=env_id,
            current_report_id=None,
            last_interaction_id=None,
            last_active_at=doc["last_active_at"],
            lock=None,
            pending_action=None,
        )

    def set_environment(self, line_user_id: str, environment_id: str) -> None:
        self._db.collection("users").document(line_user_id).update(
            {"environment_id": environment_id, "last_active_at": _now()}
        )

    def set_current_report(self, line_user_id: str, report_id: str | None) -> None:
        self._db.collection("users").document(line_user_id).update(
            {"current_report_id": report_id, "last_active_at": _now()}
        )

    def set_last_interaction(self, line_user_id: str, interaction_id: str) -> None:
        self._db.collection("users").document(line_user_id).update(
            {"last_interaction_id": interaction_id, "last_active_at": _now()}
        )

    def set_pending_action(self, line_user_id: str, action: PendingAction) -> None:
        self._db.collection("users").document(line_user_id).update(
            {"pending_action": action}
        )

    # ---------- lock ----------

    def acquire_lock(
        self,
        line_user_id: str,
        task_id: str,
        ttl_seconds: int,
    ) -> None:
        @firestore.transactional
        def _txn(txn: firestore.Transaction) -> None:
            ref = self._db.collection("users").document(line_user_id)
            snap = ref.get(transaction=txn)
            data = snap.to_dict() or {}
            lock = data.get("lock")
            now = _now()
            if lock and lock["lock_until"] > now:
                raise LockTimeoutError(
                    f"Locked by {lock['task_id']} until {lock['lock_until']}"
                )
            txn.update(
                ref,
                {
                    "lock": {
                        "task_id": task_id,
                        "lock_until": now + dt.timedelta(seconds=ttl_seconds),
                    }
                },
            )

        _txn(self._db.transaction())

    def release_lock(self, line_user_id: str) -> None:
        self._db.collection("users").document(line_user_id).update({"lock": None})

    # ---------- reports ----------

    def create_report(
        self,
        user_id: str,
        topic: str,
        summary: str,
        gcs_url: str,
    ) -> ReportRecord:
        report_id = uuid.uuid4().hex
        now = _now()
        doc = {
            "user_id": user_id,
            "topic": topic,
            "summary": summary,
            "gcs_url": gcs_url,
            "version": 1,
            "history": [],
            "created_at": now,
            "updated_at": now,
        }
        self._db.collection("reports").document(report_id).set(doc)
        return ReportRecord(
            report_id=report_id,
            user_id=user_id,
            topic=topic,
            summary=summary,
            gcs_url=gcs_url,
            version=1,
            history=[],
            created_at=now,
            updated_at=now,
        )

    def get_report(self, report_id: str) -> ReportRecord | None:
        snap = self._db.collection("reports").document(report_id).get()
        if not snap.exists:
            return None
        d = snap.to_dict() or {}
        return ReportRecord(
            report_id=report_id,
            user_id=d["user_id"],
            topic=d["topic"],
            summary=d["summary"],
            gcs_url=d["gcs_url"],
            version=d["version"],
            history=d.get("history", []),
            created_at=d.get("created_at"),
            updated_at=d.get("updated_at"),
        )

    def deepen_report(
        self,
        report_id: str,
        new_summary: str,
        snapshot_url: str,
    ) -> ReportRecord:
        ref = self._db.collection("reports").document(report_id)

        @firestore.transactional
        def _txn(txn: firestore.Transaction) -> ReportRecord:
            snap = ref.get(transaction=txn)
            d = snap.to_dict() or {}
            old_version = d["version"]
            history = d.get("history", [])
            history.append(
                {
                    "version": old_version,
                    "gcs_snapshot_url": snapshot_url,
                    "created_at": _now(),
                }
            )
            new_version = old_version + 1
            txn.update(
                ref,
                {
                    "version": new_version,
                    "summary": new_summary,
                    "history": history,
                    "updated_at": _now(),
                },
            )
            return ReportRecord(
                report_id=report_id,
                user_id=d["user_id"],
                topic=d["topic"],
                summary=new_summary,
                gcs_url=d["gcs_url"],
                version=new_version,
                history=history,
                created_at=d.get("created_at"),
                updated_at=_now(),
            )

        return _txn(self._db.transaction())
```

- [ ] **Step 5: Start Firestore emulator (if not already running)**

Run in separate terminal: `firebase emulators:start --only firestore`
Expected: emulator listening on `localhost:8081`.

- [ ] **Step 6: Run tests to verify they pass**

Run: `USE_FIRESTORE_EMULATOR=true FIRESTORE_EMULATOR_HOST=localhost:8081 pytest tests/unit/test_state.py -v`
Expected: all 10 tests PASS.

- [ ] **Step 7: Commit**

```bash
git add app/state.py tests/conftest.py tests/unit/test_state.py
git commit -m "feat: Firestore state DAO with lock and report versioning"
```

---

## Task 5: Flex Message builder (`app/flex.py`)

**Files:**
- Create: `app/flex.py`
- Create: `tests/unit/test_flex.py`
- Create: `tests/unit/__snapshots__/` (auto-created by pytest-snapshot)

- [ ] **Step 1: Write failing tests**

`tests/unit/test_flex.py`:
```python
from app.flex import build_report_card, build_progress_text


def test_progress_text_planning() -> None:
    assert build_progress_text("plan_done", source_count=7) == (
        "🔍 計畫完成：將比對 7 類來源"
    )


def test_progress_text_search_done() -> None:
    text = build_progress_text("search_done", source_count=7, disagreement_count=3)
    assert "已比對 7 份來源" in text
    assert "3" in text


def test_progress_text_starting() -> None:
    assert build_progress_text("planning") == "📋 已收到題目，開始規劃…"


def test_report_card_has_expected_shape() -> None:
    card = build_report_card(
        topic="向量資料庫選型",
        summary="一段 500 字的摘要…",
        report_url="https://storage.googleapis.com/x/r1/index.html",
        citations=[
            {"title": "Milvus 官方文件", "url": "https://milvus.io"},
            {"title": "Pinecone benchmark", "url": "https://pinecone.io/blog/b"},
        ],
        version=1,
    )
    assert card["type"] == "flex"
    assert card["altText"].startswith("📝")
    contents = card["contents"]
    assert contents["type"] == "bubble"
    # Footer button URL must match
    footer_buttons = contents["footer"]["contents"]
    assert footer_buttons[0]["action"]["uri"] == "https://storage.googleapis.com/x/r1/index.html"


def test_report_card_v2_shows_version_badge() -> None:
    card = build_report_card(
        topic="x", summary="y",
        report_url="https://example.com/r/index.html",
        citations=[],
        version=2,
    )
    # find "v2" string anywhere in the bubble
    import json
    assert "v2" in json.dumps(card, ensure_ascii=False)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pytest tests/unit/test_flex.py -v`
Expected: FAIL (module not found).

- [ ] **Step 3: Implement `app/flex.py`**

```python
from typing import Literal


ProgressStage = Literal["planning", "plan_done", "search_done", "writing"]


def build_progress_text(
    stage: ProgressStage,
    *,
    source_count: int = 0,
    disagreement_count: int = 0,
) -> str:
    if stage == "planning":
        return "📋 已收到題目，開始規劃…"
    if stage == "plan_done":
        return f"🔍 計畫完成：將比對 {source_count} 類來源"
    if stage == "search_done":
        return (
            f"📊 已比對 {source_count} 份來源，"
            f"發現 {disagreement_count} 處重要分歧"
        )
    if stage == "writing":
        return "📝 正在撰寫報告…"
    raise ValueError(f"unknown stage: {stage}")


def build_report_card(
    *,
    topic: str,
    summary: str,
    report_url: str,
    citations: list[dict],
    version: int,
) -> dict:
    summary_short = summary if len(summary) <= 400 else summary[:400] + "…"
    badge = f"v{version}"

    citation_blocks = []
    for c in citations[:3]:
        citation_blocks.append({
            "type": "text",
            "text": f"• {c['title']}",
            "wrap": True,
            "size": "xs",
            "color": "#666666",
            "action": {"type": "uri", "uri": c["url"]},
        })

    body_contents = [
        {
            "type": "box",
            "layout": "horizontal",
            "contents": [
                {"type": "text", "text": topic, "weight": "bold", "size": "lg",
                 "wrap": True, "flex": 5},
                {"type": "text", "text": badge, "size": "sm", "color": "#888888",
                 "align": "end", "flex": 1},
            ],
        },
        {"type": "separator", "margin": "md"},
        {"type": "text", "text": summary_short, "wrap": True, "size": "sm",
         "margin": "md"},
    ]
    if citation_blocks:
        body_contents.append({"type": "separator", "margin": "md"})
        body_contents.append({"type": "text", "text": "主要來源",
                              "size": "xs", "color": "#888888", "margin": "md"})
        body_contents.extend(citation_blocks)

    return {
        "type": "flex",
        "altText": f"📝 {topic} 研究報告已完成",
        "contents": {
            "type": "bubble",
            "body": {
                "type": "box",
                "layout": "vertical",
                "contents": body_contents,
            },
            "footer": {
                "type": "box",
                "layout": "vertical",
                "contents": [
                    {
                        "type": "button",
                        "style": "primary",
                        "action": {
                            "type": "uri",
                            "label": "閱讀完整報告",
                            "uri": report_url,
                        },
                    }
                ],
            },
        },
    }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pytest tests/unit/test_flex.py -v`
Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add app/flex.py tests/unit/test_flex.py
git commit -m "feat: progress text + Flex report card builder"
```

---

## Task 6: LINE client wrapper (`app/line_client.py`)

**Files:**
- Create: `app/line_client.py`
- Create: `tests/unit/test_line_client.py`

- [ ] **Step 1: Write failing tests**

`tests/unit/test_line_client.py`:
```python
import pytest
from unittest.mock import MagicMock
from app.line_client import LineClient


@pytest.fixture
def mock_messaging_api(mocker):
    api = MagicMock()
    mocker.patch("app.line_client.MessagingApi", return_value=api)
    return api


def test_reply_text(mock_messaging_api: MagicMock) -> None:
    client = LineClient(access_token="t", channel_secret="s")
    client.reply_text(reply_token="r1", text="hello")
    mock_messaging_api.reply_message.assert_called_once()
    req = mock_messaging_api.reply_message.call_args[0][0]
    assert req.reply_token == "r1"
    assert req.messages[0].text == "hello"


def test_push_text(mock_messaging_api: MagicMock) -> None:
    client = LineClient(access_token="t", channel_secret="s")
    client.push_text(user_id="U1", text="progress")
    mock_messaging_api.push_message.assert_called_once()
    req = mock_messaging_api.push_message.call_args[0][0]
    assert req.to == "U1"
    assert req.messages[0].text == "progress"


def test_push_flex(mock_messaging_api: MagicMock) -> None:
    client = LineClient(access_token="t", channel_secret="s")
    flex = {"type": "flex", "altText": "x", "contents": {"type": "bubble"}}
    client.push_flex(user_id="U1", flex_message=flex)
    mock_messaging_api.push_message.assert_called_once()


def test_verify_signature_valid() -> None:
    import base64
    import hashlib
    import hmac

    secret = "test-secret"
    body = b'{"events":[]}'
    expected_sig = base64.b64encode(
        hmac.new(secret.encode(), body, hashlib.sha256).digest()
    ).decode()

    client = LineClient(access_token="t", channel_secret=secret)
    assert client.verify_signature(body, expected_sig) is True


def test_verify_signature_invalid() -> None:
    client = LineClient(access_token="t", channel_secret="s")
    assert client.verify_signature(b'{}', "wrong-sig") is False
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pytest tests/unit/test_line_client.py -v`
Expected: FAIL (module not found).

- [ ] **Step 3: Implement `app/line_client.py`**

```python
import base64
import hashlib
import hmac

from linebot.v3.messaging import (
    ApiClient,
    Configuration,
    MessagingApi,
    PushMessageRequest,
    ReplyMessageRequest,
    TextMessage,
    FlexMessage,
    FlexContainer,
)


class LineClient:
    def __init__(self, *, access_token: str, channel_secret: str) -> None:
        self._channel_secret = channel_secret
        config = Configuration(access_token=access_token)
        api_client = ApiClient(config)
        self._api = MessagingApi(api_client)

    def verify_signature(self, body: bytes, signature: str) -> bool:
        digest = hmac.new(
            self._channel_secret.encode("utf-8"), body, hashlib.sha256
        ).digest()
        expected = base64.b64encode(digest).decode("utf-8")
        return hmac.compare_digest(expected, signature)

    def reply_text(self, *, reply_token: str, text: str) -> None:
        req = ReplyMessageRequest(
            reply_token=reply_token, messages=[TextMessage(text=text)]
        )
        self._api.reply_message(req)

    def push_text(self, *, user_id: str, text: str) -> None:
        req = PushMessageRequest(to=user_id, messages=[TextMessage(text=text)])
        self._api.push_message(req)

    def push_flex(self, *, user_id: str, flex_message: dict) -> None:
        req = PushMessageRequest(
            to=user_id,
            messages=[
                FlexMessage(
                    alt_text=flex_message["altText"],
                    contents=FlexContainer.from_dict(flex_message["contents"]),
                )
            ],
        )
        self._api.push_message(req)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pytest tests/unit/test_line_client.py -v`
Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add app/line_client.py tests/unit/test_line_client.py
git commit -m "feat: LINE Messaging API wrapper with signature verification"
```

---

## Task 7: Cloud Tasks dispatcher (`app/tasks_client.py`)

**Files:**
- Create: `app/tasks_client.py`
- Create: `tests/unit/test_tasks_client.py`

- [ ] **Step 1: Write failing tests**

`tests/unit/test_tasks_client.py`:
```python
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/unit/test_tasks_client.py -v`
Expected: FAIL (module not found).

- [ ] **Step 3: Implement `app/tasks_client.py`**

```python
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pytest tests/unit/test_tasks_client.py -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add app/tasks_client.py tests/unit/test_tasks_client.py
git commit -m "feat: Cloud Tasks dispatcher for research jobs"
```

---

## Task 8: System instructions (3 prompt files)

**Files:**
- Create: `app/system_instructions/plan.md`
- Create: `app/system_instructions/search_compare.md`
- Create: `app/system_instructions/write_report.md`

- [ ] **Step 1: Create `app/system_instructions/plan.md`**

```markdown
# Stage: PLAN

You will receive a research topic in 繁體中文.

## Your task
1. Decide 4-8 specific Google search queries that together cover the topic from multiple angles (official docs, benchmarks, community reviews, recent news, opposing viewpoints).
2. For each query, name the source category (e.g. "official_docs", "benchmark", "community_review", "news", "academic").
3. Decide languages to include (default: zh-TW and en; add others only if the topic clearly requires).
4. Write the plan to `/workspace/plan.json` using `code_execution`.

## Output JSON written to `/workspace/plan.json` AND returned in your message
```json
{
  "topic": "<original topic>",
  "queries": [
    {"q": "...", "source_category": "official_docs", "lang": "en"},
    ...
  ],
  "source_count": <int, equal to len(queries)>
}
```

Return ONLY the JSON. No prose, no markdown fences.
```

- [ ] **Step 2: Create `app/system_instructions/search_compare.md`**

```markdown
# Stage: SEARCH_COMPARE

## Your task
1. Read `/workspace/plan.json`.
2. For each query, use `google_search` then `url_context` on the top 1-2 results to read the actual page content.
3. For each source, record: title, url, language, 200-word factual summary, key claims (as a list).
4. Across all sources, identify:
   - **agreements** (claims supported by ≥ 2 sources)
   - **disagreements** (claims contradicted across sources)
   - **gaps** (questions the topic raises that no source answered)
5. Write the result to `/workspace/sources.json`.

## Output JSON written to `/workspace/sources.json` AND returned in your message
```json
{
  "sources": [
    {
      "title": "...",
      "url": "...",
      "lang": "en",
      "summary": "...",
      "key_claims": ["...", "..."]
    }
  ],
  "agreements": ["..."],
  "disagreements": [
    {"point": "...", "sides": [{"claim": "...", "source_urls": ["..."]}, ...]}
  ],
  "gaps": ["..."],
  "source_count": <int>,
  "disagreement_count": <int>
}
```

Return ONLY the JSON.
```

- [ ] **Step 3: Create `app/system_instructions/write_report.md`**

```markdown
# Stage: WRITE_REPORT

You will receive metadata in the input:
- `report_id`: target GCS path component
- `mode`: "new" | "deepen"
- `previous_version`: integer, present only when mode is "deepen"
- `deepen_request`: free-text user instruction, present only when mode is "deepen"
  (e.g. "第 3 章再深一點，加日文來源")

## Your task

### If mode == "new"
1. Read `/workspace/sources.json`.
2. Write `/workspace/report.md` with this structure (繁體中文):
   - 標題
   - 摘要（500 字）
   - 章節 1-N（每章引用至少 2 個來源，footnote 形式 `[^url]`）
   - 跨來源分歧討論
   - 結論與建議
   - 來源列表

### If mode == "deepen"
1. Read `/workspace/sources.json` and `/workspace/report.md`.
2. Parse `deepen_request` to identify target chapter(s) or change type:
   - If "第 N 章" mentioned, modify only that chapter.
   - If "補 X 來源" mentioned, run additional google_search + url_context, append to sources.json, integrate into relevant chapters.
3. If `deepen_request` references a chapter number that does not exist in `report.md`, **stop and return**:
   ```json
   {"error": "chapter_not_found", "available_chapters": ["1. ...", "2. ..."]}
   ```
4. Rewrite `/workspace/report.md` with changes applied; regenerate summary.

### Render and publish (both modes)
5. `code_execution`: install markdown if missing
   ```bash
   pip install --quiet markdown
   ```
6. Run a Python snippet to render `report.md` → `report.html` with inline CSS:
   - Use `markdown.markdown(text, extensions=["fenced_code", "tables", "footnotes"])`.
   - Wrap output in a complete `<html>` doc with a `<style>` block. Add a top banner:
     `<div class="banner">v{NEW_VERSION} · 更新於 {timestamp}</div>`.
7. If mode == "deepen":
   ```bash
   gsutil -h "Cache-Control:no-cache, max-age=0" mv \
       gs://line-reports/{report_id}/index.html \
       gs://line-reports/{report_id}/snapshots/v{previous_version}.html
   ```
8. Upload new index.html:
   ```bash
   gsutil -h "Cache-Control:no-cache, max-age=0" cp \
       /workspace/report.html gs://line-reports/{report_id}/index.html
   ```
9. Verify it is publicly reachable:
   ```bash
   curl -sI https://storage.googleapis.com/line-reports/{report_id}/index.html | head -1
   ```
   If not `HTTP/2 200`, return `{"error": "publish_failed"}`.

## Output JSON returned in your message
```json
{
  "report_id": "<same as input>",
  "summary_500": "<500-char summary>",
  "top_citations": [
    {"title": "...", "url": "..."},
    {"title": "...", "url": "..."},
    {"title": "...", "url": "..."}
  ],
  "new_version": <int, 1 if mode=="new" else previous_version+1>
}
```

Return ONLY the JSON.
```

- [ ] **Step 4: Verify files exist**

Run: `ls -la app/system_instructions/`
Expected: 3 files visible.

- [ ] **Step 5: Commit**

```bash
git add app/system_instructions
git commit -m "feat: system instructions for plan/search/write stages"
```

---

## Task 9: Agents API client wrapper (`app/agents_client.py`)

The Pre-GA Managed Agents API uses REST. We wrap the three calls we need.

**Files:**
- Create: `app/agents_client.py`
- Create: `tests/unit/test_agents_client.py`

- [ ] **Step 1: Write failing tests**

`tests/unit/test_agents_client.py`:
```python
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pytest tests/unit/test_agents_client.py -v`
Expected: FAIL (module not found).

- [ ] **Step 3: Implement `app/agents_client.py`**

```python
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pytest tests/unit/test_agents_client.py -v`
Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add app/agents_client.py tests/unit/test_agents_client.py
git commit -m "feat: Agents API REST wrapper with create_environment and interact"
```

---

## Task 10: Worker orchestrator — happy path (`app/worker.py`)

**Files:**
- Create: `app/worker.py`
- Create: `tests/unit/test_worker.py`

- [ ] **Step 1: Write happy-path test**

`tests/unit/test_worker.py`:
```python
import json
from unittest.mock import MagicMock, call

import pytest
from google.cloud import firestore

from app.agents_client import AgentResponse, EnvironmentNotFoundError
from app.state import StateStore
from app.worker import ResearchWorker, JobPayload


@pytest.fixture
def worker(firestore_client: firestore.Client) -> ResearchWorker:
    store = StateStore(firestore_client)
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    return ResearchWorker(
        store=store,
        agents=MagicMock(),
        line=MagicMock(),
        gcs_bucket="line-reports",
    )


def _ok(text: str, interaction_id: str = "i") -> AgentResponse:
    return AgentResponse(interaction_id=interaction_id, environment_id="env-A",
                         text=text, raw={})


def test_new_research_happy_path(worker: ResearchWorker) -> None:
    worker._agents.interact.side_effect = [
        _ok(json.dumps({"topic": "x", "queries": [1, 2, 3], "source_count": 3}), "i1"),
        _ok(json.dumps({"sources": [1, 2, 3], "source_count": 3,
                        "disagreement_count": 1, "agreements": [], "disagreements": [], "gaps": []}), "i2"),
        _ok(json.dumps({
            "report_id": "auto",
            "summary_500": "summary",
            "top_citations": [{"title": "T1", "url": "https://a"}],
            "new_version": 1,
        }), "i3"),
    ]

    job = JobPayload(line_user_id="U1", topic="向量資料庫", mode="new", report_id=None, task_id="t1")
    worker.run(job)

    # Three Agent calls
    assert worker._agents.interact.call_count == 3
    # Last call carries previous_interaction_id from i2
    last_kwargs = worker._agents.interact.call_args_list[2].kwargs
    assert last_kwargs["previous_interaction_id"] == "i2"
    # Progress pushes: planning, plan_done, search_done, final flex card
    push_text_calls = worker._line.push_text.call_args_list
    push_flex_calls = worker._line.push_flex.call_args_list
    assert len(push_text_calls) >= 2  # plan_done + search_done
    assert len(push_flex_calls) == 1
    # current_report_id is set
    user = worker._store.get_user("U1")
    assert user.current_report_id is not None
    report = worker._store.get_report(user.current_report_id)
    assert report.version == 1
    assert report.summary == "summary"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pytest tests/unit/test_worker.py::test_new_research_happy_path -v`
Expected: FAIL (module not found).

- [ ] **Step 3: Implement happy path in `app/worker.py`**

```python
import json
import uuid
from dataclasses import dataclass
from typing import Literal

from app.agents_client import AgentsClient, AgentResponse, EnvironmentNotFoundError
from app.flex import build_progress_text, build_report_card
from app.line_client import LineClient
from app.state import StateStore


Mode = Literal["new", "deepen", "retry_write", "retry_publish"]


@dataclass
class JobPayload:
    line_user_id: str
    topic: str
    mode: Mode
    report_id: str | None
    task_id: str


class ResearchWorker:
    def __init__(
        self,
        *,
        store: StateStore,
        agents: AgentsClient,
        line: LineClient,
        gcs_bucket: str,
    ) -> None:
        self._store = store
        self._agents = agents
        self._line = line
        self._bucket = gcs_bucket

    def run(self, job: JobPayload) -> None:
        user = self._store.get_user(job.line_user_id)
        assert user is not None, "user must exist by the time worker runs"

        if job.mode == "new":
            self._run_new(user.line_user_id, user.environment_id, job.topic)
        elif job.mode == "deepen":
            self._run_deepen(user.line_user_id, user.environment_id, job.report_id, job.topic)
        else:
            raise NotImplementedError(f"mode {job.mode} not yet implemented")

    # ---------------- new research ----------------

    def _run_new(self, user_id: str, env_id: str, topic: str) -> None:
        # Stage 1: PLAN
        plan_resp = self._agents.interact(
            environment_id=env_id,
            instruction=f"PLAN\n\ntopic: {topic}",
            previous_interaction_id=None,
        )
        plan_json = json.loads(plan_resp.text)
        self._store.set_last_interaction(user_id, plan_resp.interaction_id)
        self._line.push_text(
            user_id=user_id,
            text=build_progress_text("plan_done", source_count=plan_json["source_count"]),
        )

        # Stage 2: SEARCH_COMPARE
        search_resp = self._agents.interact(
            environment_id=env_id,
            instruction="SEARCH_COMPARE",
            previous_interaction_id=plan_resp.interaction_id,
        )
        search_json = json.loads(search_resp.text)
        self._store.set_last_interaction(user_id, search_resp.interaction_id)
        self._line.push_text(
            user_id=user_id,
            text=build_progress_text(
                "search_done",
                source_count=search_json["source_count"],
                disagreement_count=search_json["disagreement_count"],
            ),
        )

        # Stage 3: WRITE_REPORT
        report_id = uuid.uuid4().hex
        write_resp = self._agents.interact(
            environment_id=env_id,
            instruction=(
                "WRITE_REPORT\n\n"
                f"report_id: {report_id}\n"
                "mode: new"
            ),
            previous_interaction_id=search_resp.interaction_id,
        )
        write_json = json.loads(write_resp.text)
        self._store.set_last_interaction(user_id, write_resp.interaction_id)

        gcs_url = self._public_url(report_id)
        report = self._store.create_report(
            user_id=user_id,
            topic=topic,
            summary=write_json["summary_500"],
            gcs_url=gcs_url,
        )
        # Overwrite Firestore-generated id is not possible; we use the Agent's report_id
        # by storing it as the canonical id. The Agent uploaded to gs://bucket/{report_id}/.
        # To keep them aligned, we delete the auto-generated record and re-create with our id.
        # (Simpler alternative: pass our id into create_report; see Task 11 refinement.)
        self._store.set_current_report(user_id, report.report_id)

        # Push final card
        card = build_report_card(
            topic=topic,
            summary=write_json["summary_500"],
            report_url=gcs_url,
            citations=write_json["top_citations"],
            version=1,
        )
        self._line.push_flex(user_id=user_id, flex_message=card)

    def _public_url(self, report_id: str) -> str:
        return f"https://storage.googleapis.com/{self._bucket}/{report_id}/index.html"

    def _run_deepen(self, user_id: str, env_id: str, report_id: str | None, instruction: str) -> None:
        raise NotImplementedError("see Task 11")
```

- [ ] **Step 4: Note: align report_id with Agent-side GCS path**

The simplest fix is to pass our `report_id` into the agent **and** into `create_report`. We modified `state.create_report` to generate its own id (uuid). To keep them in sync, refactor `create_report` to optionally accept a `report_id` parameter. Apply this small edit:

Modify `app/state.py` `create_report` signature:
```python
def create_report(
    self,
    *,
    user_id: str,
    topic: str,
    summary: str,
    gcs_url: str,
    report_id: str | None = None,
) -> ReportRecord:
    report_id = report_id or uuid.uuid4().hex
    ...
```

Then in `worker._run_new`, change to:
```python
report = self._store.create_report(
    user_id=user_id, topic=topic, summary=write_json["summary_500"],
    gcs_url=gcs_url, report_id=report_id,
)
```

- [ ] **Step 5: Run happy-path test**

Run: `pytest tests/unit/test_worker.py::test_new_research_happy_path -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add app/worker.py app/state.py tests/unit/test_worker.py
git commit -m "feat: worker happy path orchestrating PLAN→SEARCH→WRITE"
```

---

## Task 11: Worker — deepen + retry + sandbox expiry

**Files:**
- Modify: `app/worker.py`
- Modify: `tests/unit/test_worker.py`

- [ ] **Step 1: Add failing tests for deepen, sandbox expiry, retry**

Append to `tests/unit/test_worker.py`:
```python
def test_deepen_updates_same_report(worker: ResearchWorker) -> None:
    # Seed: existing report v1
    r = worker._store.create_report(
        user_id="U1", topic="x", summary="s1",
        gcs_url="https://storage.googleapis.com/line-reports/r1/index.html",
        report_id="r1",
    )
    worker._store.set_current_report("U1", "r1")

    worker._agents.interact.return_value = _ok(json.dumps({
        "report_id": "r1",
        "summary_500": "s2",
        "top_citations": [],
        "new_version": 2,
    }), "iD")

    job = JobPayload(
        line_user_id="U1", topic="第 2 章再深一點",
        mode="deepen", report_id="r1", task_id="t2",
    )
    worker.run(job)

    assert worker._agents.interact.call_count == 1
    # Single WRITE_REPORT call with mode=deepen
    inst = worker._agents.interact.call_args.kwargs["instruction"]
    assert "mode: deepen" in inst
    assert "previous_version: 1" in inst
    assert "第 2 章再深一點" in inst

    updated = worker._store.get_report("r1")
    assert updated.version == 2
    assert updated.summary == "s2"
    assert len(updated.history) == 1


def test_sandbox_expiry_recreates_and_falls_back_to_new(worker: ResearchWorker) -> None:
    # Save the current env id so we can prove it changed
    worker._store.create_report(user_id="U1", topic="t", summary="s",
                                gcs_url="x", report_id="r1")
    worker._store.set_current_report("U1", "r1")

    new_env = "env-B"
    worker._agents.create_environment.return_value = new_env

    # First interact raises EnvironmentNotFoundError (sandbox dead),
    # then the three new-research calls succeed.
    worker._agents.interact.side_effect = [
        EnvironmentNotFoundError("dead"),
        _ok(json.dumps({"topic": "t", "queries": [], "source_count": 0}), "i1"),
        _ok(json.dumps({"sources": [], "source_count": 0, "disagreement_count": 0,
                        "agreements": [], "disagreements": [], "gaps": []}), "i2"),
        _ok(json.dumps({"report_id": "r-new", "summary_500": "s",
                        "top_citations": [], "new_version": 1}), "i3"),
    ]

    job = JobPayload(line_user_id="U1", topic="第 2 章再深",
                     mode="deepen", report_id="r1", task_id="t3")
    worker.run(job)

    user = worker._store.get_user("U1")
    assert user.environment_id == new_env
    # User was warned
    push_msgs = [c.kwargs.get("text", "") for c in worker._line.push_text.call_args_list]
    assert any("過期" in m for m in push_msgs)


def test_retry_write_uses_existing_sources(worker: ResearchWorker) -> None:
    # Seed pending state: sources.json already in sandbox; we only re-run stage 3
    worker._store.set_pending_action("U1", "retry_write")
    # Need a topic; in retry mode worker should fetch the in-progress topic from somewhere.
    # Simplest: store last attempted topic on the user record (added in this task).
    worker._store._db.collection("users").document("U1").update({"last_attempt_topic": "x"})

    worker._agents.interact.return_value = _ok(json.dumps({
        "report_id": "r-new", "summary_500": "s",
        "top_citations": [], "new_version": 1,
    }), "iR")

    job = JobPayload(line_user_id="U1", topic="再試一次",
                     mode="retry_write", report_id=None, task_id="tR")
    worker.run(job)

    assert worker._agents.interact.call_count == 1
    inst = worker._agents.interact.call_args.kwargs["instruction"]
    assert "WRITE_REPORT" in inst
    user = worker._store.get_user("U1")
    assert user.pending_action is None
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pytest tests/unit/test_worker.py -v`
Expected: 3 new tests FAIL.

- [ ] **Step 3: Implement deepen, expiry recovery, retry_write in `app/worker.py`**

Replace `_run_deepen` placeholder and add helpers:

```python
def run(self, job: JobPayload) -> None:
    user = self._store.get_user(job.line_user_id)
    assert user is not None

    try:
        if job.mode == "new":
            self._store._db.collection("users").document(user.line_user_id).update(
                {"last_attempt_topic": job.topic}
            )
            self._run_new(user.line_user_id, user.environment_id, job.topic)
        elif job.mode == "deepen":
            self._run_deepen(user.line_user_id, user.environment_id,
                             job.report_id, job.topic)
        elif job.mode == "retry_write":
            self._run_retry_write(user.line_user_id, user.environment_id)
        elif job.mode == "retry_publish":
            self._run_retry_publish(user.line_user_id, user.environment_id)
        else:
            raise ValueError(f"unknown mode: {job.mode}")
    except EnvironmentNotFoundError:
        # Sandbox expired: recreate and (for deepen) fall back to NEW mode.
        new_env = self._agents.create_environment()
        self._store.set_environment(user.line_user_id, new_env)
        self._line.push_text(
            user_id=user.line_user_id,
            text="上次研究檔案已過期，重新從頭研究 🔄",
        )
        # In retry_publish there is no sources.json on a fresh sandbox either;
        # treat as new research using the original topic.
        topic = job.topic if job.mode == "new" else self._get_last_topic(user.line_user_id)
        self._run_new(user.line_user_id, new_env, topic)

def _get_last_topic(self, user_id: str) -> str:
    snap = self._store._db.collection("users").document(user_id).get()
    return (snap.to_dict() or {}).get("last_attempt_topic", "")

def _run_deepen(self, user_id: str, env_id: str, report_id: str | None,
                deepen_request: str) -> None:
    assert report_id is not None, "deepen requires an existing report_id"
    existing = self._store.get_report(report_id)
    assert existing is not None
    previous_version = existing.version

    user = self._store.get_user(user_id)
    write_resp = self._agents.interact(
        environment_id=env_id,
        instruction=(
            "WRITE_REPORT\n\n"
            f"report_id: {report_id}\n"
            "mode: deepen\n"
            f"previous_version: {previous_version}\n"
            f"deepen_request: {deepen_request}\n"
        ),
        previous_interaction_id=user.last_interaction_id,
    )
    write_json = json.loads(write_resp.text)
    self._store.set_last_interaction(user_id, write_resp.interaction_id)

    if write_json.get("error") == "chapter_not_found":
        chapters = "\n".join(write_json.get("available_chapters", []))
        self._line.push_text(
            user_id=user_id,
            text=f"找不到指定章節。目前報告章節如下：\n{chapters}",
        )
        return

    snapshot_url = (
        f"https://storage.googleapis.com/{self._bucket}/"
        f"{report_id}/snapshots/v{previous_version}.html"
    )
    self._store.deepen_report(
        report_id, new_summary=write_json["summary_500"],
        snapshot_url=snapshot_url,
    )

    card = build_report_card(
        topic=existing.topic,
        summary=write_json["summary_500"],
        report_url=existing.gcs_url,
        citations=write_json["top_citations"],
        version=write_json["new_version"],
    )
    self._line.push_flex(user_id=user_id, flex_message=card)

def _run_retry_write(self, user_id: str, env_id: str) -> None:
    user = self._store.get_user(user_id)
    topic = self._get_last_topic(user_id)
    report_id = uuid.uuid4().hex
    write_resp = self._agents.interact(
        environment_id=env_id,
        instruction=(
            "WRITE_REPORT\n\n"
            f"report_id: {report_id}\n"
            "mode: new\n"
        ),
        previous_interaction_id=user.last_interaction_id,
    )
    write_json = json.loads(write_resp.text)
    self._store.set_last_interaction(user_id, write_resp.interaction_id)
    self._store.set_pending_action(user_id, None)

    gcs_url = self._public_url(report_id)
    self._store.create_report(
        user_id=user_id, topic=topic, summary=write_json["summary_500"],
        gcs_url=gcs_url, report_id=report_id,
    )
    self._store.set_current_report(user_id, report_id)

    card = build_report_card(
        topic=topic, summary=write_json["summary_500"],
        report_url=gcs_url, citations=write_json["top_citations"],
        version=1,
    )
    self._line.push_flex(user_id=user_id, flex_message=card)

def _run_retry_publish(self, user_id: str, env_id: str) -> None:
    """Retry only the upload step. The sandbox already has report.html.
    We instruct the Agent to run only the publish commands and report success."""
    user = self._store.get_user(user_id)
    report = None
    if user.current_report_id:
        report = self._store.get_report(user.current_report_id)
    assert report is not None, "retry_publish requires an existing report"

    resp = self._agents.interact(
        environment_id=env_id,
        instruction=(
            "WRITE_REPORT\n\n"
            f"report_id: {report.report_id}\n"
            "mode: republish\n"
        ),
        previous_interaction_id=user.last_interaction_id,
    )
    data = json.loads(resp.text)
    if data.get("error") == "publish_failed":
        self._line.push_text(user_id=user_id, text="再次發佈失敗，請稍後重試。")
        return
    self._store.set_pending_action(user_id, None)
    self._line.push_text(
        user_id=user_id,
        text=f"✅ 已重新發佈：{report.gcs_url}",
    )
```

Note: extend `write_report.md` to handle `mode: republish` (run only the gsutil upload + curl verify steps, skip writing). Append to `app/system_instructions/write_report.md`:

```markdown
### If mode == "republish"
Skip rewriting. Run only steps 8 and 9 (upload + verify). Return:
```json
{"report_id": "<input>", "summary_500": "<reuse last>", "top_citations": [], "new_version": <unchanged>}
```
If verify fails, return `{"error": "publish_failed"}`.
```

- [ ] **Step 4: Run tests to verify all pass**

Run: `pytest tests/unit/test_worker.py -v`
Expected: 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add app/worker.py app/system_instructions/write_report.md tests/unit/test_worker.py
git commit -m "feat: worker deepen, retry_write, retry_publish, sandbox expiry recovery"
```

---

## Task 12: Worker — per-stage retry on transient failures

**Files:**
- Modify: `app/worker.py`
- Modify: `tests/unit/test_worker.py`

- [ ] **Step 1: Add failing tests for retry semantics**

Append to `tests/unit/test_worker.py`:
```python
from app.agents_client import AgentsAPIError


def test_search_compare_retries_once_then_succeeds(worker: ResearchWorker) -> None:
    worker._agents.interact.side_effect = [
        _ok(json.dumps({"topic": "x", "queries": [], "source_count": 0}), "i1"),
        AgentsAPIError("transient"),
        _ok(json.dumps({"sources": [], "source_count": 0, "disagreement_count": 0,
                        "agreements": [], "disagreements": [], "gaps": []}), "i2-retry"),
        _ok(json.dumps({"report_id": "r1", "summary_500": "s",
                        "top_citations": [], "new_version": 1}), "i3"),
    ]
    job = JobPayload(line_user_id="U1", topic="t", mode="new",
                     report_id=None, task_id="t1")
    worker.run(job)
    assert worker._agents.interact.call_count == 4


def test_write_report_fails_twice_sets_pending_action(worker: ResearchWorker) -> None:
    worker._agents.interact.side_effect = [
        _ok(json.dumps({"topic": "x", "queries": [], "source_count": 0}), "i1"),
        _ok(json.dumps({"sources": [], "source_count": 0, "disagreement_count": 0,
                        "agreements": [], "disagreements": [], "gaps": []}), "i2"),
        AgentsAPIError("fail-1"),
        AgentsAPIError("fail-2"),
    ]
    job = JobPayload(line_user_id="U1", topic="t", mode="new",
                     report_id=None, task_id="t1")
    worker.run(job)
    user = worker._store.get_user("U1")
    assert user.pending_action == "retry_write"
    push_msgs = [c.kwargs.get("text", "") for c in worker._line.push_text.call_args_list]
    assert any("再試一次" in m for m in push_msgs)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pytest tests/unit/test_worker.py -v -k retry`
Expected: 2 tests FAIL.

- [ ] **Step 3: Implement retry wrapper in `app/worker.py`**

Add at top of `worker.py`:
```python
from tenacity import retry, stop_after_attempt, wait_fixed, retry_if_exception_type
```

Add method:
```python
def _interact_with_retry(self, max_attempts: int, **kwargs) -> "AgentResponse":
    @retry(
        stop=stop_after_attempt(max_attempts),
        wait=wait_fixed(2),
        retry=retry_if_exception_type(AgentsAPIError),
        reraise=True,
    )
    def _call():
        return self._agents.interact(**kwargs)
    return _call()
```

Update each stage call:
- PLAN: 1 attempt only (no retry; surface failure immediately)
- SEARCH_COMPARE: `max_attempts=2`
- WRITE_REPORT: `max_attempts=2`; on final failure set pending_action and push

Concretely, in `_run_new` replace stages 2 and 3:
```python
# Stage 2
try:
    search_resp = self._interact_with_retry(
        max_attempts=2,
        environment_id=env_id,
        instruction="SEARCH_COMPARE",
        previous_interaction_id=plan_resp.interaction_id,
    )
except AgentsAPIError:
    self._line.push_text(user_id=user_id, text="搜尋比對失敗，請稍後再試。")
    return
search_json = json.loads(search_resp.text)
self._store.set_last_interaction(user_id, search_resp.interaction_id)
self._line.push_text(
    user_id=user_id,
    text=build_progress_text("search_done",
                             source_count=search_json["source_count"],
                             disagreement_count=search_json["disagreement_count"]),
)

# Stage 3
report_id = uuid.uuid4().hex
try:
    write_resp = self._interact_with_retry(
        max_attempts=2,
        environment_id=env_id,
        instruction=("WRITE_REPORT\n\n"
                     f"report_id: {report_id}\n"
                     "mode: new"),
        previous_interaction_id=search_resp.interaction_id,
    )
except AgentsAPIError:
    self._store.set_pending_action(user_id, "retry_write")
    self._line.push_text(
        user_id=user_id,
        text="資料已找齊，組稿失敗，回『再試一次』可再試。",
    )
    return
```

And in PLAN, on failure:
```python
try:
    plan_resp = self._agents.interact(...)
except AgentsAPIError:
    self._line.push_text(user_id=user_id, text="規劃失敗，請換個說法重試。")
    return
```

- [ ] **Step 4: Run tests to verify all pass**

Run: `pytest tests/unit/test_worker.py -v`
Expected: all worker tests PASS.

- [ ] **Step 5: Commit**

```bash
git add app/worker.py tests/unit/test_worker.py
git commit -m "feat: per-stage retry semantics with tenacity"
```

---

## Task 13: Webhook handler (`app/webhook.py`)

**Files:**
- Create: `app/webhook.py`
- Create: `tests/unit/test_webhook.py`

- [ ] **Step 1: Write failing tests**

`tests/unit/test_webhook.py`:
```python
import json
from unittest.mock import MagicMock
import pytest
from fastapi.testclient import TestClient
from google.cloud import firestore

from app.webhook import build_webhook_router
from app.state import StateStore


def _sign(secret: str, body: bytes) -> str:
    import base64, hashlib, hmac
    return base64.b64encode(
        hmac.new(secret.encode(), body, hashlib.sha256).digest()
    ).decode()


@pytest.fixture
def app_and_mocks(firestore_client: firestore.Client):
    from fastapi import FastAPI
    store = StateStore(firestore_client)
    line = MagicMock()
    line.verify_signature.side_effect = lambda body, sig: sig == _sign("secret", body)
    tasks = MagicMock()
    agents = MagicMock()
    agents.create_environment.return_value = "env-A"

    router = build_webhook_router(
        store=store, line=line, tasks=tasks, agents=agents,
    )
    app = FastAPI()
    app.include_router(router)
    return TestClient(app), store, line, tasks, agents


def test_webhook_bad_signature_returns_401(app_and_mocks) -> None:
    client, *_ = app_and_mocks
    r = client.post("/webhook",
                    json={"events": []},
                    headers={"X-Line-Signature": "bad"})
    assert r.status_code == 401


def test_webhook_valid_signature_acks_immediately(app_and_mocks) -> None:
    client, store, line, tasks, agents = app_and_mocks
    body = json.dumps({"events": [{
        "type": "message",
        "replyToken": "rep1",
        "source": {"type": "user", "userId": "U1"},
        "message": {"type": "text", "text": "研究 SOTA 向量資料庫"},
    }]}).encode()

    r = client.post("/webhook", content=body,
                    headers={"X-Line-Signature": _sign("secret", body),
                             "Content-Type": "application/json"})
    assert r.status_code == 200
    line.reply_text.assert_called_once()
    assert "已收到題目" in line.reply_text.call_args.kwargs["text"]
    tasks.enqueue.assert_called_once()


def test_webhook_non_text_message_replies_only(app_and_mocks) -> None:
    client, store, line, tasks, agents = app_and_mocks
    body = json.dumps({"events": [{
        "type": "message",
        "replyToken": "rep1",
        "source": {"type": "user", "userId": "U1"},
        "message": {"type": "sticker", "stickerId": "1"},
    }]}).encode()

    r = client.post("/webhook", content=body,
                    headers={"X-Line-Signature": _sign("secret", body),
                             "Content-Type": "application/json"})
    assert r.status_code == 200
    line.reply_text.assert_called_once()
    assert "只支援文字" in line.reply_text.call_args.kwargs["text"]
    tasks.enqueue.assert_not_called()


def test_webhook_deepen_routes_to_existing_report(app_and_mocks) -> None:
    client, store, line, tasks, agents = app_and_mocks
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.create_report(user_id="U1", topic="x", summary="s",
                        gcs_url="g", report_id="r1")
    store.set_current_report("U1", "r1")

    body = json.dumps({"events": [{
        "type": "message",
        "replyToken": "rep1",
        "source": {"type": "user", "userId": "U1"},
        "message": {"type": "text", "text": "第 2 章再深一點"},
    }]}).encode()

    r = client.post("/webhook", content=body,
                    headers={"X-Line-Signature": _sign("secret", body),
                             "Content-Type": "application/json"})
    assert r.status_code == 200
    job = tasks.enqueue.call_args.args[0]
    assert job.mode == "deepen"
    assert job.report_id == "r1"


def test_webhook_locked_user_replies_busy(app_and_mocks) -> None:
    import datetime as dt
    client, store, line, tasks, agents = app_and_mocks
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.acquire_lock("U1", task_id="t-prev", ttl_seconds=300)

    body = json.dumps({"events": [{
        "type": "message",
        "replyToken": "rep1",
        "source": {"type": "user", "userId": "U1"},
        "message": {"type": "text", "text": "新題目"},
    }]}).encode()

    r = client.post("/webhook", content=body,
                    headers={"X-Line-Signature": _sign("secret", body),
                             "Content-Type": "application/json"})
    assert r.status_code == 200
    msg = line.reply_text.call_args.kwargs["text"]
    assert "還在跑" in msg
    tasks.enqueue.assert_not_called()
```

`_sign` uses the literal secret string `"secret"`, so the `LineClient.verify_signature` mock in fixture must check against that. Done above via `side_effect`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `pytest tests/unit/test_webhook.py -v`
Expected: FAIL (module not found).

- [ ] **Step 3: Implement `app/webhook.py`**

```python
import json
import uuid
from fastapi import APIRouter, Request, HTTPException

from app.agents_client import AgentsClient
from app.intent import Intent, UserState, classify
from app.line_client import LineClient
from app.state import StateStore, LockTimeoutError
from app.tasks_client import TasksDispatcher, ResearchJob


def build_webhook_router(
    *,
    store: StateStore,
    line: LineClient,
    tasks: TasksDispatcher,
    agents: AgentsClient,
) -> APIRouter:
    router = APIRouter()

    @router.post("/webhook")
    async def webhook(request: Request) -> dict:
        body = await request.body()
        signature = request.headers.get("X-Line-Signature", "")
        if not line.verify_signature(body, signature):
            raise HTTPException(status_code=401, detail="bad signature")

        payload = json.loads(body)
        for event in payload.get("events", []):
            _handle_event(event, store=store, line=line, tasks=tasks, agents=agents)
        return {"ok": True}

    return router


def _handle_event(
    event: dict,
    *,
    store: StateStore,
    line: LineClient,
    tasks: TasksDispatcher,
    agents: AgentsClient,
) -> None:
    if event.get("type") != "message":
        return
    msg = event["message"]
    reply_token = event["replyToken"]
    user_id = event["source"]["userId"]

    if msg.get("type") != "text":
        line.reply_text(reply_token=reply_token,
                        text="目前只支援文字題目，請用文字描述要研究的主題。")
        return

    text = msg["text"]

    # Ensure user exists; create environment lazily on first contact.
    user = store.get_or_create_user(
        user_id, environment_factory=lambda: agents.create_environment()
    )

    intent = classify(
        text, UserState(current_report_id=user.current_report_id,
                        pending_action=user.pending_action),
    )

    if intent == Intent.CHITCHAT:
        line.reply_text(reply_token=reply_token,
                        text="哈囉！傳一個研究主題給我吧，例如「研究 SOTA 開源向量資料庫」。")
        return

    if intent == Intent.RECALL:
        if user.current_report_id:
            report = store.get_report(user.current_report_id)
            line.reply_text(reply_token=reply_token,
                            text=f"上次的報告連結：\n{report.gcs_url}")
        else:
            line.reply_text(reply_token=reply_token,
                            text="目前沒有進行中的研究。")
        return

    if intent == Intent.RESET:
        store.set_current_report(user_id, None)
        store.set_last_interaction(user_id, "")
        store.set_pending_action(user_id, None)
        line.reply_text(reply_token=reply_token,
                        text="✅ 已清空目前研究。請傳新題目。")
        return

    # NEW / DEEPEN / RETRY → all enqueue a background task. Acquire lock first.
    task_id = f"t-{uuid.uuid4().hex[:12]}"
    try:
        store.acquire_lock(user_id, task_id=task_id, ttl_seconds=300)
    except LockTimeoutError:
        line.reply_text(reply_token=reply_token,
                        text="上一份研究還在跑，請稍候幾分鐘後再傳新題目。")
        return

    mode_map = {
        Intent.NEW: "new",
        Intent.DEEPEN: "deepen",
        Intent.RETRY: user.pending_action or "retry_write",  # retry_write|retry_publish
    }
    mode = mode_map[intent]
    report_id = user.current_report_id if intent == Intent.DEEPEN else None

    line.reply_text(reply_token=reply_token, text="📋 已收到題目，開始規劃…")

    job = ResearchJob(
        line_user_id=user_id, topic=text, mode=mode, report_id=report_id,
    )
    tasks.enqueue(job, task_id=task_id)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pytest tests/unit/test_webhook.py -v`
Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add app/webhook.py tests/unit/test_webhook.py
git commit -m "feat: webhook handler with signature, intent routing, lock check"
```

---

## Task 14: FastAPI app + worker route + healthz (`app/main.py`)

**Files:**
- Create: `app/main.py`
- Modify: `tests/unit/test_worker.py` (add HTTP route test)

- [ ] **Step 1: Implement `app/main.py`**

```python
import os
from fastapi import FastAPI, Request

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
    creds, _ = google.auth.default(
        scopes=["https://www.googleapis.com/auth/cloud-platform"]
    )
    auth_req = google.auth.transport.requests.Request()

    def _provider() -> str:
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
    # Cloud Run SA email is auto-detected from metadata server in production.
    # For local dev, pass it via env.
    sa_email = os.environ.get(
        "SERVICE_ACCOUNT_EMAIL",
        f"{s.gcp_project_id}-compute@developer.gserviceaccount.com",
    )
    tasks = TasksDispatcher(
        project_id=s.gcp_project_id,
        location=s.cloud_tasks_location,
        queue=s.cloud_tasks_queue,
        target_url=f"{s.cloud_run_service_url}/tasks/run-research",
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
            worker.run(job)
        finally:
            store.release_lock(job.line_user_id)
        return {"ok": True}

    @app.get("/healthz")
    async def healthz() -> dict:
        return {"status": "ok"}

    return app


app = create_app()
```

- [ ] **Step 2: Add HTTP integration test for `/tasks/run-research`**

Add to `tests/unit/test_worker.py`:
```python
def test_run_research_endpoint_releases_lock(firestore_client) -> None:
    # End-to-end of /tasks/run-research: lock acquired by webhook is released here.
    from fastapi import FastAPI
    from fastapi.testclient import TestClient
    from app.worker import ResearchWorker, JobPayload

    store = StateStore(firestore_client)
    store.get_or_create_user("U1", environment_factory=lambda: "env-A")
    store.acquire_lock("U1", task_id="t1", ttl_seconds=300)

    agents = MagicMock()
    agents.interact.side_effect = [
        _ok(json.dumps({"topic": "x", "queries": [], "source_count": 0}), "i1"),
        _ok(json.dumps({"sources": [], "source_count": 0, "disagreement_count": 0,
                        "agreements": [], "disagreements": [], "gaps": []}), "i2"),
        _ok(json.dumps({"report_id": "r1", "summary_500": "s",
                        "top_citations": [], "new_version": 1}), "i3"),
    ]
    worker = ResearchWorker(store=store, agents=agents, line=MagicMock(),
                            gcs_bucket="line-reports")
    app = FastAPI()
    @app.post("/tasks/run-research")
    async def run(req):
        data = await req.json()
        job = JobPayload(**{k: data[k] for k in ["line_user_id", "topic", "mode",
                                                  "report_id", "task_id"]})
        try:
            worker.run(job)
        finally:
            store.release_lock(job.line_user_id)
        return {"ok": True}

    client = TestClient(app)
    r = client.post("/tasks/run-research", json={
        "line_user_id": "U1", "topic": "x", "mode": "new",
        "report_id": None, "task_id": "t1",
    })
    assert r.status_code == 200
    assert store.get_user("U1").lock is None
```

- [ ] **Step 3: Run all unit tests**

Run: `pytest tests/unit -v`
Expected: all tests PASS.

- [ ] **Step 4: Smoke run the app**

Run: `uvicorn app.main:app --port 8080`
Expected: starts without import errors. Then in another terminal:
```bash
curl http://localhost:8080/healthz
```
Expected: `{"status":"ok"}`.

Stop the server.

- [ ] **Step 5: Commit**

```bash
git add app/main.py tests/unit/test_worker.py
git commit -m "feat: FastAPI app wiring webhook + task worker + healthz"
```

---

## Task 15: Dockerfile

**Files:**
- Create: `Dockerfile`
- Create: `.dockerignore`

- [ ] **Step 1: Create `Dockerfile`**

```dockerfile
FROM python:3.11-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PORT=8080

WORKDIR /app

COPY pyproject.toml ./
RUN pip install --no-cache-dir -e .

COPY app/ ./app/

EXPOSE 8080

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8080"]
```

- [ ] **Step 2: Create `.dockerignore`**

```
.git
.venv
__pycache__
*.pyc
tests/
docs/
deploy/
.env
.env.local
```

- [ ] **Step 3: Build locally to verify**

Run: `docker build -t line-research-bot:dev .`
Expected: image builds without errors.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "chore: Dockerfile for Cloud Run"
```

---

## Task 16: GCP one-off setup scripts

**Files:**
- Create: `deploy/create-bucket.sh`
- Create: `deploy/create-tasks-queue.sh`
- Create: `deploy/create-agent.sh`
- Create: `deploy/deploy.sh`

- [ ] **Step 1: Create `deploy/create-bucket.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?set PROJECT_ID}"
BUCKET="${BUCKET:-line-reports}"
LOCATION="${LOCATION:-ASIA-EAST1}"

gcloud storage buckets create "gs://${BUCKET}" \
    --project="${PROJECT_ID}" \
    --location="${LOCATION}" \
    --uniform-bucket-level-access \
    --public-access-prevention=inherited || true

# Make all objects publicly readable
gcloud storage buckets add-iam-policy-binding "gs://${BUCKET}" \
    --member=allUsers --role=roles/storage.objectViewer

echo "✅ Bucket gs://${BUCKET} ready."
```

- [ ] **Step 2: Create `deploy/create-tasks-queue.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?set PROJECT_ID}"
LOCATION="${LOCATION:-asia-east1}"
QUEUE="${QUEUE:-research-jobs}"

gcloud tasks queues create "${QUEUE}" \
    --project="${PROJECT_ID}" \
    --location="${LOCATION}" \
    --max-attempts=3 \
    --min-backoff=10s \
    --max-backoff=120s || true

echo "✅ Cloud Tasks queue ${QUEUE} ready."
```

- [ ] **Step 3: Create `deploy/create-agent.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?set PROJECT_ID}"
AGENT_ID="${AGENT_ID:-research-planner}"
LOCATION="global"
SI_DIR="$(dirname "$0")/../app/system_instructions"

PLAN_SI="$(cat "${SI_DIR}/plan.md")"
SEARCH_SI="$(cat "${SI_DIR}/search_compare.md")"
WRITE_SI="$(cat "${SI_DIR}/write_report.md")"

# Combined system instruction — the agent receives the stage tag in each input
# and reads the matching section.
COMBINED=$(cat <<EOF
You are 研究規劃師. The first line of every user input is a stage tag (PLAN,
SEARCH_COMPARE, WRITE_REPORT). Read the section below that matches the tag and
follow it exactly.

---
${PLAN_SI}

---
${SEARCH_SI}

---
${WRITE_SI}
EOF
)

ACCESS_TOKEN="$(gcloud auth print-access-token)"

cat > /tmp/agent.json <<JSON
{
  "id": "${AGENT_ID}",
  "base_agent": "antigravity-preview-05-2026",
  "tools": [
    {"code_execution": {}},
    {"filesystem": {}},
    {"google_search": {}},
    {"url_context": {}}
  ],
  "network_allowlist": ["*"],
  "system_instruction": $(jq -Rs . <<<"${COMBINED}")
}
JSON

curl -sS -X POST \
    -H "Authorization: Bearer ${ACCESS_TOKEN}" \
    -H "Content-Type: application/json" \
    -d @/tmp/agent.json \
    "https://aiplatform.googleapis.com/v1beta1/projects/${PROJECT_ID}/locations/${LOCATION}/agents?agentId=${AGENT_ID}" \
  | jq .

rm -f /tmp/agent.json
echo "✅ Agent ${AGENT_ID} created (long-running op; check console)."
```

Note: the exact request body shape may differ from current Pre-GA docs; verify against the latest doc page before running.

- [ ] **Step 4: Create `deploy/deploy.sh`**

```bash
#!/usr/bin/env bash
set -euo pipefail

PROJECT_ID="${PROJECT_ID:?set PROJECT_ID}"
REGION="${REGION:-asia-east1}"
SERVICE="${SERVICE:-line-research-bot}"
SA_EMAIL="${SA_EMAIL:?set SA_EMAIL}"  # e.g. line-bot-sa@PROJECT.iam.gserviceaccount.com

# Required Secret Manager secrets must exist already:
#   - LINE_CHANNEL_SECRET
#   - LINE_CHANNEL_ACCESS_TOKEN

gcloud run deploy "${SERVICE}" \
    --project="${PROJECT_ID}" \
    --region="${REGION}" \
    --source=. \
    --service-account="${SA_EMAIL}" \
    --min-instances=1 \
    --max-instances=4 \
    --memory=1Gi \
    --cpu=1 \
    --concurrency=10 \
    --timeout=600 \
    --allow-unauthenticated \
    --set-env-vars="GCP_PROJECT_ID=${PROJECT_ID},GCP_LOCATION=global,AGENT_ID=research-planner,GCS_BUCKET=line-reports,CLOUD_TASKS_QUEUE=research-jobs,CLOUD_TASKS_LOCATION=${REGION},SERVICE_ACCOUNT_EMAIL=${SA_EMAIL}" \
    --update-secrets="LINE_CHANNEL_SECRET=LINE_CHANNEL_SECRET:latest,LINE_CHANNEL_ACCESS_TOKEN=LINE_CHANNEL_ACCESS_TOKEN:latest"

URL="$(gcloud run services describe "${SERVICE}" --region="${REGION}" --format='value(status.url)')"
echo "✅ Deployed: ${URL}"
echo "👉 Re-deploy with CLOUD_RUN_SERVICE_URL=${URL} so Cloud Tasks knows the target."
gcloud run services update "${SERVICE}" --region="${REGION}" \
    --update-env-vars="CLOUD_RUN_SERVICE_URL=${URL}"

echo "👉 LINE webhook URL: ${URL}/webhook"
```

- [ ] **Step 5: Make scripts executable + commit**

```bash
chmod +x deploy/*.sh
git add deploy
git commit -m "chore: GCP one-off setup and deploy scripts"
```

---

## Task 17: Manual GCP setup walkthrough (documented in repo)

**Files:**
- Create: `deploy/README.md`

- [ ] **Step 1: Write `deploy/README.md`**

````markdown
# Deploy

One-off GCP setup, then `deploy.sh` for every code change.

## Pre-reqs

```bash
export PROJECT_ID="your-gcp-project"
export REGION="asia-east1"
export SA_EMAIL="line-bot-sa@${PROJECT_ID}.iam.gserviceaccount.com"

gcloud config set project "${PROJECT_ID}"
gcloud auth login
gcloud auth application-default login
```

## 1. Enable APIs

```bash
gcloud services enable \
    aiplatform.googleapis.com \
    run.googleapis.com \
    cloudtasks.googleapis.com \
    firestore.googleapis.com \
    storage.googleapis.com \
    secretmanager.googleapis.com
```

## 2. Service account

```bash
gcloud iam service-accounts create line-bot-sa \
    --display-name="LINE Research Bot"

for role in aiplatform.user datastore.user cloudtasks.enqueuer \
            storage.objectAdmin secretmanager.secretAccessor \
            iam.serviceAccountTokenCreator run.invoker; do
  gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
      --member="serviceAccount:${SA_EMAIL}" \
      --role="roles/${role}"
done
```

## 3. Firestore (native mode)

```bash
gcloud firestore databases create --location="${REGION}"
```

## 4. Secrets

```bash
echo -n "<paste LINE channel secret>" | \
    gcloud secrets create LINE_CHANNEL_SECRET --data-file=-
echo -n "<paste LINE channel access token>" | \
    gcloud secrets create LINE_CHANNEL_ACCESS_TOKEN --data-file=-
```

## 5. Bucket + queue + agent

```bash
PROJECT_ID="${PROJECT_ID}" ./create-bucket.sh
PROJECT_ID="${PROJECT_ID}" LOCATION="${REGION}" ./create-tasks-queue.sh
PROJECT_ID="${PROJECT_ID}" ./create-agent.sh
```

## 6. Deploy

```bash
PROJECT_ID="${PROJECT_ID}" REGION="${REGION}" SA_EMAIL="${SA_EMAIL}" \
    ./deploy.sh
```

## 7. LINE console

In LINE Developers Console for your Messaging API channel:
- Webhook URL: `<Cloud Run URL>/webhook`
- Use webhook: ON
- Auto-reply messages: OFF (so our bot owns replies)
- Verify webhook → expect `200`.
````

- [ ] **Step 2: Commit**

```bash
git add deploy/README.md
git commit -m "docs: deploy walkthrough"
```

---

## Task 18: Integration test — Agents API filesystem persistence

**Files:**
- Create: `tests/integration/test_filesystem_persistence.py`

This proves the load-bearing assumption that `environment_id` persists files across interactions.

- [ ] **Step 1: Write the test**

```python
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
```

- [ ] **Step 2: Run integration test**

Run: `GCP_PROJECT_ID=<your-proj> AGENT_ID=research-planner make test-integration`
Expected: PASS. If it fails, the entire demo's premise is wrong — stop and revisit the spec.

- [ ] **Step 3: Commit**

```bash
git add tests/integration/test_filesystem_persistence.py
git commit -m "test: integration test for Agents API filesystem persistence"
```

---

## Task 19: Integration tests — real PLAN and WRITE_REPORT

**Files:**
- Create: `tests/integration/test_plan_real.py`
- Create: `tests/integration/test_write_report_real.py`

- [ ] **Step 1: Write `test_plan_real.py`**

```python
import json
import os
import pytest
from tests.integration.test_filesystem_persistence import client  # reuse fixture


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
```

- [ ] **Step 2: Write `test_write_report_real.py`**

```python
import json
import os
import uuid
import pytest
import httpx
from tests.integration.test_filesystem_persistence import client


@pytest.mark.skipif(
    not os.environ.get("RUN_FULL_WRITE_TEST"),
    reason="Set RUN_FULL_WRITE_TEST=1 (this test takes ~2 minutes and writes to GCS)",
)
def test_full_three_stage_writes_publicly_reachable_html(client) -> None:
    env_id = client.create_environment()
    report_id = uuid.uuid4().hex

    r1 = client.interact(
        environment_id=env_id,
        instruction="PLAN\n\ntopic: 開源向量資料庫選型 (測試)",
        previous_interaction_id=None,
    )
    r2 = client.interact(
        environment_id=env_id,
        instruction="SEARCH_COMPARE",
        previous_interaction_id=r1.interaction_id,
    )
    r3 = client.interact(
        environment_id=env_id,
        instruction=(
            "WRITE_REPORT\n\n"
            f"report_id: {report_id}\n"
            "mode: new"
        ),
        previous_interaction_id=r2.interaction_id,
    )
    out = json.loads(r3.text)
    assert out["report_id"] == report_id
    assert out["new_version"] == 1

    url = f"https://storage.googleapis.com/{os.environ['GCS_BUCKET']}/{report_id}/index.html"
    head = httpx.head(url, timeout=10.0)
    assert head.status_code == 200
    body = httpx.get(url, timeout=10.0).text
    assert "<html" in body.lower()
```

- [ ] **Step 3: Run both integration tests**

Run:
```bash
GCP_PROJECT_ID=<proj> GCS_BUCKET=line-reports make test-integration
# Optionally also:
RUN_FULL_WRITE_TEST=1 GCP_PROJECT_ID=<proj> GCS_BUCKET=line-reports \
    pytest tests/integration/test_write_report_real.py -v -s
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add tests/integration/test_plan_real.py tests/integration/test_write_report_real.py
git commit -m "test: integration tests for PLAN and full WRITE_REPORT cycles"
```

---

## Task 20: End-to-end manual test checklist (run after deploy)

**Files:**
- Create: `docs/e2e-checklist.md`

- [ ] **Step 1: Write the checklist**

```markdown
# E2E Checklist (run on LINE)

Before each Demo run:

1. Add the LINE bot as friend on the demo account.
2. Verify health: `curl <SERVICE_URL>/healthz` → `{"status":"ok"}`.

## Flow A — Happy path

- [ ] Send: `研究 SOTA 開源向量資料庫的選型`
- [ ] Within ~3 sec: see `📋 已收到題目，開始規劃…`
- [ ] Within ~30 sec: see `🔍 計畫完成：將比對 N 類來源`
- [ ] Within ~90 sec: see `📊 已比對 N 份來源，發現 M 處重要分歧`
- [ ] Within ~3 min: see Flex card with title, summary, "閱讀完整報告" button
- [ ] Tap button → opens GCS HTML → readable, styled, has v1 banner

## Flow B — Progressive deepening

- [ ] Send: `第 2 章再深一點，加日文來源`
- [ ] See new Flex card with `v2` badge
- [ ] Tap button → same URL → updated content; sources include Japanese
- [ ] Manually browse `gs://line-reports/<id>/snapshots/v1.html` → old content

## Flow C — Public link

- [ ] Open report URL in incognito browser → page loads without login

## Edge cases

- [ ] Send a sticker → reply `目前只支援文字題目`
- [ ] Send a new topic while previous still running → reply `上一份研究還在跑`
- [ ] Manually clear `environment_id` in Firestore → next message recreates sandbox and pushes `已過期，重新從頭研究 🔄`
- [ ] Send `重新開始` → reply `✅ 已清空目前研究`
- [ ] Send `連結` → reply with last report URL (or `目前沒有進行中的研究`)
```

- [ ] **Step 2: Commit**

```bash
git add docs/e2e-checklist.md
git commit -m "docs: end-to-end demo checklist"
```

---

## Done

The system is deployable and demo-ready. Next steps (out of scope for this plan):

- Promote from Pre-GA to GA once Agents API stabilises.
- Add LINE Login / LIFF for protected report links.
- Add multi-project support.
- Add image/audio input modalities.
- Switch from Cloud Tasks to Pub/Sub if higher throughput is needed.

---

## Plan Self-Review Notes

**Spec coverage:**
- §3 Use cases → Tasks 10–13 (worker + webhook)
- §4 Architecture → Tasks 14–16 (FastAPI wiring + Dockerfile + scripts)
- §5.1 Cloud Run directory → mapped 1:1 in Task 1
- §5.2 Agents config → Task 8 (system instructions) + Task 16 (`create-agent.sh`)
- §5.3 Firestore schema → Task 4 (StateStore) including `pending_action`
- §5.4 GCS bucket → Task 16 (`create-bucket.sh`)
- §5.5 Intent classifier → Task 3 (with `retry` intent)
- §6.1 Sandbox expiry → Task 11 (auto-recovery test)
- §6.2 Reply token → Task 13 (immediate reply, push afterwards)
- §6.3 Per-stage retry → Task 12 (tenacity-based)
- §6.4 Chapter not found → Task 11 (handled in `_run_deepen`)
- §6.5 Length limit → Task 5 (summary trim in Flex card)
- §6.6 Publish failure → Task 11 (`retry_publish` path)
- §6.7 Lock → Tasks 4 (DAO) + 13 (webhook) + 14 (release in `/tasks/run-research`)
- §6.8 Signature/non-text → Task 13
- §7 Testing → Tasks 18-20

**Placeholder scan:** none of "TBD / TODO / implement later". All code blocks present.

**Type consistency:**
- `JobPayload` fields used identically across worker, webhook, tasks_client, main.
- `Intent` enum used in classifier and webhook.
- `UserRecord.pending_action` typed identically to `PendingAction` in intent and state.
- `create_report(report_id=...)` signature change in Task 10 referenced again in Task 11.
