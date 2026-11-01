package flex

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProgressText(t *testing.T) {
	if got := ProgressText(PlanDone, 7, 0); got != "🔍 計畫完成：將比對 7 類來源" {
		t.Fatal(got)
	}
	if got := ProgressText(SearchDone, 7, 3); got != "📊 已比對 7 份來源，發現 3 處重要分歧" {
		t.Fatal(got)
	}
}

func TestReportCard(t *testing.T) {
	long := strings.Repeat("字", 500)
	cites := []Citation{{"a", "https://a"}, {"b", "https://b"}, {"c", "https://c"}, {"d", "https://d"}}
	m := ReportCard("主題", long, "https://report", cites, 2)
	if m.AltText != "📝 主題 研究報告已完成" {
		t.Fatal(m.AltText)
	}
	raw, _ := json.Marshal(m.Contents)
	s := string(raw)
	if !strings.Contains(s, `"v2"`) || !strings.Contains(s, "https://report") {
		t.Fatalf("missing badge/url: %s", s)
	}
	if strings.Contains(s, "https://d") {
		t.Fatal("should only include top 3 citations")
	}
	if !strings.Contains(s, strings.Repeat("字", 400)+"…") || strings.Contains(s, strings.Repeat("字", 401)) {
		t.Fatal("summary not truncated to 400 runes")
	}
}

func TestReportCardNoCitations(t *testing.T) {
	raw, _ := json.Marshal(ReportCard("t", "s", "https://r", nil, 1).Contents)
	if strings.Contains(string(raw), "主要來源") {
		t.Fatal("no citations header expected")
	}
}
