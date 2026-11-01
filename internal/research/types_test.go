package research

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestParseJSON(t *testing.T) {
	cases := map[string]string{
		"raw":    `{"source_count": 3}`,
		"fenced": "here you go\n```json\n{\"source_count\": 3}\n```\nthanks",
		"prose":  `Result: {"source_count": 3} done.`,
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			var p PlanResult
			if err := ParseJSON(in, &p); err != nil || p.SourceCount != 3 {
				t.Fatalf("got %+v, %v", p, err)
			}
		})
	}
	var p PlanResult
	if err := ParseJSON("not json", &p); !errors.Is(err, ErrAgent) {
		t.Fatalf("want ErrAgent, got %v", err)
	}
}

func TestMergeSources(t *testing.T) {
	orig := `{"sources":[{"title":"a","url":"https://a"}],"source_count":1,"gaps":["g"]}`
	out, err := MergeSources(orig, []Source{{Title: "b", URL: "https://b"}})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal([]byte(out), &doc)
	if doc["source_count"].(float64) != 2 || len(doc["sources"].([]any)) != 2 || doc["gaps"] == nil {
		t.Fatalf("bad merge: %s", out)
	}
	if same, _ := MergeSources(orig, nil); same != orig {
		t.Fatal("no-op merge should return input")
	}
	if out, _ := MergeSources("", []Source{{Title: "x"}}); !strings.Contains(out, `"x"`) {
		t.Fatal(out)
	}
}

func TestPromptsHaveOnlyKnownPlaceholders(t *testing.T) {
	// ADK treats {identifier} as session-state placeholders; make sure the
	// prompts only reference keys we actually seed.
	allowed := map[string]bool{"{plan}": true, "{sources}": true, "{report_md?}": true}
	for name, p := range map[string]string{"plan": planPrompt, "search": searchPrompt, "write": writePrompt} {
		for _, m := range placeholderRE.FindAllString(p, -1) {
			if !allowed[m] {
				t.Errorf("%s prompt has unexpected placeholder %s", name, m)
			}
		}
	}
}

var placeholderRE = regexp.MustCompile(`\{[a-zA-Z_][a-zA-Z0-9_]*\??\}`)
