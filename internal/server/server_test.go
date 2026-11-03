package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kkdai/line-research-bot/internal/state"
	"github.com/kkdai/line-research-bot/internal/tasks"
)

type fakeStore struct {
	user          state.User
	report        *state.Report
	locked        bool
	released      bool
	workspaceGone bool
}

func (s *fakeStore) GetOrCreateUser(_ context.Context, id string) (*state.User, error) {
	u := s.user
	u.LineUserID = id
	return &u, nil
}
func (s *fakeStore) GetReport(context.Context, string) (*state.Report, error) { return s.report, nil }
func (s *fakeStore) SetCurrentReport(_ context.Context, _, r string) error {
	s.user.CurrentReportID = r
	return nil
}
func (s *fakeStore) SetLastInteraction(context.Context, string, string) error { return nil }
func (s *fakeStore) SetPendingAction(_ context.Context, _, a string) error {
	s.user.PendingAction = a
	return nil
}
func (s *fakeStore) AcquireLock(context.Context, string, string, time.Duration) error {
	if s.locked {
		return state.ErrLocked
	}
	s.locked = true
	return nil
}
func (s *fakeStore) ReleaseLock(context.Context, string) error {
	s.locked, s.released = false, true
	return nil
}
func (s *fakeStore) ClearWorkspace(context.Context, string) error { s.workspaceGone = true; return nil }

type fakeLine struct{ replies []string }

func (l *fakeLine) VerifySignature(_ []byte, sig string) bool { return sig == "ok" }
func (l *fakeLine) ReplyText(_, t string) error               { l.replies = append(l.replies, t); return nil }

type fakeTasks struct {
	jobs []tasks.Job
	err  error
}

func (t *fakeTasks) Enqueue(_ context.Context, j tasks.Job) error {
	if t.err != nil {
		return t.err
	}
	t.jobs = append(t.jobs, j)
	return nil
}

type fakeWorker struct {
	err error
	got []tasks.Job
}

func (w *fakeWorker) Run(_ context.Context, j tasks.Job) error {
	w.got = append(w.got, j)
	return w.err
}

func msg(text string) string {
	return `{"events":[{"type":"message","replyToken":"rt","source":{"userId":"U1"},"message":{"type":"text","text":"` + text + `"}}]}`
}

func post(h http.Handler, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newServer() (*Server, *fakeStore, *fakeLine, *fakeTasks, *fakeWorker) {
	s, l, t, w := &fakeStore{}, &fakeLine{}, &fakeTasks{}, &fakeWorker{}
	return &Server{Store: s, Line: l, Tasks: t, Worker: w}, s, l, t, w
}

var sig = map[string]string{"X-Line-Signature": "ok"}

func TestWebhookBadSignature(t *testing.T) {
	srv, _, _, _, _ := newServer()
	if rec := post(srv.Handler(), "/webhook", msg("研究 X"), map[string]string{"X-Line-Signature": "nope"}); rec.Code != 401 {
		t.Fatal(rec.Code)
	}
}

func TestWebhookNewEnqueues(t *testing.T) {
	srv, st, l, tk, _ := newServer()
	rec := post(srv.Handler(), "/webhook", msg("研究 向量資料庫"), sig)
	if rec.Code != 200 || len(tk.jobs) != 1 || tk.jobs[0].Mode != "new" || tk.jobs[0].Topic != "研究 向量資料庫" {
		t.Fatalf("code=%d jobs=%+v", rec.Code, tk.jobs)
	}
	if !st.locked || l.replies[0] != "📋 已收到題目，開始規劃…" {
		t.Fatalf("locked=%v replies=%v", st.locked, l.replies)
	}
}

func TestWebhookDeepenAndRetry(t *testing.T) {
	srv, st, _, tk, _ := newServer()
	st.user.CurrentReportID = "r1"
	post(srv.Handler(), "/webhook", msg("第 2 章再深一點"), sig)
	if tk.jobs[0].Mode != "deepen" || tk.jobs[0].ReportID != "r1" {
		t.Fatalf("%+v", tk.jobs[0])
	}
	st.locked = false
	st.user.PendingAction = "retry_publish"
	post(srv.Handler(), "/webhook", msg("再發佈一次"), sig)
	if tk.jobs[1].Mode != "retry_publish" {
		t.Fatalf("%+v", tk.jobs[1])
	}
}

func TestWebhookLocked(t *testing.T) {
	srv, st, l, tk, _ := newServer()
	st.locked = true
	post(srv.Handler(), "/webhook", msg("研究 X"), sig)
	if len(tk.jobs) != 0 || !strings.Contains(l.replies[0], "還在跑") {
		t.Fatalf("%v", l.replies)
	}
}

func TestWebhookEnqueueFailureReleasesLock(t *testing.T) {
	srv, st, l, tk, _ := newServer()
	tk.err = errors.New("down")
	post(srv.Handler(), "/webhook", msg("研究 X"), sig)
	if st.locked || l.replies[0] != "排程失敗，請稍後再試。" {
		t.Fatalf("locked=%v %v", st.locked, l.replies)
	}
}

func TestWebhookRecallResetChitchatNonText(t *testing.T) {
	srv, st, l, _, _ := newServer()
	st.user.CurrentReportID = "r1"
	st.report = &state.Report{GCSURL: "https://x/r1/index.html"}
	post(srv.Handler(), "/webhook", msg("上次的連結"), sig)
	post(srv.Handler(), "/webhook", msg("hi"), sig)
	post(srv.Handler(), "/webhook", msg("重新開始"), sig)
	post(srv.Handler(), "/webhook", `{"events":[{"type":"message","replyToken":"rt","source":{"userId":"U1"},"message":{"type":"image"}}]}`, sig)
	want := []string{"上次的報告連結：\nhttps://x/r1/index.html", "哈囉", "✅ 已清空", "目前只支援文字"}
	for i, w := range want {
		if !strings.HasPrefix(l.replies[i], w) {
			t.Fatalf("reply %d = %q, want prefix %q", i, l.replies[i], w)
		}
	}
	if st.user.CurrentReportID != "" || !st.workspaceGone {
		t.Fatal("reset should clear report and workspace")
	}
}

func TestRunResearchAuthAndLock(t *testing.T) {
	srv, st, _, _, wk := newServer()
	srv.VerifyToken = func(_ context.Context, tok string) error {
		if tok != "good" {
			return errors.New("bad")
		}
		return nil
	}
	body := `{"line_user_id":"U1","topic":"t","mode":"new","task_id":"t-1"}`
	if rec := post(srv.Handler(), "/tasks/run-research", body, nil); rec.Code != 401 {
		t.Fatalf("no token: %d", rec.Code)
	}
	if rec := post(srv.Handler(), "/tasks/run-research", body, map[string]string{"Authorization": "Bearer bad"}); rec.Code != 401 {
		t.Fatalf("bad token: %d", rec.Code)
	}
	rec := post(srv.Handler(), "/tasks/run-research", body, map[string]string{"Authorization": "Bearer good"})
	if rec.Code != 200 || len(wk.got) != 1 || !st.released {
		t.Fatalf("code=%d got=%v released=%v", rec.Code, wk.got, st.released)
	}

	wk.err = errors.New("firestore down")
	st.released = false
	if rec := post(srv.Handler(), "/tasks/run-research", body, map[string]string{"Authorization": "Bearer good"}); rec.Code != 500 || !st.released {
		t.Fatalf("worker error: code=%d released=%v", rec.Code, st.released)
	}
}

func TestReadyz(t *testing.T) {
	srv, _, _, _, _ := newServer()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
}
