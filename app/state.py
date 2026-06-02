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
        ref = self._db.collection("users").document(line_user_id)

        # NOTE: If the transaction retries due to contention, environment_factory()
        # may be called more than once. This is acceptable for our demo: it only
        # happens on a user's very first message and contention there is rare.
        @firestore.transactional
        def _txn(txn: firestore.Transaction) -> tuple[bool, dict]:
            """Returns (created, doc_data)."""
            snap = ref.get(transaction=txn)
            if snap.exists:
                return False, (snap.to_dict() or {})
            env_id = environment_factory()
            doc = {
                "environment_id": env_id,
                "current_report_id": None,
                "last_interaction_id": None,
                "last_active_at": _now(),
                "lock": None,
                "pending_action": None,
            }
            txn.set(ref, doc)
            return True, doc

        _, d = _txn(self._db.transaction())
        return UserRecord(
            line_user_id=line_user_id,
            environment_id=d["environment_id"],
            current_report_id=d.get("current_report_id"),
            last_interaction_id=d.get("last_interaction_id"),
            last_active_at=d.get("last_active_at", _now()),
            lock=d.get("lock"),
            pending_action=d.get("pending_action"),
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

    # ---------- last_attempt_topic ----------

    def set_last_attempt_topic(self, line_user_id: str, topic: str) -> None:
        self._db.collection("users").document(line_user_id).update(
            {"last_attempt_topic": topic}
        )

    def get_last_attempt_topic(self, line_user_id: str) -> str:
        snap = self._db.collection("users").document(line_user_id).get()
        return (snap.to_dict() or {}).get("last_attempt_topic", "")

    # ---------- reports ----------

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
