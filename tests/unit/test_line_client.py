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
