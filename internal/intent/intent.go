// Package intent classifies an incoming LINE message with simple keyword rules.
package intent

import "strings"

type Intent string

const (
	New      Intent = "new"
	Deepen   Intent = "deepen"
	Reset    Intent = "reset"
	Recall   Intent = "recall"
	Retry    Intent = "retry"
	Chitchat Intent = "chitchat"
)

// UserState is the subset of user state the classifier needs.
type UserState struct {
	CurrentReportID string // "" when there is no active report
	PendingAction   string // "", "retry_write" or "retry_publish"
}

var (
	newKeywords      = []string{"研究", "幫我查", "找一下"}
	deepenKeywords   = []string{"深化", "補", "改", "第"}
	resetKeywords    = []string{"重新開始", "換題目", "清除"}
	recallKeywords   = []string{"上次", "我之前", "連結"}
	retryKeywords    = []string{"再試一次", "再發佈一次"}
	chitchatGreeting = []string{"你好", "嗨", "hi", "hello", "哈囉"}
)

func containsAny(s string, ks []string) bool {
	for _, k := range ks {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func hasAnyPrefix(s string, ks []string) bool {
	for _, k := range ks {
		if strings.HasPrefix(s, k) {
			return true
		}
	}
	return false
}

// Classify returns the intent for message given the user's state.
func Classify(message string, st UserState) Intent {
	m := strings.TrimSpace(message)

	if st.PendingAction != "" && containsAny(m, retryKeywords) {
		return Retry
	}
	if hasAnyPrefix(m, resetKeywords) {
		return Reset
	}
	if containsAny(m, recallKeywords) {
		return Recall
	}
	if st.CurrentReportID != "" && hasAnyPrefix(m, deepenKeywords) {
		return Deepen
	}
	if hasAnyPrefix(m, newKeywords) {
		return New
	}
	// len() in Python counts code points, not bytes.
	if len([]rune(m)) < 5 && containsAny(strings.ToLower(m), chitchatGreeting) {
		return Chitchat
	}
	if st.CurrentReportID == "" {
		return New
	}
	return Chitchat
}
