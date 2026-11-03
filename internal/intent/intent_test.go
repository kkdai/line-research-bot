package intent

import "testing"

func TestClassify(t *testing.T) {
	none := UserState{}
	active := UserState{CurrentReportID: "r1"}
	pending := UserState{CurrentReportID: "r1", PendingAction: "retry_write"}

	cases := []struct {
		name string
		msg  string
		st   UserState
		want Intent
	}{
		{"new keyword", "研究 向量資料庫", none, New},
		{"new keyword with active report", "幫我查 Go ADK", active, New},
		{"deepen", "第 2 章再深一點", active, Deepen},
		{"deepen needs report", "第 2 章再深一點", none, New},
		{"reset", "重新開始", active, Reset},
		{"recall", "上次的報告呢", active, Recall},
		{"retry with pending", "再試一次", pending, Retry},
		{"retry without pending is chitchat", "再試一次", active, Chitchat},
		{"greeting", "hi", none, Chitchat},
		{"greeting zh", "你好", active, Chitchat},
		{"free text no report -> new", "量子電腦的商業化進展", none, New},
		{"free text with report -> chitchat", "量子電腦的商業化進展", active, Chitchat},
		{"whitespace trimmed", "  研究 X  ", none, New},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Classify(c.msg, c.st); got != c.want {
				t.Fatalf("Classify(%q) = %q, want %q", c.msg, got, c.want)
			}
		})
	}
}
