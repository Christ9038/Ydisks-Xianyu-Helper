package server

// keywordBasicResponse 是传统关键词接口的基础响应 DTO。
type keywordBasicResponse struct {
	// Keyword 是第一条匹配表达式的兼容单值字段。
	Keyword string `json:"keyword"`
	// Expressions 是同一条规则的多个匹配表达式。
	Expressions []string `json:"expressions,omitempty"`
	// MatchType 是 contains 或 regexp 匹配模式。
	MatchType string `json:"match_type,omitempty"`
	// Reply 是文字回复内容。
	Reply string `json:"reply"`
}

// keywordItemResponse 是带商品范围的关键词响应 DTO。
type keywordItemResponse struct {
	// Keyword 是第一条匹配表达式的兼容单值字段。
	Keyword string `json:"keyword"`
	// Expressions 是同一条规则的多个匹配表达式。
	Expressions []string `json:"expressions,omitempty"`
	// MatchType 是 contains 或 regexp 匹配模式。
	MatchType string `json:"match_type,omitempty"`
	// Reply 是文字回复内容。
	Reply string `json:"reply"`
	// ItemID 是限定的商品标识。
	ItemID string `json:"item_id"`
}

// keywordTypedResponse 是支持文本、图片和多表达式规则的关键词响应 DTO。
type keywordTypedResponse struct {
	// ID 是关键词规则主键。
	ID int64 `json:"id"`
	// Keyword 是第一条匹配表达式的兼容单值字段。
	Keyword string `json:"keyword"`
	// Expressions 是同一条规则的多个匹配表达式。
	Expressions []string `json:"expressions"`
	// MatchType 是 contains 或 regexp 匹配模式。
	MatchType string `json:"match_type"`
	// Reply 是文字回复内容。
	Reply string `json:"reply"`
	// ItemID 是限定的商品标识；多选规则取 ItemIDs 首项以保持兼容。
	ItemID string `json:"item_id"`
	// ItemIDs 是限定的商品标识集合；空集合表示账号级规则。
	ItemIDs []string `json:"item_ids"`
	// Type 是回复类型。
	Type string `json:"type"`
	// ImageURL 是图片回复地址。
	ImageURL string `json:"image_url"`
	// LibraryImageID 是图片库图片主键；与 image_url 互斥，0 或省略表示未使用图片库。
	LibraryImageID int64 `json:"library_image_id,omitempty"`
}
