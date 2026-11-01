// Package worker orchestrates the research pipeline for one Cloud Tasks job:
// PLAN → SEARCH_COMPARE → WRITE_REPORT → publish, plus deepen and retries.
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kkdai/line-research-bot/internal/flex"
	"github.com/kkdai/line-research-bot/internal/research"
	"github.com/kkdai/line-research-bot/internal/state"
	"github.com/kkdai/line-research-bot/internal/tasks"
)

type Store interface {
	GetUser(ctx context.Context, id string) (*state.User, error)
	SetLastAttemptTopic(ctx context.Context, id, topic string) error
	SetLastInteraction(ctx context.Context, id, interactionID string) error
	SetPendingAction(ctx context.Context, id, action string) error
	SetCurrentReport(ctx context.Context, id, reportID string) error
	CreateReport(ctx context.Context, reportID, userID, topic, summary, gcsURL string) (*state.Report, error)
	GetReport(ctx context.Context, reportID string) (*state.Report, error)
	DeepenReport(ctx context.Context, reportID, newSummary, snapshotURL string) (*state.Report, error)
	GetWorkspace(ctx context.Context, userID string) (*state.Workspace, error)
	SaveWorkspace(ctx context.Context, userID string, w *state.Workspace) error
}

type Agents interface {
	Plan(ctx context.Context, userID, topic string) (*research.Output[research.PlanResult], error)
	SearchCompare(ctx context.Context, userID, planJSON string) (*research.Output[research.SearchResult], error)
	WriteReport(ctx context.Context, userID string, in research.WriteInput) (*research.Output[research.WriteResult], error)
}

type Line interface {
	PushText(userID, text string) error
	PushFlex(userID string, m flex.Message) error
}

type Publisher interface {
	Publish(ctx context.Context, reportID, topic, reportMD string, version, snapshotPrevious int) (string, error)
}

// Timeouts per agent attempt.
var (
	PlanTimeout   = 3 * time.Minute
	SearchTimeout = 8 * time.Minute
	WriteTimeout  = 6 * time.Minute
	RetryWait     = 2 * time.Second
)

const expiredText = "上次研究檔案已過期，重新從頭研究 🔄"

type Worker struct {
	Store     Store
	Agents    Agents
	Line      Line
	Publisher Publisher
	Bucket    string
	NewID     func() string
}

func New(s Store, a Agents, l Line, p Publisher, bucket string) *Worker {
	return &Worker{Store: s, Agents: a, Line: l, Publisher: p, Bucket: bucket, NewID: state.NewReportID}
}

// Run executes a job. User-facing failures (agent/publish) are reported to
// LINE and return nil; infrastructure errors are returned so Cloud Tasks
// can retry.
func (w *Worker) Run(ctx context.Context, job tasks.Job) error {
	user, err := w.Store.GetUser(ctx, job.LineUserID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("user %s not found", job.LineUserID)
	}
	switch job.Mode {
	case "new":
		return w.runNew(ctx, user.LineUserID, job.Topic)
	case "deepen":
		return w.runDeepen(ctx, user, job.ReportID, job.Topic)
	case "retry_write":
		return w.runRetryWrite(ctx, user)
	case "retry_publish":
		return w.runRetryPublish(ctx, user)
	}
	return fmt.Errorf("unknown mode: %q", job.Mode)
}

// push and pushCard only log LINE failures: returning an error would make
// Cloud Tasks re-run the whole (slow, billed) research pipeline.
func (w *Worker) push(userID, text string) {
	if err := w.Line.PushText(userID, text); err != nil {
		slog.Error("line push failed", "err", err)
	}
}

func (w *Worker) pushCard(userID string, m flex.Message) error {
	if err := w.Line.PushFlex(userID, m); err != nil {
		slog.Error("line push flex failed", "err", err)
	}
	return nil
}

func withRetry[T any](ctx context.Context, attempts int, timeout time.Duration, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	var err error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return zero, ctx.Err()
			case <-time.After(RetryWait):
			}
		}
		actx, cancel := context.WithTimeout(ctx, timeout)
		var v T
		v, err = fn(actx)
		cancel()
		if err == nil {
			return v, nil
		}
		slog.Warn("agent attempt failed", "attempt", i+1, "err", err)
		if ctx.Err() != nil {
			break
		}
	}
	return zero, err
}

// ---------------- new research ----------------

func (w *Worker) runNew(ctx context.Context, userID, topic string) error {
	if err := w.Store.SetLastAttemptTopic(ctx, userID, topic); err != nil {
		return err
	}

	// Stage 1: PLAN (single attempt; surface failure immediately)
	plan, err := withRetry(ctx, 1, PlanTimeout, func(c context.Context) (*research.Output[research.PlanResult], error) {
		return w.Agents.Plan(c, userID, topic)
	})
	if err != nil {
		slog.Error("plan failed", "err", err)
		w.push(userID, "規劃失敗，請換個說法重試。")
		return nil
	}
	ws := &state.Workspace{Topic: topic, Plan: plan.Raw}
	if err := w.Store.SaveWorkspace(ctx, userID, ws); err != nil {
		return err
	}
	_ = w.Store.SetLastInteraction(ctx, userID, plan.InvocationID)
	sourceCount := plan.Value.SourceCount
	if sourceCount == 0 {
		sourceCount = len(plan.Value.Queries)
	}
	w.push(userID, flex.ProgressText(flex.PlanDone, sourceCount, 0))

	// Stage 2: SEARCH_COMPARE (2 attempts)
	search, err := withRetry(ctx, 2, SearchTimeout, func(c context.Context) (*research.Output[research.SearchResult], error) {
		return w.Agents.SearchCompare(c, userID, ws.Plan)
	})
	if err != nil {
		slog.Error("search_compare failed", "err", err)
		w.push(userID, "搜尋比對失敗，請稍後再試。")
		return nil
	}
	ws.Sources = search.Raw
	if err := w.Store.SaveWorkspace(ctx, userID, ws); err != nil {
		return err
	}
	_ = w.Store.SetLastInteraction(ctx, userID, search.InvocationID)
	w.push(userID, flex.ProgressText(flex.SearchDone, search.Value.SourceCount, search.Value.DisagreementCount))

	// Stage 3: WRITE_REPORT (2 attempts) + publish
	return w.writeAndPublishNew(ctx, userID, topic, ws, true)
}

// writeAndPublishNew runs WRITE_REPORT in "new" mode and publishes v1.
// fromPipeline distinguishes the first run (sets pending actions on failure)
// from a "retry_write" job.
func (w *Worker) writeAndPublishNew(ctx context.Context, userID, topic string, ws *state.Workspace, fromPipeline bool) error {
	reportID := w.NewID()
	write, err := withRetry(ctx, 2, WriteTimeout, func(c context.Context) (*research.Output[research.WriteResult], error) {
		return w.Agents.WriteReport(c, userID, research.WriteInput{
			SourcesJSON: ws.Sources, ReportID: reportID, Mode: "new",
		})
	})
	if err != nil {
		slog.Error("write_report failed", "err", err)
		if err := w.Store.SetPendingAction(ctx, userID, "retry_write"); err != nil {
			return err
		}
		w.push(userID, "資料已找齊，組稿失敗，回『再試一次』可再試。")
		return nil
	}
	wr := write.Value
	_ = w.Store.SetLastInteraction(ctx, userID, write.InvocationID)
	ws.ReportMD = nonEmptyReport(wr.ReportMD, topic)
	if err := w.Store.SaveWorkspace(ctx, userID, ws); err != nil {
		return err
	}

	gcsURL, err := w.Publisher.Publish(ctx, reportID, topic, ws.ReportMD, 1, 0)
	if err != nil {
		slog.Error("publish failed", "err", err)
		if !fromPipeline {
			w.push(userID, "組稿完成但發佈失敗，請稍後重試。")
			return nil
		}
		if err := w.Store.SetPendingAction(ctx, userID, "retry_publish"); err != nil {
			return err
		}
		if _, err := w.Store.CreateReport(ctx, reportID, userID, topic, "(尚未發佈成功)", w.publicURL(reportID)); err != nil {
			return err
		}
		if err := w.Store.SetCurrentReport(ctx, userID, reportID); err != nil {
			return err
		}
		w.push(userID, "報告寫好但發佈失敗，回『再發佈一次』可再試。")
		return nil
	}

	if !fromPipeline {
		if err := w.Store.SetPendingAction(ctx, userID, ""); err != nil {
			return err
		}
	}
	if _, err := w.Store.CreateReport(ctx, reportID, userID, topic, wr.Summary500, gcsURL); err != nil {
		return err
	}
	if err := w.Store.SetCurrentReport(ctx, userID, reportID); err != nil {
		return err
	}
	return w.pushCard(userID, flex.ReportCard(topic, wr.Summary500, gcsURL, wr.TopCitations, 1))
}

func nonEmptyReport(md, topic string) string {
	if md == "" {
		return "# " + topic + "\n\n(報告內容缺失)"
	}
	return md
}

func (w *Worker) publicURL(reportID string) string {
	return fmt.Sprintf("https://storage.googleapis.com/%s/%s/index.html", w.Bucket, reportID)
}

// restartIfExpired handles a missing workspace (e.g. users migrated from the
// Python version whose files lived in the Managed Agents sandbox): tell the
// user and start the research over, mirroring the old sandbox-expiry path.
func (w *Worker) restartIfExpired(ctx context.Context, userID, topic string) error {
	w.push(userID, expiredText)
	return w.runNew(ctx, userID, topic)
}

// ---------------- deepen ----------------

func (w *Worker) runDeepen(ctx context.Context, user *state.User, reportID, request string) error {
	if reportID == "" {
		return errors.New("deepen requires report_id")
	}
	existing, err := w.Store.GetReport(ctx, reportID)
	if err != nil {
		return err
	}
	if existing == nil {
		return fmt.Errorf("report %s not found", reportID)
	}
	userID := user.LineUserID
	if err := w.Store.SetLastAttemptTopic(ctx, userID, existing.Topic); err != nil {
		return err
	}
	ws, err := w.Store.GetWorkspace(ctx, userID)
	if err != nil {
		return err
	}
	if ws.Sources == "" || ws.ReportMD == "" {
		return w.restartIfExpired(ctx, userID, existing.Topic)
	}
	prev := existing.Version

	write, err := withRetry(ctx, 1, WriteTimeout, func(c context.Context) (*research.Output[research.WriteResult], error) {
		return w.Agents.WriteReport(c, userID, research.WriteInput{
			SourcesJSON: ws.Sources, ReportMD: ws.ReportMD, ReportID: reportID,
			Mode: "deepen", PreviousVersion: prev, DeepenRequest: request,
		})
	})
	if err != nil {
		slog.Error("deepen write failed", "err", err)
		w.push(userID, "深化失敗，請稍後再試。")
		return nil
	}
	wr := write.Value
	_ = w.Store.SetLastInteraction(ctx, userID, write.InvocationID)

	if wr.Error == "chapter_not_found" {
		chapters := ""
		for i, c := range wr.AvailableChapters {
			if i > 0 {
				chapters += "\n"
			}
			chapters += c
		}
		w.push(userID, "找不到指定章節。目前報告章節如下：\n"+chapters)
		return nil
	}

	newVersion := prev + 1
	reportMD := nonEmptyReport(wr.ReportMD, existing.Topic)
	if _, err := w.Publisher.Publish(ctx, reportID, existing.Topic, reportMD, newVersion, prev); err != nil {
		slog.Error("deepen publish failed", "err", err)
		w.push(userID, "深化完成但發佈失敗，請稍後重試。")
		return nil
	}

	ws.ReportMD = reportMD
	if merged, err := research.MergeSources(ws.Sources, wr.AddedSources); err == nil {
		ws.Sources = merged
	} else {
		slog.Warn("merge added sources failed", "err", err)
	}
	if err := w.Store.SaveWorkspace(ctx, userID, ws); err != nil {
		return err
	}
	snapshotURL := fmt.Sprintf("https://storage.googleapis.com/%s/%s/snapshots/v%d.html", w.Bucket, reportID, prev)
	if _, err := w.Store.DeepenReport(ctx, reportID, wr.Summary500, snapshotURL); err != nil {
		return err
	}
	return w.pushCard(userID, flex.ReportCard(existing.Topic, wr.Summary500, existing.GCSURL, wr.TopCitations, newVersion))
}

// ---------------- retry_write ----------------

func (w *Worker) runRetryWrite(ctx context.Context, user *state.User) error {
	topic := user.LastAttemptTopic
	ws, err := w.Store.GetWorkspace(ctx, user.LineUserID)
	if err != nil {
		return err
	}
	if ws.Sources == "" {
		return w.restartIfExpired(ctx, user.LineUserID, topic)
	}
	return w.writeAndPublishNew(ctx, user.LineUserID, topic, ws, false)
}

// ---------------- retry_publish ----------------

// runRetryPublish re-publishes the report Markdown kept in the workspace.
// Unlike the sandbox-based version, no agent call is needed.
func (w *Worker) runRetryPublish(ctx context.Context, user *state.User) error {
	if user.CurrentReportID == "" {
		return errors.New("retry_publish requires a current report")
	}
	report, err := w.Store.GetReport(ctx, user.CurrentReportID)
	if err != nil {
		return err
	}
	if report == nil {
		return fmt.Errorf("report %s not found", user.CurrentReportID)
	}
	ws, err := w.Store.GetWorkspace(ctx, user.LineUserID)
	if err != nil {
		return err
	}
	if ws.ReportMD == "" {
		return w.restartIfExpired(ctx, user.LineUserID, report.Topic)
	}
	gcsURL, err := w.Publisher.Publish(ctx, report.ReportID, report.Topic, ws.ReportMD, report.Version, 0)
	if err != nil {
		slog.Error("republish failed", "err", err)
		w.push(user.LineUserID, "再次發佈失敗，請稍後重試。")
		return nil
	}
	if err := w.Store.SetPendingAction(ctx, user.LineUserID, ""); err != nil {
		return err
	}
	w.push(user.LineUserID, "✅ 已重新發佈："+gcsURL)
	return nil
}
