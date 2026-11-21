// 退避策略 —— 与 Python/TypeScript 行为对齐，见 SDK-DESIGN.md §4。
package isud

import (
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

const (
	// MaxWaitSeconds 单次等待上限：即便服务端 Retry-After 很长，也不长时间阻塞用户
	MaxWaitSeconds = 60
	baseBackoff    = 1.0
	maxBackoff     = 30.0
)

// ParseRetryAfter 解析 Retry-After 头：整数秒 或 HTTP-date。无法解析返回 nil。
func ParseRetryAfter(value string) *int {
	if value == "" {
		return nil
	}
	value = trimSpace(value)
	// 整数秒
	if n, err := strconv.Atoi(value); err == nil && n >= 0 {
		return &n
	}
	// HTTP-date (RFC 7231 / RFC 1123)
	for _, layout := range []string{
		http.TimeFormat,
		time.RFC1123,
		"Mon, 02 Jan 2006 15:04:05 GMT",
	} {
		if t, err := time.Parse(layout, value); err == nil {
			secs := int(t.Sub(time.Now()).Seconds())
			if secs < 0 {
				secs = 0
			}
			return &secs
		}
	}
	return nil
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

// ExponentialBackoff 第 attempt 次（从 0 起）指数退避秒数，带抖动防惊群。
func ExponentialBackoff(attempt int) float64 {
	raw := math.Min(baseBackoff*math.Pow(2, float64(attempt)), maxBackoff)
	return raw * (0.5 + rand.Float64()*0.5)
}

// ResolveWait 决定本次重试等待秒数。
// 优先级：信封 retryAfter > RateLimitError.RetryAfter > 指数退避。实际等待不超过 MaxWaitSeconds。
func ResolveWait(err *SdkError, attempt int) float64 {
	var wait float64
	if err.Kind == KindRateLimit && err.RetryAfter != nil {
		wait = float64(*err.RetryAfter)
	} else {
		wait = ExponentialBackoff(attempt)
	}
	if wait < 0 {
		wait = 0
	}
	if wait > MaxWaitSeconds {
		wait = MaxWaitSeconds
	}
	return wait
}

// DoRequest 实际发起单次请求的函数签名。
type DoRequest func() (any, error)

// ShouldRetry 判定某异常是否可重试（幂等安全由外层保证）。
type ShouldRetry func(*SdkError) bool

// WithRetry 通用重试执行器。sleep/onRetry 可注入以便测试。
func WithRetry(
	fn DoRequest,
	maxRetries int,
	shouldRetry ShouldRetry,
	sleep func(time.Duration),
	onRetry func(*SdkError, int, time.Duration),
) (any, error) {
	attempt := 0
	for {
		result, err := fn()
		if err == nil {
			return result, nil
		}
		var se *SdkError
		if !asSdkError(err, &se) {
			// 非 SDK 异常：包装成 NetworkError 再判断（一般不该发生）
			se = newError(KindNetwork, err.Error())
		}
		if attempt >= maxRetries || !se.Retryable || (shouldRetry != nil && !shouldRetry(se)) {
			return nil, err
		}
		wait := time.Duration(ResolveWait(se, attempt) * float64(time.Second))
		if onRetry != nil {
			onRetry(se, attempt+1, wait)
		}
		if sleep != nil {
			sleep(wait)
		}
		attempt++
	}
}
