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
