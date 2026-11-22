// Package isud 优标宝开放 API Go 客户端 —— 手写体验层核心。
//
// 生成层在 models.go（勿手改），本文件实现 SDK-DESIGN.md 定义的全部体验：
// 鉴权、信封解析、异常映射、Retry-After 退避、幂等键、扣点回读、分页、本地校验。
// 零第三方依赖，仅用标准库 net/http。
package isud

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultBaseURL 开放 API 根路径
	DefaultBaseURL = "https://app.isud.cn/api/v1/open"
	// MCPBaseURL MCP 挂在 /api/v1，不在 /open 下
	MCPBaseURL = "https://app.isud.cn/api/v1"
)

// Client 优标宝开放 API 客户端。
type Client struct {
	apiKey     string
	baseURL    string
	mcpBaseURL string
	timeout    time.Duration
	maxRetries int
	http       *http.Client
	userAgent  string

	// LastMeta 最近一次调用的响应元数据（非并发安全，示例/调试用）
	LastMeta ResponseMeta

	// OnRetry 每次重试前的回调（异常、第几次、等待时长），可用于日志
	OnRetry func(*SdkError, int, time.Duration)
}

// ClientOption 客户端配置选项。
type ClientOption func(*Client)

// WithAPIKey 显式设置密钥（否则从 ISUD_API_KEY 环境变量读）。
func WithAPIKey(key string) ClientOption { return func(c *Client) { c.apiKey = key } }

// WithBaseURL 自定义开放 API 根路径。
func WithBaseURL(base string) ClientOption { return func(c *Client) { c.baseURL = strings.TrimRight(base, "/") } }

// WithMCPBaseURL 自定义 MCP 根路径。
func WithMCPBaseURL(base string) ClientOption { return func(c *Client) { c.mcpBaseURL = strings.TrimRight(base, "/") } }

// WithTimeout 请求超时（默认 30s）。
func WithTimeout(d time.Duration) ClientOption { return func(c *Client) { c.timeout = d } }

// WithMaxRetries 最大重试次数（默认 3）。
func WithMaxRetries(n int) ClientOption { return func(c *Client) { c.maxRetries = n } }

// WithHTTPClient 注入自定义 *http.Client（测试用，可配 RoundTripper）。
func WithHTTPClient(hc *http.Client) ClientOption { return func(c *Client) { c.http = hc } }

// WithUserAgent 自定义 User-Agent。
func WithUserAgent(ua string) ClientOption { return func(c *Client) { c.userAgent = ua } }

// NewClient 构造客户端。缺 key 时返回 ValidationError（友好提示，不裸奔）。
func NewClient(opts ...ClientOption) (*Client, error) {
	c := &Client{
		baseURL:    DefaultBaseURL,
		mcpBaseURL: MCPBaseURL,
		timeout:    30 * time.Second,
		maxRetries: 3,
		userAgent:  "isud-sdk-go/1.0.1",
	}
	for _, o := range opts {
		o(c)
	}
	if c.apiKey == "" {
		c.apiKey = os.Getenv("ISUD_API_KEY")
	}
	if c.apiKey == "" {
		return nil, newError(KindValidation,
			"缺少 API 密钥：请传入 WithAPIKey 或设置环境变量 ISUD_API_KEY。密钥在控制台创建（注册即送调用次数）。")
	}
	if c.http == nil {
		c.http = &http.Client{Timeout: c.timeout}
	}
	return c, nil
}

// requestOptions 单次请求的可选参数。
type requestOptions struct {
	method         string
	path           string
	params         url.Values
	jsonBody       any
	idempotent     bool
	idempotencyKey string
	mcp            bool
	noEnvelope     bool // true = 裸响应（/mcp/health、/mcp）；零值 false = 走业务信封
	retries        *int
}

// doOnce 发起单次 HTTP 请求并解析。
func (c *Client) doOnce(ctx context.Context, o requestOptions) (any, error) {
	root := c.baseURL
	if o.mcp {
		root = c.mcpBaseURL
	}
	fullURL := root + o.path
	if len(o.params) > 0 {
		fullURL += "?" + o.params.Encode()
	}

	var bodyReader io.Reader
	headers := map[string]string{
		"X-API-Key":   c.apiKey,
		"User-Agent":  c.userAgent,
		"Accept":      "application/json",
		"Content-Type": "application/json",
	}
	if o.jsonBody != nil {
		raw, err := json.Marshal(o.jsonBody)
		if err != nil {
			return nil, newError(KindValidation, fmt.Sprintf("请求体序列化失败: %v", err))
		}
		bodyReader = bytes.NewReader(raw)
	}
	if o.idempotent {
		key := o.idempotencyKey
		if key == "" {
			key = newUUIDHex()
		}
		if len(key) < 1 || len(key) > 128 {
			return nil, newError(KindValidation, "幂等键长度必须在 1~128 字符之间。")
		}
		headers["Idempotency-Key"] = key
	}

	req, err := http.NewRequestWithContext(ctx, o.method, fullURL, bodyReader)
	if err != nil {
		return nil, newError(KindNetwork, fmt.Sprintf("构造请求失败: %v", err))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, newError(KindNetwork, fmt.Sprintf("网络请求失败: %v", err))
	}
	defer resp.Body.Close()

	rawBody, _ := io.ReadAll(resp.Body)
	meta := buildMeta(resp, nil)

	// 裸响应端点（MCP health / JSON-RPC）没有业务信封
	if o.noEnvelope {
		c.LastMeta = meta
		if resp.StatusCode >= 400 {
			return nil, newError(KindNetwork,
				fmt.Sprintf("HTTP %d: %s", resp.StatusCode, trunc(string(rawBody), 200)),
				withMeta(meta), withStatus(resp.StatusCode))
		}
		var payload any
		if err := json.Unmarshal(rawBody, &payload); err != nil {
			payload = string(rawBody)
		}
		return payload, nil
	}

	// 业务信封解析
	var env Envelope
	if err := json.Unmarshal(rawBody, &env); err != nil {
		env = Envelope{
			Code: "0001",
			Msg:  fmt.Sprintf("HTTP %d 返回了非 JSON 内容", resp.StatusCode),
		}
	}
	meta = buildMeta(resp, &env)
	c.LastMeta = meta

	if err := RaiseForEnvelope(env, resp.StatusCode, meta); err != nil {
		var se *SdkError
		if asSdkError(err, &se) && se.Kind == KindRateLimit && se.RetryAfter == nil {
			if ra := ParseRetryAfter(resp.Header.Get("Retry-After")); ra != nil {
				se.RetryAfter = ra
			}
		}
		return nil, err
	}
	return env.Data, nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// buildMeta 从响应头 + 信封抽取元数据。
func buildMeta(resp *http.Response, env *Envelope) ResponseMeta {
	asInt := func(name string) *int {
		v := resp.Header.Get(name)
		if v == "" {
			return nil
		}
		if n, err := strconv.Atoi(v); err == nil {
			return &n
		}
		return nil
	}
	m := ResponseMeta{
		CreditsCharged:   asInt("X-Credits-Charged"),
		CreditsRemaining: asInt("X-Credits-Remaining"),
		RateLimit:        asInt("X-RateLimit-Limit"),
		RateRemaining:    asInt("X-RateLimit-Remaining"),
		IdempotentReplay: strings.EqualFold(resp.Header.Get("Idempotency-Replayed"), "true"),
		HTTPStatus:       resp.StatusCode,
	}
	if env != nil {
		m.RequestID = env.RequestID
	}
	if m.RequestID == "" {
		m.RequestID = resp.Header.Get("X-Request-Id")
	}
	return m
}

// request 统一请求管线：重试 + 幂等安全判定。
func (c *Client) request(ctx context.Context, o requestOptions) (any, error) {
	attempts := c.maxRetries
	if o.retries != nil {
		attempts = *o.retries
	}
	safe := o.method == http.MethodGet || o.idempotent
	shouldRetry := func(se *SdkError) bool { return safe }

	fn := func() (any, error) { return c.doOnce(ctx, o) }
	onRetry := c.OnRetry
	return WithRetry(fn, attempts, shouldRetry, time.Sleep, onRetry)
}

// ------------------------------------------------------------------ //
// 本地参数校验（避免无谓扣点，见 SDK-DESIGN.md §5）
// ------------------------------------------------------------------ //
func checkSize(size int) error {
	if size < 1 || size > 100 {
		return newError(KindValidation, fmt.Sprintf("size 必须在 1~100 之间（收到 %d）。", size))
	}
	return nil
}

func checkSearchInput(keyword, owner string) error {
	if keyword == "" && owner == "" {
		return newError(KindValidation, "keyword 与 owner 至少提供一项。")
	}
	return nil
}

// ------------------------------------------------------------------ //
// 9 个端点方法（与 operationId 对齐）
// ------------------------------------------------------------------ //

// TrademarkSearchInput POST /ip/trademark/search 入参（扣 10 点，自动注入幂等键）。
type TrademarkSearchInput struct {
	JurisdictionCode string
	ConnectorCode    string
	Keyword          string
	NiceClasses      []string
	Status           []string
	Owner            string
	Current          int
	Size             int
	IntentMark       string
	IntentClasses    []string
	SaveReport       bool
	ReportTitle      string
	IdempotencyKey   string
}

// SearchTrademarks 商标检索与冲突判定。
func (c *Client) SearchTrademarks(ctx context.Context, in TrademarkSearchInput) (*TrademarkSearchResult, error) {
	if in.Size == 0 {
		in.Size = 50
	}
	if err := checkSize(in.Size); err != nil {
		return nil, err
	}
	if err := checkSearchInput(in.Keyword, in.Owner); err != nil {
		return nil, err
	}
	if in.Current == 0 {
		in.Current = 1
	}
	body := map[string]any{
		"jurisdictionCode": in.JurisdictionCode,
		"current":          in.Current,
		"size":             in.Size,
	}
	putIf(body, "connectorCode", in.ConnectorCode)
	putIf(body, "keyword", in.Keyword)
	putIf(body, "owner", in.Owner)
	putIf(body, "niceClasses", in.NiceClasses)
	putIf(body, "status", in.Status)
	putIf(body, "intentMark", in.IntentMark)
	putIf(body, "intentClasses", in.IntentClasses)
	putIf(body, "reportTitle", in.ReportTitle)
	if in.SaveReport {
		body["saveReport"] = true
	}
	data, err := c.request(ctx, requestOptions{
		method: http.MethodPost, path: "/ip/trademark/search", jsonBody: body,
		idempotent: true, idempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return nil, err
	}
	return decodeResult[TrademarkSearchResult](data)
}

// DesignSearchInput POST /ip/design/search 入参（扣 10 点；CN 法域暂未开放）。
type DesignSearchInput struct {
	JurisdictionCode string
	Keyword          string
	LocarnoClasses   []string
	Status           []string
	Owner            string
	SaveReport       bool
	ReportTitle      string
	IdempotencyKey   string
}

// SearchDesigns 外观设计检索。
func (c *Client) SearchDesigns(ctx context.Context, in DesignSearchInput) (any, error) {
	body := map[string]any{
		"jurisdictionCode": in.JurisdictionCode,
		"keyword":          in.Keyword,
	}
	putIf(body, "locarnoClasses", in.LocarnoClasses)
	putIf(body, "status", in.Status)
	putIf(body, "owner", in.Owner)
	putIf(body, "reportTitle", in.ReportTitle)
	if in.SaveReport {
		body["saveReport"] = true
	}
	return c.request(ctx, requestOptions{
		method: http.MethodPost, path: "/ip/design/search", jsonBody: body,
		idempotent: true, idempotencyKey: in.IdempotencyKey,
	})
}

// TrademarkListInput GET /ip/trademark/list 入参。
type TrademarkListInput struct {
	Current   int
	Size      int
	Keyword   string
	MarkText  string
	OwnerName string
	Status    string
	NiceClass string
	MarkType  string
	RiskLevel string
	SortBy    string
	IsOwned   *bool
	IsWatched *bool
}

// ListTrademarks 分页查询商标档案。
func (c *Client) ListTrademarks(ctx context.Context, in TrademarkListInput) (*Page, error) {
	if in.Size == 0 {
		in.Size = 10
	}
	if err := checkSize(in.Size); err != nil {
		return nil, err
	}
	if in.Current == 0 {
		in.Current = 1
	}
	q := url.Values{}
	q.Set("current", strconv.Itoa(in.Current))
	q.Set("size", strconv.Itoa(in.Size))
	putQ(q, "keyword", in.Keyword)
	putQ(q, "markText", in.MarkText)
	putQ(q, "ownerName", in.OwnerName)
	putQ(q, "status", in.Status)
	putQ(q, "niceClass", in.NiceClass)
	putQ(q, "markType", in.MarkType)
	putQ(q, "riskLevel", in.RiskLevel)
	putQ(q, "sortBy", in.SortBy)
	if in.IsOwned != nil {
		q.Set("isOwned", strconv.FormatBool(*in.IsOwned))
	}
	if in.IsWatched != nil {
		q.Set("isWatched", strconv.FormatBool(*in.IsWatched))
	}
	data, err := c.request(ctx, requestOptions{
		method: http.MethodGet, path: "/ip/trademark/list", params: q,
	})
	if err != nil {
		return nil, err
	}
	return decodePage(data)
}

// IterTrademarks 自动翻页迭代商标档案，把每条记录送到 ch（分页安全重试）。
func (c *Client) IterTrademarks(ctx context.Context, in TrademarkListInput, maxPages int) (<-chan map[string]any, <-chan error) {
	ch := make(chan map[string]any)
	errCh := make(chan error, 1)
	go func() {
		defer close(ch)
		defer close(errCh)
		if in.Size == 0 {
			in.Size = 50
		}
		page, pages := in.Current, 0
		if page == 0 {
			page = 1
		}
		for {
			if maxPages > 0 && pages >= maxPages {
				return
			}
			in.Current = page
			result, err := c.ListTrademarks(ctx, in)
			if err != nil {
				errCh <- err
				return
			}
			if result == nil {
				return
			}
			for _, rec := range result.Records {
				select {
				case ch <- rec:
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				}
			}
			pages++
			totalPages := 0
			if result.Size > 0 {
				totalPages = (result.Total + result.Size - 1) / result.Size
			}
			if len(result.Records) == 0 || page >= totalPages {
				return
			}
			page++
		}
	}()
	return ch, errCh
}

// ListReports 分页查询排查报告。
func (c *Client) ListReports(ctx context.Context, current, size int) (any, error) {
	if size == 0 {
		size = 10
	}
	if err := checkSize(size); err != nil {
		return nil, err
	}
	if current == 0 {
		current = 1
	}
	q := url.Values{}
	q.Set("current", strconv.Itoa(current))
	q.Set("size", strconv.Itoa(size))
	return c.request(ctx, requestOptions{method: http.MethodGet, path: "/ip/report/list", params: q})
}

// GetWorkbenchStats 工作台汇总统计。
func (c *Client) GetWorkbenchStats(ctx context.Context) (any, error) {
	return c.request(ctx, requestOptions{method: http.MethodGet, path: "/ip/workbench/stats"})
}

// GetUsage 点数余额与用量查询（实时）。
func (c *Client) GetUsage(ctx context.Context) (*OpenApiUsage, error) {
	data, err := c.request(ctx, requestOptions{method: http.MethodGet, path: "/ip/usage"})
	if err != nil {
		return nil, err
	}
	return decodeResult[OpenApiUsage](data)
}

// QueryRequestLog 按 requestId 回查请求日志。
func (c *Client) QueryRequestLog(ctx context.Context, requestID string) (any, error) {
	if requestID == "" || len(requestID) > 64 {
		return nil, newError(KindValidation, "requestId 必须为 1~64 字符。")
	}
	q := url.Values{}
	q.Set("requestId", requestID)
	return c.request(ctx, requestOptions{method: http.MethodGet, path: "/ip/requests", params: q})
}

// ---- 数据能力：图形检索 / 商标公告 / 评审文书 / 海关备案（GET，读类 1 点）----
// 返回 data 原始 JSON（any），结构随调用方传入的 mode 变化，由调用方按 mode 解析。

// QueryVienna 图形检索：维也纳 VCL10 编码 / 关键词换码 / 分类树 / 以图搜图。
func (c *Client) QueryVienna(ctx context.Context, params url.Values) (any, error) {
	return c.request(ctx, requestOptions{method: http.MethodGet, path: "/ip/vienna/query", params: params})
}

// QueryGazette 商标公告：CN 公告明细与期号 / 全球 7 局事件。
func (c *Client) QueryGazette(ctx context.Context, params url.Values) (any, error) {
	return c.request(ctx, requestOptions{method: http.MethodGet, path: "/ip/gazette/query", params: params})
}

// QueryReview 评审文书：CNIPA 298 万裁定书。
func (c *Client) QueryReview(ctx context.Context, params url.Values) (any, error) {
	return c.request(ctx, requestOptions{method: http.MethodGet, path: "/ip/review/query", params: params})
}

// QueryIpr 海关备案：12 万条备案记录（可选接入）。
func (c *Client) QueryIpr(ctx context.Context, params url.Values) (any, error) {
	return c.request(ctx, requestOptions{method: http.MethodGet, path: "/ip/ipr/query", params: params})
}

// GetMCPHealth MCP 服务健康检查（无需鉴权语义由服务端决定；裸响应）。
func (c *Client) GetMCPHealth(ctx context.Context) (*MCPHealth, error) {
	zero := 0
	data, err := c.request(ctx, requestOptions{
		method: http.MethodGet, path: "/mcp/health", mcp: true, noEnvelope: true, retries: &zero,
	})
	if err != nil {
		return nil, err
	}
	return decodeResult[MCPHealth](data)
}

// CallMCP MCP JSON-RPC 调用（scope: mcp）。默认不自动重试。
func (c *Client) CallMCP(ctx context.Context, method string, params any, id int) (any, error) {
	if id == 0 {
		id = 1
	}
	payload := map[string]any{"jsonrpc": "2.0", "method": method, "id": id}
	if params != nil {
		payload["params"] = params
	}
	zero := 0
	return c.request(ctx, requestOptions{
		method: http.MethodPost, path: "/mcp", jsonBody: payload,
		mcp: true, noEnvelope: true, retries: &zero,
	})
}

// ------------------------------------------------------------------ //
// 泛型辅助
// ------------------------------------------------------------------ //
func putIf(m map[string]any, k string, v any) {
	switch x := v.(type) {
	case string:
		if x != "" {
			m[k] = x
		}
	case []string:
		if len(x) > 0 {
			m[k] = x
		}
	}
}

func putQ(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

// newUUIDHex 生成 32 位十六进制 UUID v4（无连字符），仅用标准库。
func newUUIDHex() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 极端情况回退到时间+计数器，保证幂等键非空且长度合法
		return hex.EncodeToString([]byte(fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return hex.EncodeToString(b[:])
}

// decodeResult 把 data（可能是 map[string]any）重新编码成目标结构。
func decodeResult[T any](data any) (*T, error) {
	if data == nil {
		return nil, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, newError(KindApi, fmt.Sprintf("响应数据编码失败: %v", err))
	}
	var out T
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, newError(KindApi, fmt.Sprintf("响应数据解析失败: %v", err))
	}
	return &out, nil
}

func decodePage(data any) (*Page, error) {
	return decodeResult[Page](data)
}
