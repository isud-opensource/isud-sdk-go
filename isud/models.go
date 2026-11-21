// Code generated from openapi.yaml (OpenAPI 3.1, v1.0.0). DO NOT EDIT BY HAND.
// 映射契约中的 schema；字段开放枚举一律用 string，不封闭成常量，便于前向兼容。
// 重新生成：见 README「模型层同步」章节。
package isud

// TrademarkItem 商标条目（对外稳定视图）。
type TrademarkItem struct {
	MarkText           string   `json:"markText"`
	MarkType           string   `json:"markType"`
	NiceClasses        []string `json:"niceClasses"`
	OwnerName          string   `json:"ownerName"`
	Status             string   `json:"status"`
	ApplicationNumber  string   `json:"applicationNumber"`
	RegistrationNumber string   `json:"registrationNumber"`
	FilingDate         int64    `json:"filingDate"`
	RegistrationDate   int64    `json:"registrationDate"`
	ExpiryDate         int64    `json:"expiryDate"`
	RiskLevel          string   `json:"riskLevel"`
	RiskReason         string   `json:"riskReason"`
	ExternalID         string   `json:"externalId"`
}

// OpenSearchMeta 检索可审计元数据。
type OpenSearchMeta struct {
	SortVersion string `json:"sortVersion"`
	DataChannel string `json:"dataChannel"`
	Normalizer  string `json:"normalizer"`
}

// TrademarkSearchResult POST /ip/trademark/search 的 data 载荷。
type TrademarkSearchResult struct {
	JurisdictionCode string          `json:"jurisdictionCode"`
	Keyword          string          `json:"keyword"`
	Total            int             `json:"total"`
	Page             int             `json:"page"`
	Size             int             `json:"size"`
	TotalPages       int             `json:"totalPages"`
	HighRisk         int             `json:"highRisk"`
	MediumRisk       int             `json:"mediumRisk"`
	LowRisk          int             `json:"lowRisk"`
	ActiveCount      int             `json:"activeCount"`
	Conclusion       string          `json:"conclusion"`
	Source           string          `json:"source"`
	ElapsedMS        int64           `json:"elapsedMs"`
	Meta             OpenSearchMeta  `json:"meta"`
	ReportID         int64           `json:"reportId"`
	Items            []TrademarkItem `json:"items"`
}

// DesignItem 外观设计条目。
type DesignItem struct {
	Title             string `json:"title"`
	LocarnoClass      string `json:"locarnoClass"`
	LocarnoIndication string `json:"locarnoIndication"`
	OwnerName         string `json:"ownerName"`
	Status            string `json:"status"`
	ApplicationNumber string `json:"applicationNumber"`
	FilingDate        int64  `json:"filingDate"`
	ExpiryDate        int64  `json:"expiryDate"`
	ExternalID        string `json:"externalId"`
}

// ReportItem 排查报告条目。
type ReportItem struct {
	ReportNo      string `json:"reportNo"`
	ReportType    string `json:"reportType"`
	Title         string `json:"title"`
	Keyword       string `json:"keyword"`
	RiskLevel     string `json:"riskLevel"`
	Conclusion    string `json:"conclusion"`
	TotalResults  int    `json:"totalResults"`
	ConflictCount int    `json:"conflictCount"`
	HighRiskCount int    `json:"highRiskCount"`
	CreatedAt     int64  `json:"createdAt"`
}

// WorkbenchStats 工作台汇总统计。
type WorkbenchStats struct {
	TrademarkTotal      int `json:"trademarkTotal"`
	TrademarkOwned      int `json:"trademarkOwned"`
	DesignTotal         int `json:"designTotal"`
	HighRiskTotal       int `json:"highRiskTotal"`
	WatchActiveCount    int `json:"watchActiveCount"`
	WatchDueCount       int `json:"watchDueCount"`
	CaseActiveCount     int `json:"caseActiveCount"`
	DeadlineDueCount    int `json:"deadlineDueCount"`
	DeadlineOverdueCount int `json:"deadlineOverdueCount"`
}

// UsagePeriod 用量周期计数（今日/本月）。
type UsagePeriod struct {
	Calls  int `json:"calls"`
	Points int `json:"points"`
}

// OpenApiUsage 开放 API 用量（组织级共享池）。
type OpenApiUsage struct {
	Balance       int         `json:"balance"`
	Credits       int         `json:"credits"`
	CreditsUsed   int         `json:"creditsUsed"`
	Today         UsagePeriod `json:"today"`
	Month         UsagePeriod `json:"month"`
	PeriodResetAt int64       `json:"periodResetAt"`
	DailyLimit    int         `json:"dailyLimit"`
}

// RequestLogItem 请求日志条目。
type RequestLogItem struct {
	RequestID  string `json:"requestId"`
	Endpoint   string `json:"endpoint"`
	Method     string `json:"method"`
	Success    bool   `json:"success"`
	Credits    int    `json:"credits"`
	StatusCode int    `json:"statusCode"`
	LatencyMS  int64  `json:"latencyMs"`
	CreatedAt  int64  `json:"createdAt"`
}

// Page 通用分页响应（list 系列端点的 data 载荷）。
type Page struct {
	Current int `json:"current"`
	Size    int `json:"size"`
	Total   int `json:"total"`
	Records []map[string]any `json:"records"`
}

// MCPHealth /mcp/health 裸响应（无信封）。
type MCPHealth struct {
	Status    string `json:"status"`
	ToolCount int    `json:"toolCount"`
}
