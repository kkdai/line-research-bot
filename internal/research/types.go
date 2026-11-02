package research

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/kkdai/line-research-bot/internal/flex"
)

type Query struct {
	Q              string `json:"q"`
	SourceCategory string `json:"source_category"`
	Lang           string `json:"lang"`
}

type PlanResult struct {
	Topic       string  `json:"topic"`
	Queries     []Query `json:"queries"`
	SourceCount int     `json:"source_count"`
}

type Source struct {
	Title     string   `json:"title"`
	URL       string   `json:"url"`
	Lang      string   `json:"lang"`
	Summary   string   `json:"summary"`
	KeyClaims []string `json:"key_claims"`
}

type SearchResult struct {
	Sources           []Source          `json:"sources"`
	Agreements        []string          `json:"agreements"`
	Disagreements     []json.RawMessage `json:"disagreements"`
	Gaps              []string          `json:"gaps"`
	SourceCount       int               `json:"source_count"`
	DisagreementCount int               `json:"disagreement_count"`
}

type WriteResult struct {
	ReportID          string          `json:"report_id"`
	ReportMD          string          `json:"report_md"`
	Summary500        string          `json:"summary_500"`
	TopCitations      []flex.Citation `json:"top_citations"`
	NewVersion        int             `json:"new_version"`
	AddedSources      []Source        `json:"added_sources"`
	Error             string          `json:"error"`
	AvailableChapters []string        `json:"available_chapters"`
}

// ErrChapterNotFound is the WriteResult.Error value for a deepen request that
// names a chapter the report does not have.
const ErrChapterNotFound = "chapter_not_found"

// ChapterNotFound reports whether the model flagged a missing chapter. Models
// occasionally mangle the enum value (e.g. repeat it), so match loosely.
func (w WriteResult) ChapterNotFound() bool {
	return strings.Contains(w.Error, ErrChapterNotFound)
}

var headingRE = regexp.MustCompile(`(?m)^##\s+(.+?)\s*$`)

// Chapters lists the level-2 headings of a Markdown report.
func Chapters(reportMD string) []string {
	var out []string
	for _, m := range headingRE.FindAllStringSubmatch(reportMD, -1) {
		out = append(out, m[1])
	}
	return out
}

// Output wraps a parsed stage result with its raw JSON and the ADK
// invocation id (stored as last_interaction_id for traceability).
type Output[T any] struct {
	Value        T
	Raw          string
	InvocationID string
}

// ErrAgent marks a failure of the agent run itself (model/API error, empty
// or unparseable output) so callers can decide whether to retry.
var ErrAgent = errors.New("agent error")

var fenceRE = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(.*?)\\n```")

// ParseJSON does a best-effort JSON extraction from model output: the raw
// text, the contents of a fenced block, or the outermost {...} span.
func ParseJSON(text string, v any) error {
	t := strings.TrimSpace(text)
	candidates := []string{t}
	if m := fenceRE.FindStringSubmatch(t); m != nil {
		candidates = append(candidates, strings.TrimSpace(m[1]))
	}
	if i, j := strings.Index(t, "{"), strings.LastIndex(t, "}"); i >= 0 && j > i {
		candidates = append(candidates, t[i:j+1])
	}
	var lastErr error
	for _, c := range candidates {
		if lastErr = json.Unmarshal([]byte(c), v); lastErr == nil {
			return nil
		}
	}
	return fmt.Errorf("%w: output is not valid JSON: %v", ErrAgent, lastErr)
}

// MergeSources appends added sources to a sources JSON document and returns
// the updated JSON. Unknown fields are preserved.
func MergeSources(sourcesJSON string, added []Source) (string, error) {
	if len(added) == 0 {
		return sourcesJSON, nil
	}
	doc := map[string]any{}
	if strings.TrimSpace(sourcesJSON) != "" {
		if err := json.Unmarshal([]byte(sourcesJSON), &doc); err != nil {
			return "", err
		}
	}
	existing, _ := doc["sources"].([]any)
	for _, s := range added {
		existing = append(existing, s)
	}
	doc["sources"] = existing
	doc["source_count"] = len(existing)
	out, err := json.Marshal(doc)
	return string(out), err
}
