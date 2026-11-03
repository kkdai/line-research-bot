package linebot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"

	"github.com/kkdai/line-research-bot/internal/flex"
)

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"events":[]}`)
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	good := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if !VerifySignature("secret", body, good) {
		t.Fatal("valid signature rejected")
	}
	if VerifySignature("other", body, good) {
		t.Fatal("wrong secret accepted")
	}
	if VerifySignature("secret", body, "not-base64!!") {
		t.Fatal("garbage accepted")
	}
	if VerifySignature("secret", body, "") {
		t.Fatal("empty accepted")
	}
}

func TestReportCardIsValidFlexContainer(t *testing.T) {
	m := flex.ReportCard("主題", "摘要", "https://example.com/r", []flex.Citation{{Title: "a", URL: "https://a"}}, 1)
	raw, _ := json.Marshal(m.Contents)
	c, err := messaging_api.UnmarshalFlexContainer(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.(messaging_api.FlexBubble); !ok {
		t.Fatalf("got %T", c)
	}
}
