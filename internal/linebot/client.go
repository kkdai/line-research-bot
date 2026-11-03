// Package linebot wraps the LINE Messaging API SDK.
package linebot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/line/line-bot-sdk-go/v8/linebot/messaging_api"

	"github.com/kkdai/line-research-bot/internal/flex"
)

type Client struct {
	secret string
	api    *messaging_api.MessagingApiAPI
}

func New(accessToken, channelSecret string) (*Client, error) {
	api, err := messaging_api.NewMessagingApiAPI(accessToken)
	if err != nil {
		return nil, err
	}
	return &Client{secret: channelSecret, api: api}, nil
}

// VerifySignature checks the X-Line-Signature header (HMAC-SHA256, base64).
func VerifySignature(channelSecret string, body []byte, signature string) bool {
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(channelSecret))
	mac.Write(body)
	return hmac.Equal(mac.Sum(nil), sig)
}

func (c *Client) VerifySignature(body []byte, signature string) bool {
	return VerifySignature(c.secret, body, signature)
}

func (c *Client) ReplyText(replyToken, text string) error {
	_, err := c.api.ReplyMessage(&messaging_api.ReplyMessageRequest{
		ReplyToken: replyToken,
		Messages:   []messaging_api.MessageInterface{messaging_api.TextMessage{Text: text}},
	})
	return err
}

func (c *Client) PushText(userID, text string) error {
	_, err := c.api.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{messaging_api.TextMessage{Text: text}},
	}, "")
	return err
}

func (c *Client) PushFlex(userID string, m flex.Message) error {
	raw, err := json.Marshal(m.Contents)
	if err != nil {
		return err
	}
	container, err := messaging_api.UnmarshalFlexContainer(raw)
	if err != nil {
		return fmt.Errorf("build flex container: %w", err)
	}
	_, err = c.api.PushMessage(&messaging_api.PushMessageRequest{
		To:       userID,
		Messages: []messaging_api.MessageInterface{messaging_api.FlexMessage{AltText: m.AltText, Contents: container}},
	}, "")
	return err
}
