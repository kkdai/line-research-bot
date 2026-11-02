//go:build integration

package research

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/adk/model/gemini"
	"google.golang.org/genai"
)

// Runs the three stages against real Vertex AI.
//
//	GCP_PROJECT_ID=... go test -tags integration -v -run TestStages ./internal/research
func TestStages(t *testing.T) {
	project := os.Getenv("GCP_PROJECT_ID")
	if project == "" {
		t.Skip("GCP_PROJECT_ID not set")
	}
	modelName := os.Getenv("GEMINI_MODEL")
	if modelName == "" {
		modelName = "gemini-3.8-flash"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	llm, err := gemini.NewModel(ctx, modelName, &genai.ClientConfig{Backend: genai.BackendVertexAI, Project: project, Location: "global"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(llm, "research-planner")
	if err != nil {
		t.Fatal(err)
	}

	plan, err := a.Plan(ctx, "it-user", "研究 Go 語言 1.27 版的新功能")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("plan: %d queries, inv=%s", len(plan.Value.Queries), plan.InvocationID)
	if len(plan.Value.Queries) == 0 {
		t.Fatalf("empty plan: %s", plan.Raw)
	}

	search, err := a.SearchCompare(ctx, "it-user", plan.Raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("search: %d sources, %d disagreements", len(search.Value.Sources), search.Value.DisagreementCount)
	if len(search.Value.Sources) == 0 {
		t.Fatalf("no sources: %s", search.Raw)
	}

	write, err := a.WriteReport(ctx, "it-user", WriteInput{SourcesJSON: search.Raw, ReportID: "it-report", Mode: "new"})
	if err != nil {
		t.Fatal(err)
	}
	w := write.Value
	t.Logf("write: report_id=%s md=%d chars summary=%d chars citations=%d version=%d",
		w.ReportID, len(w.ReportMD), len([]rune(w.Summary500)), len(w.TopCitations), w.NewVersion)
	if len(w.ReportMD) < 200 || w.Summary500 == "" || w.NewVersion != 1 {
		t.Fatalf("bad write result: %+v", w)
	}

	deep, err := a.WriteReport(ctx, "it-user", WriteInput{
		SourcesJSON: search.Raw, ReportMD: w.ReportMD, ReportID: "it-report",
		Mode: "deepen", PreviousVersion: 1, DeepenRequest: "第 99 章再深一點",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("deepen ch99: error=%q chapters=%v", deep.Value.Error, deep.Value.AvailableChapters)
}
