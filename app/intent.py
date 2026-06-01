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
