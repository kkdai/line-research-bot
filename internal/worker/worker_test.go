package worker

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kkdai/line-research-bot/internal/flex"
	"github.com/kkdai/line-research-bot/internal/research"
	"github.com/kkdai/line-research-bot/internal/state"
	"github.com/kkdai/line-research-bot/internal/tasks"
)

// ---------- fakes ----------

type fakeStore struct {
	users      map[string]*state.User
	reports    map[string]*state.Report
	workspaces map[string]*state.Workspace
}

func newStore() *fakeStore {
	return &fakeStore{
		users:      map[string]*state.User{"U1": {LineUserID: "U1"}},
		reports:    map[string]*state.Report{},
		workspaces: map[string]*state.Workspace{},
	}
}

func (s *fakeStore) GetUser(_ context.Context, id string) (*state.User, error) {
	u := s.users[id]
	if u == nil {
		return nil, nil
	}
	cp := *u
	return &cp, nil
}
func (s *fakeStore) SetLastAttemptTopic(_ context.Context, id, t string) error {
	s.users[id].LastAttemptTopic = t
	return nil
}
func (s *fakeStore) SetLastInteraction(_ context.Context, id, i string) error {
	s.users[id].LastInteractionID = i
	return nil
}
func (s *fakeStore) SetPendingAction(_ context.Context, id, a string) error {
	s.users[id].PendingAction = a
	return nil
}
func (s *fakeStore) SetCurrentReport(_ context.Context, id, r string) error {
	s.users[id].CurrentReportID = r
	return nil
}
func (s *fakeStore) CreateReport(_ context.Context, rid, uid, topic, summary, url string) (*state.Report, error) {
	r := &state.Report{ReportID: rid, UserID: uid, Topic: topic, Summary: summary, GCSURL: url, Version: 1}
	s.reports[rid] = r
	return r, nil
}
func (s *fakeStore) GetReport(_ context.Context, rid string) (*state.Report, error) {
	return s.reports[rid], nil
}
func (s *fakeStore) DeepenReport(_ context.Context, rid, summary, snap string) (*state.Report, error) {
	r := s.reports[rid]
	r.History = append(r.History, state.HistoryEntry{Version: r.Version, GCSSnapshotURL: snap})
	r.Version++
	r.Summary = summary
	return r, nil
}
func (s *fakeStore) GetWorkspace(_ context.Context, uid string) (*state.Workspace, error) {
	if w := s.workspaces[uid]; w != nil {
		cp := *w
		return &cp, nil
	}
	return &state.Workspace{}, nil
}
func (s *fakeStore) SaveWorkspace(_ context.Context, uid string, w *state.Workspace) error {
	cp := *w
	s.workspaces[uid] = &cp
	return nil
}

type fakeAgents struct {
	planErr, searchErr, writeErr error
	writeCalls                   int
	lastWrite                    research.WriteInput
	write                        research.WriteResult
}

func (a *fakeAgents) Plan(_ context.Context, _, topic string) (*research.Output[research.PlanResult], error) {
	if a.planErr != nil {
		return nil, a.planErr
	}
	return &research.Output[research.PlanResult]{Value: research.PlanResult{Topic: topic, SourceCount: 5}, Raw: `{"plan":1}`, InvocationID: "inv-plan"}, nil
}
func (a *fakeAgents) SearchCompare(_ context.Context, _, plan string) (*research.Output[research.SearchResult], error) {
	if a.searchErr != nil {
		return nil, a.searchErr
	}
	return &research.Output[research.SearchResult]{Value: research.SearchResult{SourceCount: 6, DisagreementCount: 2}, Raw: `{"sources":[]}`, InvocationID: "inv-search"}, nil
}
func (a *fakeAgents) WriteReport(_ context.Context, _ string, in research.WriteInput) (*research.Output[research.WriteResult], error) {
	a.writeCalls++
	a.lastWrite = in
	if a.writeErr != nil {
		return nil, a.writeErr
	}
	r := a.write
	if r.ReportMD == "" && r.Error == "" {
		r.ReportMD = "# report"
	}
	if r.Summary500 == "" {
		r.Summary500 = "summary"
	}
	return &research.Output[research.WriteResult]{Value: r, Raw: "{}", InvocationID: "inv-write"}, nil
}

type fakeLine struct {
	texts []string
	flex  []flex.Message
}

func (l *fakeLine) PushText(_, t string) error              { l.texts = append(l.texts, t); return nil }
func (l *fakeLine) PushFlex(_ string, m flex.Message) error { l.flex = append(l.flex, m); return nil }

type publishCall struct {
	reportID, md      string
	version, snapshot int
}

type fakePublisher struct {
	err   error
	calls []publishCall
}

func (p *fakePublisher) Publish(_ context.Context, rid, _, md string, v, snap int) (string, error) {
	p.calls = append(p.calls, publishCall{rid, md, v, snap})
	if p.err != nil {
		return "", p.err
	}
	return "https://storage.googleapis.com/b/" + rid + "/index.html", nil
}

func setup() (*Worker, *fakeStore, *fakeAgents, *fakeLine, *fakePublisher) {
	RetryWait = time.Millisecond
	s, a, l, p := newStore(), &fakeAgents{}, &fakeLine{}, &fakePublisher{}
	w := New(s, a, l, p, "b")
	w.NewID = func() string { return "rid" }
	return w, s, a, l, p
}

var errAgent = errors.New("boom")

// ---------- tests ----------

func TestNewHappyPath(t *testing.T) {
	w, s, _, l, p := setup()
	if err := w.Run(context.Background(), tasks.Job{LineUserID: "U1", Topic: "研究 X", Mode: "new"}); err != nil {
		t.Fatal(err)
	}
	if len(l.texts) != 2 || !strings.Contains(l.texts[0], "5 類來源") || !strings.Contains(l.texts[1], "6 份來源，發現 2") {
		t.Fatalf("progress texts: %v", l.texts)
	}
	if len(l.flex) != 1 || len(p.calls) != 1 || p.calls[0].version != 1 {
		t.Fatalf("flex=%d publish=%v", len(l.flex), p.calls)
	}
	u := s.users["U1"]
	if u.CurrentReportID != "rid" || u.LastInteractionID != "inv-write" || u.LastAttemptTopic != "研究 X" {
		t.Fatalf("user: %+v", u)
	}
	ws := s.workspaces["U1"]
	if ws.Plan == "" || ws.Sources == "" || ws.ReportMD != "# report" {
		t.Fatalf("workspace: %+v", ws)
	}
}

func TestPlanFailure(t *testing.T) {
	w, s, a, l, _ := setup()
	a.planErr = errAgent
	if err := w.Run(context.Background(), tasks.Job{LineUserID: "U1", Topic: "t", Mode: "new"}); err != nil {
		t.Fatal(err)
	}
	if len(l.texts) != 1 || l.texts[0] != "規劃失敗，請換個說法重試。" || s.users["U1"].CurrentReportID != "" {
		t.Fatalf("%v", l.texts)
	}
}

func TestWriteFailureSetsRetryWrite(t *testing.T) {
	w, s, a, l, _ := setup()
	a.writeErr = errAgent
	_ = w.Run(context.Background(), tasks.Job{LineUserID: "U1", Topic: "t", Mode: "new"})
	if a.writeCalls != 2 {
		t.Fatalf("write should be attempted twice, got %d", a.writeCalls)
	}
	if s.users["U1"].PendingAction != "retry_write" || !strings.Contains(l.texts[len(l.texts)-1], "再試一次") {
		t.Fatalf("pending=%q texts=%v", s.users["U1"].PendingAction, l.texts)
	}

	// retry_write succeeds and clears pending action
	a.writeErr = nil
	if err := w.Run(context.Background(), tasks.Job{LineUserID: "U1", Mode: "retry_write"}); err != nil {
		t.Fatal(err)
	}
	if s.users["U1"].PendingAction != "" || s.users["U1"].CurrentReportID != "rid" || len(l.flex) != 1 {
		t.Fatalf("after retry: %+v", s.users["U1"])
	}
}

func TestPublishFailureThenRetryPublish(t *testing.T) {
	w, s, _, l, p := setup()
	p.err = errAgent
	_ = w.Run(context.Background(), tasks.Job{LineUserID: "U1", Topic: "t", Mode: "new"})
	if s.users["U1"].PendingAction != "retry_publish" || s.reports["rid"].Summary != "(尚未發佈成功)" {
		t.Fatalf("state: %+v", s.users["U1"])
	}

	p.err = nil
	if err := w.Run(context.Background(), tasks.Job{LineUserID: "U1", Mode: "retry_publish"}); err != nil {
		t.Fatal(err)
	}
	last := p.calls[len(p.calls)-1]
	if last.md != "# report" || s.users["U1"].PendingAction != "" || !strings.HasPrefix(l.texts[len(l.texts)-1], "✅ 已重新發佈") {
		t.Fatalf("last=%+v texts=%v", last, l.texts)
	}
}

func seedReport(s *fakeStore) {
	s.reports["rid"] = &state.Report{ReportID: "rid", UserID: "U1", Topic: "topic", GCSURL: "https://x/rid/index.html", Version: 1}
	s.users["U1"].CurrentReportID = "rid"
	s.workspaces["U1"] = &state.Workspace{Topic: "topic", Plan: "{}", Sources: `{"sources":[]}`, ReportMD: "# v1"}
}

func TestDeepen(t *testing.T) {
	w, s, a, l, p := setup()
	seedReport(s)
	a.write = research.WriteResult{ReportMD: "# v2", Summary500: "new sum", AddedSources: []research.Source{{Title: "jp", URL: "https://jp"}}}
	if err := w.Run(context.Background(), tasks.Job{LineUserID: "U1", Topic: "第 2 章再深一點", Mode: "deepen", ReportID: "rid"}); err != nil {
		t.Fatal(err)
	}
	if a.lastWrite.Mode != "deepen" || a.lastWrite.ReportMD != "# v1" || a.lastWrite.PreviousVersion != 1 {
		t.Fatalf("write input: %+v", a.lastWrite)
	}
	if c := p.calls[0]; c.version != 2 || c.snapshot != 1 {
		t.Fatalf("publish: %+v", c)
	}
	r := s.reports["rid"]
	if r.Version != 2 || r.Summary != "new sum" || r.History[0].GCSSnapshotURL != "https://storage.googleapis.com/b/rid/snapshots/v1.html" {
		t.Fatalf("report: %+v", r)
	}
	ws := s.workspaces["U1"]
	if ws.ReportMD != "# v2" || !strings.Contains(ws.Sources, "https://jp") {
		t.Fatalf("workspace: %+v", ws)
	}
	if len(l.flex) != 1 {
		t.Fatal("expected flex card")
	}
}

func TestDeepenChapterNotFound(t *testing.T) {
	w, s, a, l, p := setup()
	seedReport(s)
	a.write = research.WriteResult{Error: "chapter_not_found", AvailableChapters: []string{"1. A", "2. B"}}
	_ = w.Run(context.Background(), tasks.Job{LineUserID: "U1", Topic: "第 9 章", Mode: "deepen", ReportID: "rid"})
	if len(p.calls) != 0 || l.texts[0] != "找不到指定章節。目前報告章節如下：\n1. A\n2. B" {
		t.Fatalf("texts=%v publish=%v", l.texts, p.calls)
	}
	if s.reports["rid"].Version != 1 {
		t.Fatal("version should not change")
	}
}

func TestDeepenWithoutWorkspaceRestarts(t *testing.T) {
	w, s, _, l, _ := setup()
	seedReport(s)
	delete(s.workspaces, "U1") // e.g. user migrated from the sandbox-based version
	if err := w.Run(context.Background(), tasks.Job{LineUserID: "U1", Topic: "第 2 章", Mode: "deepen", ReportID: "rid"}); err != nil {
		t.Fatal(err)
	}
	if l.texts[0] != expiredText || len(l.flex) != 1 || s.users["U1"].LastAttemptTopic != "topic" {
		t.Fatalf("texts=%v", l.texts)
	}
}

func TestUnknownUser(t *testing.T) {
	w, _, _, _, _ := setup()
	if err := w.Run(context.Background(), tasks.Job{LineUserID: "nobody", Mode: "new"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeepenChapterNotFoundMangledFallsBackToHeadings(t *testing.T) {
	w, s, a, l, p := setup()
	seedReport(s)
	s.workspaces["U1"].ReportMD = "# T\n\n## 第一章 A\n\ntext\n\n## 第二章 B\n"
	a.write = research.WriteResult{Error: "chapter_not_found_chapter_not_found"}
	_ = w.Run(context.Background(), tasks.Job{LineUserID: "U1", Topic: "第 9 章", Mode: "deepen", ReportID: "rid"})
	if len(p.calls) != 0 || l.texts[0] != "找不到指定章節。目前報告章節如下：\n第一章 A\n第二章 B" {
		t.Fatalf("texts=%v publish=%v", l.texts, p.calls)
	}
}

func TestDeepenEmptyReportNotPublished(t *testing.T) {
	w, s, a, l, p := setup()
	seedReport(s)
	a.write = research.WriteResult{Error: "something_else"}
	a.write.ReportMD = "   "
	_ = w.Run(context.Background(), tasks.Job{LineUserID: "U1", Topic: "第 1 章", Mode: "deepen", ReportID: "rid"})
	if len(p.calls) != 0 || l.texts[0] != "深化失敗，請稍後再試。" {
		t.Fatalf("texts=%v publish=%v", l.texts, p.calls)
	}
}
