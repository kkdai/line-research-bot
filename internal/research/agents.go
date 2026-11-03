// Package research implements the three research stages as ADK for Go agents.
//
// The original Python service relied on the Pre-GA Managed Agents API and its
// sandbox filesystem (/workspace/plan.json, sources.json, report.md). Here each
// stage is an ADK llmagent; the "filesystem" becomes ADK session state that is
// seeded from Firestore before each run (see internal/state.Workspace) and
// injected into instructions via {plan}, {sources} and {report_md?}.
package research

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/geminitool"
	"google.golang.org/genai"
)

var (
	//go:embed prompts/plan.md
	planPrompt string
	//go:embed prompts/search_compare.md
	searchPrompt string
	//go:embed prompts/write_report.md
	writePrompt string
)

// Session state keys shared between stages.
const (
	KeyPlan     = "plan"
	KeySources  = "sources"
	KeyReportMD = "report_md"

	outPlan   = "plan_output"
	outSearch = "search_output"
	outWrite  = "write_output"
)

type stage struct {
	name      string
	outputKey string
	runner    *runner.Runner
}

// Agents runs the PLAN, SEARCH_COMPARE and WRITE_REPORT stages.
type Agents struct {
	appName  string
	sessions session.Service
	plan     stage
	search   stage
	write    stage
}

func urlContextTool() tool.Tool {
	return geminitool.New("url_context", "Reads the content of web pages by URL.",
		&genai.Tool{URLContext: &genai.URLContext{}})
}

// New builds the ADK agents and runners. appName is used as the ADK app
// name and agent name prefix (the AGENT_ID env var).
func New(llm model.LLM, appName string) (*Agents, error) {
	a := &Agents{appName: appName, sessions: session.InMemoryService()}
	prefix := strings.ReplaceAll(appName, "-", "_")

	defs := []struct {
		dst         *stage
		name        string
		description string
		instruction string
		schema      *genai.Schema
		outputKey   string
		tools       []tool.Tool
	}{
		{&a.plan, prefix + "_plan", "Designs Google search queries for a research topic.",
			planPrompt, planSchema, outPlan, nil},
		{&a.search, prefix + "_search_compare", "Searches the web and cross-compares sources.",
			searchPrompt, searchSchema, outSearch, []tool.Tool{geminitool.GoogleSearch{}, urlContextTool()}},
		{&a.write, prefix + "_write_report", "Writes or deepens the Markdown research report.",
			writePrompt, writeSchema, outWrite, []tool.Tool{geminitool.GoogleSearch{}, urlContextTool()}},
	}
	for _, d := range defs {
		ag, err := llmagent.New(llmagent.Config{
			Name:         d.name,
			Description:  d.description,
			Model:        llm,
			Instruction:  d.instruction,
			Tools:        d.tools,
			OutputSchema: d.schema,
			OutputKey:    d.outputKey,
		})
		if err != nil {
			return nil, fmt.Errorf("create agent %s: %w", d.name, err)
		}
		r, err := runner.New(runner.Config{AppName: appName, Agent: ag, SessionService: a.sessions})
		if err != nil {
			return nil, fmt.Errorf("create runner %s: %w", d.name, err)
		}
		*d.dst = stage{name: d.name, outputKey: d.outputKey, runner: r}
	}
	return a, nil
}

// run executes one stage in a fresh ADK session seeded with initState and
// returns the agent's structured output as a JSON string.
func (a *Agents) run(ctx context.Context, st stage, userID string, initState map[string]any, input string) (raw, invocationID string, err error) {
	created, err := a.sessions.Create(ctx, &session.CreateRequest{AppName: a.appName, UserID: userID, State: initState})
	if err != nil {
		return "", "", fmt.Errorf("%w: create session: %v", ErrAgent, err)
	}
	sid := created.Session.ID()
	defer func() {
		_ = a.sessions.Delete(context.WithoutCancel(ctx), &session.DeleteRequest{AppName: a.appName, UserID: userID, SessionID: sid})
	}()

	var lastText string
	for ev, err := range st.runner.Run(ctx, userID, sid, genai.NewContentFromText(input, genai.RoleUser), agent.RunConfig{}) {
		if err != nil {
			return "", invocationID, fmt.Errorf("%w: %s: %v", ErrAgent, st.name, err)
		}
		if ev == nil {
			continue
		}
		invocationID = ev.InvocationID
		if ev.Content != nil && ev.IsFinalResponse() {
			var sb strings.Builder
			for _, p := range ev.Content.Parts {
				if p.Text != "" && !p.Thought {
					sb.WriteString(p.Text)
				}
			}
			if sb.Len() > 0 {
				lastText = sb.String()
			}
		}
	}

	got, err := a.sessions.Get(ctx, &session.GetRequest{AppName: a.appName, UserID: userID, SessionID: sid})
	if err == nil {
		if v, err := got.Session.State().Get(st.outputKey); err == nil && v != nil {
			switch t := v.(type) {
			case string:
				raw = t
			default:
				b, _ := json.Marshal(t)
				raw = string(b)
			}
		}
	}
	if strings.TrimSpace(raw) == "" {
		raw = lastText
	}
	if strings.TrimSpace(raw) == "" {
		return "", invocationID, fmt.Errorf("%w: %s returned empty output", ErrAgent, st.name)
	}
	return raw, invocationID, nil
}

func runParsed[T any](ctx context.Context, a *Agents, st stage, userID string, initState map[string]any, input string) (*Output[T], error) {
	raw, inv, err := a.run(ctx, st, userID, initState, input)
	if err != nil {
		return nil, err
	}
	out := &Output[T]{Raw: raw, InvocationID: inv}
	if err := ParseJSON(raw, &out.Value); err != nil {
		return nil, err
	}
	return out, nil
}

// Plan runs stage 1.
func (a *Agents) Plan(ctx context.Context, userID, topic string) (*Output[PlanResult], error) {
	return runParsed[PlanResult](ctx, a, a.plan, userID, nil, "PLAN\n\ntopic: "+topic)
}

// SearchCompare runs stage 2 against the plan JSON from stage 1.
func (a *Agents) SearchCompare(ctx context.Context, userID, planJSON string) (*Output[SearchResult], error) {
	return runParsed[SearchResult](ctx, a, a.search, userID,
		map[string]any{KeyPlan: planJSON}, "SEARCH_COMPARE")
}

// WriteInput are the stage 3 parameters.
type WriteInput struct {
	SourcesJSON     string
	ReportMD        string // existing report, for deepen
	ReportID        string
	Mode            string // new | deepen
	PreviousVersion int
	DeepenRequest   string
}

// WriteReport runs stage 3.
func (a *Agents) WriteReport(ctx context.Context, userID string, in WriteInput) (*Output[WriteResult], error) {
	var msg strings.Builder
	fmt.Fprintf(&msg, "WRITE_REPORT\n\nreport_id: %s\nmode: %s\n", in.ReportID, in.Mode)
	if in.Mode == "deepen" {
		fmt.Fprintf(&msg, "previous_version: %d\ndeepen_request: %s\n", in.PreviousVersion, in.DeepenRequest)
	}
	sources := in.SourcesJSON
	if strings.TrimSpace(sources) == "" {
		sources = "{}"
	}
	return runParsed[WriteResult](ctx, a, a.write, userID,
		map[string]any{KeySources: sources, KeyReportMD: in.ReportMD}, msg.String())
}
