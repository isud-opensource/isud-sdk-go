// Package isud 优标宝开放 API Go SDK —— 异常体系。
//
// 设计见 SDK-DESIGN.md §3。错误码/HTTP 状态码到 Go error 类型的映射，
// 与 Python/TypeScript 版本保持同样的分类与语义。
package isud

import (
	"errors"
	"fmt"
	"strings"
)

// ResponseMeta 每次调用都解析的响应元数据（扣点/追踪/限流），成功失败都有。
type ResponseMeta struct {
	RequestID        string
	CreditsCharged   *int
	CreditsRemaining *int
	RateLimit        *int
	RateRemaining    *int
	IdempotentReplay bool
	HTTPStatus       int
}

func (m ResponseMeta) String() string {
	return fmt.Sprintf("request_id=%s credits_charged=%v credits_remaining=%v",
		m.RequestID, derefInt(m.CreditsCharged), derefInt(m.CreditsRemaining))
}

func derefInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// DerefInt 安全解引用 *int，nil 返回 0。给示例/调用方用。
func DerefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// ErrorKind 错误分类，供调用方做 errors.As / 类型判断。
type ErrorKind string

const (
	KindAuth       ErrorKind = "AuthenticationError"
	KindRateLimit  ErrorKind = "RateLimitError"
	KindValidation ErrorKind = "ValidationError"
	KindCapability ErrorKind = "CapabilityError"
	KindApi        ErrorKind = "ApiError"
	KindNetwork    ErrorKind = "NetworkError"
)

// SdkError 所有 SDK 异常的基类。
type SdkError struct {
	Kind       ErrorKind
	Message    string
	Code       string
	RequestID  string
	Suggestion string
	Retryable  bool
	Meta       ResponseMeta
	StatusCode int

	// RetryAfter 仅 RateLimitError 使用（秒）
	RetryAfter *int
}

func (e *SdkError) Error() string {
	var b strings.Builder
	b.WriteString(e.Message)
	if e.Code != "" {
		fmt.Fprintf(&b, " [code=%s]", e.Code)
	}
	if e.Suggestion != "" {
		fmt.Fprintf(&b, " 建议: %s", e.Suggestion)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, " (requestId=%s)", e.RequestID)
	}
	return b.String()
}

// Friendly 给示例/终端用的完整友好提示（不裸 stack）。
func (e *SdkError) Friendly() string {
	var b strings.Builder
	fmt.Fprintf(&b, "✗ %s\n", e.Message)
	if e.Code != "" {
		fmt.Fprintf(&b, "  错误码: %s\n", e.Code)
	}
	if e.Suggestion != "" {
		fmt.Fprintf(&b, "  建议: %s\n", e.Suggestion)
	}
	if e.RequestID != "" {
		fmt.Fprintf(&b, "  追踪ID: %s（报障时提供）\n", e.RequestID)
	}
	if e.Meta.CreditsCharged != nil {
		fmt.Fprintf(&b, "  本次扣点: %d\n", *e.Meta.CreditsCharged)
	}
	return strings.TrimRight(b.String(), "\n")
}

// IsKind 判断错误分类（等价其他语言的 isinstance）。
func IsKind(err error, kind ErrorKind) bool {
	var se *SdkError
	if ok := asSdkError(err, &se); !ok {
		return false
	}
	return se.Kind == kind
}

// asSdkError 包装 errors.As，避免在 client 里散落 type assert。
func asSdkError(err error, target **SdkError) bool {
	return errors.As(err, target)
}

// codeToKind 信封 code -> 异常分类（HTTP 401/429 由 client 层优先判定）。
var codeToKind = map[string]ErrorKind{
	"8889": KindAuth,
	"9001": KindValidation,
	"9002": KindRateLimit,
	"9003": KindRateLimit,
	"0002": KindCapability,
	"0001": KindApi,
}

// newError 构造SdkError，按 kind 决定默认可重试性。
func newError(kind ErrorKind, message string, opts ...errorOpt) *SdkError {
	defaultRetryable := kind == KindRateLimit || kind == KindNetwork
	e := &SdkError{Kind: kind, Message: message, Retryable: defaultRetryable}
	for _, o := range opts {
		o(e)
	}
	return e
}

type errorOpt func(*SdkError)

func withCode(v string) errorOpt       { return func(e *SdkError) { e.Code = v } }
func withRequestID(v string) errorOpt  { return func(e *SdkError) { e.RequestID = v } }
func withSuggestion(v string) errorOpt { return func(e *SdkError) { e.Suggestion = v } }
func withMeta(m ResponseMeta) errorOpt { return func(e *SdkError) { e.Meta = m } }
func withStatus(s int) errorOpt        { return func(e *SdkError) { e.StatusCode = s } }
func withRetryable(v bool) errorOpt    { return func(e *SdkError) { e.Retryable = v } }
func withRetryAfter(v int) errorOpt {
	return func(e *SdkError) { e.RetryAfter = &v }
}

// RaiseForEnvelope 根据信封 + HTTP 状态码构造对应异常；成功（0000）返回 nil。
// 优先级：先看 HTTP 状态码（401/429），再看信封 code。
func RaiseForEnvelope(env Envelope, httpStatus int, meta ResponseMeta) error {
	code := env.Code
	if code == "0000" {
		return nil
	}
	msg := env.Msg
	if msg == "" {
		msg = "请求失败"
	}
	rid := env.RequestID
	if rid == "" {
		rid = meta.RequestID
	}

	base := []errorOpt{
		withCode(code),
		withRequestID(rid),
		withSuggestion(env.Suggestion),
		withMeta(meta),
		withStatus(httpStatus),
	}
	if env.Retryable != nil {
		base = append(base, withRetryable(*env.Retryable))
	}

	if httpStatus == 401 || code == "8889" {
		return newError(KindAuth, msg, base...)
	}
	if httpStatus == 429 || code == "9002" || code == "9003" {
		opts := append([]errorOpt{}, base...)
		if env.RetryAfter != nil {
			opts = append(opts, withRetryAfter(*env.RetryAfter))
		}
		return newError(KindRateLimit, msg, opts...)
	}

	kind, ok := codeToKind[code]
	if !ok {
		kind = KindApi
	}
	return newError(kind, msg, base...)
}

// Envelope 统一响应信封（定义在 client.go 的响应解析里也会用到）。
type Envelope struct {
	Code       string `json:"code"`
	Msg        string `json:"msg"`
	Data       any    `json:"data"`
	RequestID  string `json:"requestId"`
	Suggestion string `json:"suggestion"`
	Retryable  *bool  `json:"retryable"`
	RetryAfter *int   `json:"retryAfter"`
}
