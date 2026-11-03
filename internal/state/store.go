// Package state is the Firestore data layer.
//
// Collection and field names are kept compatible with the original Python
// service (line_bot_users, line_bot_reports). The new line_bot_workspaces
// collection replaces the Managed Agents sandbox filesystem: it stores the
// research plan, cross-compared sources and the report Markdown, which are
// loaded into ADK session state before each agent run.
package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	usersColl      = "line_bot_users"
	reportsColl    = "line_bot_reports"
	workspacesColl = "line_bot_workspaces"
)

var ErrLocked = errors.New("user is locked by another task")

type User struct {
	LineUserID        string
	EnvironmentID     string
	CurrentReportID   string
	LastInteractionID string
	PendingAction     string
	LastAttemptTopic  string
}

type HistoryEntry struct {
	Version        int       `firestore:"version"`
	GCSSnapshotURL string    `firestore:"gcs_snapshot_url"`
	CreatedAt      time.Time `firestore:"created_at"`
}

type Report struct {
	ReportID  string         `firestore:"-"`
	UserID    string         `firestore:"user_id"`
	Topic     string         `firestore:"topic"`
	Summary   string         `firestore:"summary"`
	GCSURL    string         `firestore:"gcs_url"`
	Version   int            `firestore:"version"`
	History   []HistoryEntry `firestore:"history"`
	CreatedAt time.Time      `firestore:"created_at"`
	UpdatedAt time.Time      `firestore:"updated_at"`
}

// Workspace is the persistent agent working memory for one user.
// Values are JSON strings (Plan, Sources) or Markdown (ReportMD).
type Workspace struct {
	Topic     string    `firestore:"topic"`
	Plan      string    `firestore:"plan"`
	Sources   string    `firestore:"sources"`
	ReportMD  string    `firestore:"report_md"`
	UpdatedAt time.Time `firestore:"updated_at"`
}

type Store struct {
	db  *firestore.Client
	now func() time.Time
}

func New(db *firestore.Client) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Store) users() *firestore.CollectionRef { return s.db.Collection(usersColl) }

func str(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func userFrom(id string, d map[string]any) *User {
	return &User{
		LineUserID:        id,
		EnvironmentID:     str(d, "environment_id"),
		CurrentReportID:   str(d, "current_report_id"),
		LastInteractionID: str(d, "last_interaction_id"),
		PendingAction:     str(d, "pending_action"),
		LastAttemptTopic:  str(d, "last_attempt_topic"),
	}
}

// GetUser returns nil, nil when the user does not exist.
func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	snap, err := s.users().Doc(id).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return userFrom(id, snap.Data()), nil
}

func (s *Store) GetOrCreateUser(ctx context.Context, id string) (*User, error) {
	ref := s.users().Doc(id)
	var out *User
	err := s.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(ref)
		if err == nil {
			out = userFrom(id, snap.Data())
			return nil
		}
		if status.Code(err) != codes.NotFound {
			return err
		}
		doc := map[string]any{
			"environment_id":      "",
			"current_report_id":   nil,
			"last_interaction_id": nil,
			"last_active_at":      s.now(),
			"lock":                nil,
			"pending_action":      nil,
		}
		out = userFrom(id, doc)
		return tx.Set(ref, doc)
	})
	return out, err
}

func (s *Store) update(ctx context.Context, id string, ups ...firestore.Update) error {
	_, err := s.users().Doc(id).Update(ctx, ups)
	return err
}

func nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func (s *Store) SetCurrentReport(ctx context.Context, id, reportID string) error {
	return s.update(ctx, id,
		firestore.Update{Path: "current_report_id", Value: nullable(reportID)},
		firestore.Update{Path: "last_active_at", Value: s.now()})
}

func (s *Store) SetLastInteraction(ctx context.Context, id, interactionID string) error {
	return s.update(ctx, id,
		firestore.Update{Path: "last_interaction_id", Value: interactionID},
		firestore.Update{Path: "last_active_at", Value: s.now()})
}

func (s *Store) SetPendingAction(ctx context.Context, id, action string) error {
	return s.update(ctx, id, firestore.Update{Path: "pending_action", Value: nullable(action)})
}

func (s *Store) SetLastAttemptTopic(ctx context.Context, id, topic string) error {
	return s.update(ctx, id, firestore.Update{Path: "last_attempt_topic", Value: topic})
}

// AcquireLock takes the per-user lock or returns ErrLocked if another
// unexpired lock is held.
func (s *Store) AcquireLock(ctx context.Context, id, taskID string, ttl time.Duration) error {
	ref := s.users().Doc(id)
	return s.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		now := s.now()
		if lock, ok := snap.Data()["lock"].(map[string]any); ok {
			if until, ok := lock["lock_until"].(time.Time); ok && until.After(now) {
				return fmt.Errorf("%w: %v until %v", ErrLocked, lock["task_id"], until)
			}
		}
		return tx.Update(ref, []firestore.Update{{Path: "lock", Value: map[string]any{
			"task_id":    taskID,
			"lock_until": now.Add(ttl),
		}}})
	})
}

func (s *Store) ReleaseLock(ctx context.Context, id string) error {
	return s.update(ctx, id, firestore.Update{Path: "lock", Value: nil})
}

// ---------- reports ----------

func NewReportID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) CreateReport(ctx context.Context, reportID, userID, topic, summary, gcsURL string) (*Report, error) {
	now := s.now()
	r := &Report{
		ReportID: reportID, UserID: userID, Topic: topic, Summary: summary, GCSURL: gcsURL,
		Version: 1, History: []HistoryEntry{}, CreatedAt: now, UpdatedAt: now,
	}
	_, err := s.db.Collection(reportsColl).Doc(reportID).Set(ctx, r)
	return r, err
}

// GetReport returns nil, nil when not found.
func (s *Store) GetReport(ctx context.Context, reportID string) (*Report, error) {
	snap, err := s.db.Collection(reportsColl).Doc(reportID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Report
	if err := snap.DataTo(&r); err != nil {
		return nil, err
	}
	r.ReportID = reportID
	return &r, nil
}

// DeepenReport bumps the version, records the previous version snapshot and
// stores the new summary.
func (s *Store) DeepenReport(ctx context.Context, reportID, newSummary, snapshotURL string) (*Report, error) {
	ref := s.db.Collection(reportsColl).Doc(reportID)
	var out Report
	err := s.db.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(ref)
		if err != nil {
			return err
		}
		var r Report
		if err := snap.DataTo(&r); err != nil {
			return err
		}
		now := s.now()
		r.History = append(r.History, HistoryEntry{Version: r.Version, GCSSnapshotURL: snapshotURL, CreatedAt: now})
		r.Version++
		r.Summary = newSummary
		r.UpdatedAt = now
		r.ReportID = reportID
		out = r
		return tx.Update(ref, []firestore.Update{
			{Path: "version", Value: r.Version},
			{Path: "summary", Value: r.Summary},
			{Path: "history", Value: r.History},
			{Path: "updated_at", Value: now},
		})
	})
	return &out, err
}

// ---------- workspaces ----------

// GetWorkspace returns an empty workspace when none exists.
func (s *Store) GetWorkspace(ctx context.Context, userID string) (*Workspace, error) {
	snap, err := s.db.Collection(workspacesColl).Doc(userID).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return &Workspace{}, nil
	}
	if err != nil {
		return nil, err
	}
	var w Workspace
	return &w, snap.DataTo(&w)
}

func (s *Store) SaveWorkspace(ctx context.Context, userID string, w *Workspace) error {
	w.UpdatedAt = s.now()
	_, err := s.db.Collection(workspacesColl).Doc(userID).Set(ctx, w)
	return err
}

func (s *Store) ClearWorkspace(ctx context.Context, userID string) error {
	_, err := s.db.Collection(workspacesColl).Doc(userID).Delete(ctx)
	return err
}
