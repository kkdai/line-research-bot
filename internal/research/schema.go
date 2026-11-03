package research

import "google.golang.org/genai"

func str(desc string) *genai.Schema { return &genai.Schema{Type: genai.TypeString, Description: desc} }
func integer(desc string) *genai.Schema {
	return &genai.Schema{Type: genai.TypeInteger, Description: desc}
}
func arr(item *genai.Schema) *genai.Schema { return &genai.Schema{Type: genai.TypeArray, Items: item} }
func obj(props map[string]*genai.Schema, required ...string) *genai.Schema {
	return &genai.Schema{Type: genai.TypeObject, Properties: props, Required: required}
}

var sourceSchema = obj(map[string]*genai.Schema{
	"title":      str("page title"),
	"url":        str("page url"),
	"lang":       str("language code, e.g. en, zh-TW"),
	"summary":    str("~200-word factual summary"),
	"key_claims": arr(str("")),
}, "title", "url", "lang", "summary", "key_claims")

var planSchema = obj(map[string]*genai.Schema{
	"topic": str("original topic"),
	"queries": arr(obj(map[string]*genai.Schema{
		"q":               str("search query"),
		"source_category": str("official_docs | benchmark | community_review | news | academic | ..."),
		"lang":            str("language code"),
	}, "q", "source_category", "lang")),
	"source_count": integer("number of queries"),
}, "topic", "queries", "source_count")

var searchSchema = obj(map[string]*genai.Schema{
	"sources":    arr(sourceSchema),
	"agreements": arr(str("")),
	"disagreements": arr(obj(map[string]*genai.Schema{
		"point": str("the disputed point"),
		"sides": arr(obj(map[string]*genai.Schema{
			"claim":       str(""),
			"source_urls": arr(str("")),
		}, "claim", "source_urls")),
	}, "point", "sides")),
	"gaps":               arr(str("")),
	"source_count":       integer("number of sources"),
	"disagreement_count": integer("number of disagreements"),
}, "sources", "agreements", "disagreements", "gaps", "source_count", "disagreement_count")

var writeSchema = obj(map[string]*genai.Schema{
	"report_id":   str("echo of input report_id"),
	"report_md":   str("full Markdown report"),
	"summary_500": str("~500-character summary"),
	"top_citations": arr(obj(map[string]*genai.Schema{
		"title": str(""),
		"url":   str(""),
	}, "title", "url")),
	"new_version":   integer("1 for new, previous_version+1 for deepen"),
	"added_sources": arr(sourceSchema),
	"error": {Type: genai.TypeString, Enum: []string{ErrChapterNotFound},
		Description: "only set when the requested chapter does not exist"},
	"available_chapters": arr(str("")),
}, "report_id", "report_md", "summary_500", "top_citations", "new_version")
