// Package flex builds LINE progress texts and the final report Flex card.
package flex

import "fmt"

type Stage string

const (
	Planning   Stage = "planning"
	PlanDone   Stage = "plan_done"
	SearchDone Stage = "search_done"
	Writing    Stage = "writing"
)

// ProgressText returns the progress message for a stage.
func ProgressText(stage Stage, sourceCount, disagreementCount int) string {
	switch stage {
	case Planning:
		return "📋 已收到題目，開始規劃…"
	case PlanDone:
		return fmt.Sprintf("🔍 計畫完成：將比對 %d 類來源", sourceCount)
	case SearchDone:
		return fmt.Sprintf("📊 已比對 %d 份來源，發現 %d 處重要分歧", sourceCount, disagreementCount)
	case Writing:
		return "📝 正在撰寫報告…"
	}
	panic("unknown stage: " + string(stage))
}

type Citation struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Message is a Flex message: altText plus the raw container JSON object.
type Message struct {
	AltText  string
	Contents map[string]any
}

const maxSummaryRunes = 400

// ReportCard builds the final report card shown in LINE.
func ReportCard(topic, summary, reportURL string, citations []Citation, version int) Message {
	if r := []rune(summary); len(r) > maxSummaryRunes {
		summary = string(r[:maxSummaryRunes]) + "…"
	}

	body := []any{
		map[string]any{
			"type":   "box",
			"layout": "horizontal",
			"contents": []any{
				map[string]any{"type": "text", "text": topic, "weight": "bold", "size": "lg", "wrap": true, "flex": 5},
				map[string]any{"type": "text", "text": fmt.Sprintf("v%d", version), "size": "sm", "color": "#888888", "align": "end", "flex": 1},
			},
		},
		map[string]any{"type": "separator", "margin": "md"},
		map[string]any{"type": "text", "text": summary, "wrap": true, "size": "sm", "margin": "md"},
	}

	var cites []any
	for i, c := range citations {
		if i >= 3 {
			break
		}
		if c.URL == "" || c.Title == "" {
			continue
		}
		cites = append(cites, map[string]any{
			"type": "text", "text": "• " + c.Title, "wrap": true, "size": "xs", "color": "#666666",
			"action": map[string]any{"type": "uri", "uri": c.URL},
		})
	}
	if len(cites) > 0 {
		body = append(body,
			map[string]any{"type": "separator", "margin": "md"},
			map[string]any{"type": "text", "text": "主要來源", "size": "xs", "color": "#888888", "margin": "md"},
		)
		body = append(body, cites...)
	}

	return Message{
		AltText: fmt.Sprintf("📝 %s 研究報告已完成", topic),
		Contents: map[string]any{
			"type": "bubble",
			"body": map[string]any{"type": "box", "layout": "vertical", "contents": body},
			"footer": map[string]any{
				"type": "box", "layout": "vertical",
				"contents": []any{map[string]any{
					"type": "button", "style": "primary",
					"action": map[string]any{"type": "uri", "label": "閱讀完整報告", "uri": reportURL},
				}},
			},
		},
	}
}
