// Package server exposes the HTTP endpoints:
//
//	POST /webhook             LINE webhook (HMAC-verified)
//	POST /tasks/run-research  Cloud Tasks target (OIDC-verified)
//	GET  /readyz              readiness probe
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kkdai/line-research-bot/internal/intent"
	"github.com/kkdai/line-research-bot/internal/state"
	"github.com/kkdai/line-research-bot/internal/tasks"
)

type Store interface {
	GetOrCreateUser(ctx context.Context, id string) (*state.User, error)
	GetReport(ctx context.Context, reportID string) (*state.Report, error)
	SetCurrentReport(ctx context.Context, id, reportID string) error
	SetLastInteraction(ctx context.Context, id, interactionID string) error
	SetPendingAction(ctx context.Context, id, action string) error
	AcquireLock(ctx context.Context, id, taskID string, ttl time.Duration) error
	ReleaseLock(ctx context.Context, id string) error
	ClearWorkspace(ctx context.Context, userID string) error
}

type Line interface {
	VerifySignature(body []byte, signature string) bool
	ReplyText(replyToken, text string) error
}

type Enqueuer interface {
	Enqueue(ctx context.Context, job tasks.Job) error
}

type Runner interface {
	Run(ctx context.Context, job tasks.Job) error
}

// TokenVerifier validates the Cloud Tasks OIDC bearer token.
type TokenVerifier func(ctx context.Context, bearer string) error

// LockTTL must outlive the Cloud Tasks dispatch deadline so a second job for
// the same user cannot start while the first is still running.
const LockTTL = tasks.DispatchDeadline + 5*time.Minute

const maxBody = 1 << 20

type Server struct {
	Store       Store
	Line        Line
	Tasks       Enqueuer
	Worker      Runner
	VerifyToken TokenVerifier // nil disables verification (local dev only)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", s.webhook)
	mux.HandleFunc("POST /tasks/run-research", s.runResearch)
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------- LINE webhook ----------

type webhookPayload struct {
	Events []event `json:"events"`
}

type event struct {
	Type       string `json:"type"`
	ReplyToken string `json:"replyToken"`
	Source     struct {
		UserID string `json:"userId"`
	} `json:"source"`
	Message struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"message"`
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if !s.Line.VerifySignature(body, r.Header.Get("X-Line-Signature")) {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	for _, ev := range p.Events {
		if err := s.handleEvent(r.Context(), ev); err != nil {
			// Swallow so we still return 200; LINE won't redeliver this batch.
			slog.Error("event handling failed", "type", ev.Type, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func newTaskID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "t-" + hex.EncodeToString(b)
}

func (s *Server) handleEvent(ctx context.Context, ev event) error {
	if ev.Type != "message" {
		return nil
	}
	userID := ev.Source.UserID
	if userID == "" {
		return errors.New("event without userId")
	}
	reply := func(text string) error { return s.Line.ReplyText(ev.ReplyToken, text) }

	if ev.Message.Type != "text" {
		return reply("目前只支援文字題目，請用文字描述要研究的主題。")
	}
	text := ev.Message.Text

	user, err := s.Store.GetOrCreateUser(ctx, userID)
	if err != nil {
		return err
	}
	in := intent.Classify(text, intent.UserState{CurrentReportID: user.CurrentReportID, PendingAction: user.PendingAction})

	switch in {
	case intent.Chitchat:
		return reply("哈囉！傳一個研究主題給我吧，例如「研究 SOTA 開源向量資料庫」。")
	case intent.Recall:
		if user.CurrentReportID != "" {
			report, err := s.Store.GetReport(ctx, user.CurrentReportID)
			if err != nil {
				return err
			}
			if report != nil {
				return reply("上次的報告連結：\n" + report.GCSURL)
			}
		}
		return reply("目前沒有進行中的研究。")
	case intent.Reset:
		if err := s.Store.SetCurrentReport(ctx, userID, ""); err != nil {
			return err
		}
		if err := s.Store.SetLastInteraction(ctx, userID, ""); err != nil {
			return err
		}
		if err := s.Store.SetPendingAction(ctx, userID, ""); err != nil {
			return err
		}
		if err := s.Store.ClearWorkspace(ctx, userID); err != nil {
			return err
		}
		return reply("✅ 已清空目前研究。請傳新題目。")
	}

	// NEW / DEEPEN / RETRY → background task. Acquire lock first.
	taskID := newTaskID()
	if err := s.Store.AcquireLock(ctx, userID, taskID, LockTTL); err != nil {
		if errors.Is(err, state.ErrLocked) {
			return reply("上一份研究還在跑，請稍候幾分鐘後再傳新題目。")
		}
		return err
	}

	job := tasks.Job{LineUserID: userID, Topic: text, TaskID: taskID}
	switch in {
	case intent.New:
		job.Mode = "new"
	case intent.Deepen:
		job.Mode = "deepen"
		job.ReportID = user.CurrentReportID
	case intent.Retry:
		job.Mode = user.PendingAction
		if job.Mode == "" {
			job.Mode = "retry_write"
		}
	}

	if err := s.Tasks.Enqueue(ctx, job); err != nil {
		// Roll back the lock so the user can retry.
		_ = s.Store.ReleaseLock(ctx, userID)
		slog.Error("tasks enqueue failed", "err", err)
		return reply("排程失敗，請稍後再試。")
	}
	return reply("📋 已收到題目，開始規劃…")
}

// ---------- Cloud Tasks target ----------

func (s *Server) runResearch(w http.ResponseWriter, r *http.Request) {
	if s.VerifyToken != nil {
		bearer, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || s.VerifyToken(r.Context(), bearer) != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}
	var job tasks.Job
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&job); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if job.LineUserID == "" || job.Mode == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), tasks.DispatchDeadline-time.Minute)
	defer cancel()
	defer func() {
		if err := s.Store.ReleaseLock(context.WithoutCancel(ctx), job.LineUserID); err != nil {
			slog.Error("release lock failed", "err", err)
		}
	}()

	slog.Info("job start", "task_id", job.TaskID, "mode", job.Mode)
	if err := s.Worker.Run(ctx, job); err != nil {
		slog.Error("job failed", "task_id", job.TaskID, "err", err)
		http.Error(w, "job failed", http.StatusInternalServerError)
		return
	}
	slog.Info("job done", "task_id", job.TaskID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
