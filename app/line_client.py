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
