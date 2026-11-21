package isud

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// roundTripFunc 让函数实现 http.RoundTripper，便于 mock。
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func resp(status int, body string, headers map[string]string) *http.Response {
	h := http.Header{}
	for k, v := range headers {
		h.Set(k, v)
	}
	return &http.Response{
		StatusCode: status,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func newTestClient(t *testing.T, rt roundTripFunc) *Client {
	t.Helper()
	c, err := NewClient(
		WithAPIKey("lg_ip_test_fake_key"),
		WithMaxRetries(0),
		WithHTTPClient(&http.Client{Transport: rt}),
	)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func envJSON(code string, data any, extra map[string]any) string {
	m := map[string]any{"code": code, "msg": "test msg", "data": data}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func TestSuccessEnvelope(t *testing.T) {
	body := envJSON("0000", map[string]any{
		"jurisdictionCode": "CN", "keyword": "优标宝", "total": 3,
		"highRisk": 1, "items": []any{map[string]any{"markText": "优标宝", "riskLevel": "high"}},
	}, nil)
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("X-API-Key") != "lg_ip_test_fake_key" {
			t.Errorf("missing api key header")
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Errorf("missing idempotency key on search POST")
		}
		return resp(200, body, map[string]string{
			"X-Credits-Charged": "10", "X-Request-Id": "req-123",
		}), nil
	})
	got, err := c.SearchTrademarks(context.Background(), TrademarkSearchInput{
		JurisdictionCode: "CN", Keyword: "优标宝",
	})
	if err != nil {
		t.Fatalf("SearchTrademarks: %v", err)
	}
	if got.Total != 3 || len(got.Items) != 1 || got.Items[0].MarkText != "优标宝" {
		t.Errorf("unexpected result: %+v", got)
	}
	if c.LastMeta.CreditsCharged == nil || *c.LastMeta.CreditsCharged != 10 {
		t.Errorf("credits meta missing")
	}
	if c.LastMeta.RequestID != "req-123" {
		t.Errorf("request id meta missing: %q", c.LastMeta.RequestID)
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		status   int
		code     string
		wantKind ErrorKind
	}{
		{401, "8889", KindAuth},
		{200, "8889", KindAuth},
		{429, "9002", KindRateLimit},
		{200, "9003", KindRateLimit},
		{200, "9001", KindValidation},
		{200, "0002", KindCapability},
		{200, "0001", KindApi},
	}
	for _, tc := range cases {
		c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
			return resp(tc.status, envJSON(tc.code, nil, nil), nil), nil
		})
		_, err := c.GetUsage(context.Background())
		if err == nil {
			t.Errorf("status=%d code=%s: expected error", tc.status, tc.code)
			continue
		}
		if !IsKind(err, tc.wantKind) {
			t.Errorf("status=%d code=%s: want %s got %v", tc.status, tc.code, tc.wantKind, err)
		}
	}
}

func TestRetryOn429(t *testing.T) {
	calls := 0
	c, err := NewClient(
		WithAPIKey("lg_ip_test_fake_key"),
		WithMaxRetries(2),
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if calls < 3 {
				return resp(429, envJSON("9002", nil, map[string]any{"retryAfter": 0}), nil), nil
			}
			return resp(200, envJSON("0000", map[string]any{"balance": 100}, nil), nil), nil
		})}),
	)
	if err != nil {
		t.Fatal(err)
	}
	// 用注入的 sleep（no-op）避免真等待
	got, err := WithRetry(
		func() (any, error) { return c.GetUsage(context.Background()) },
		2,
		func(se *SdkError) bool { return true },
		func(time.Duration) {}, nil,
	)
	if err != nil {
		t.Fatalf("expected success after retries: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
	if got == nil {
		t.Errorf("expected non-nil result")
	}
}

func TestLocalValidation(t *testing.T) {
	// 本地校验失败时不应发请求：mock 里直接报错
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		t.Errorf("should not send request for invalid local params")
		return resp(200, envJSON("0000", map[string]any{}, nil), nil), nil
	})
	// size 超范围
	_, err := c.ListTrademarks(context.Background(), TrademarkListInput{Size: 101})
	if err == nil || !IsKind(err, KindValidation) {
		t.Errorf("size=101 should be ValidationError, got %v", err)
	}
	// keyword+owner 都空
	_, err = c.SearchTrademarks(context.Background(), TrademarkSearchInput{JurisdictionCode: "CN"})
	if err == nil || !IsKind(err, KindValidation) {
		t.Errorf("empty keyword/owner should be ValidationError, got %v", err)
	}
	// requestId 超长
	_, err = c.QueryRequestLog(context.Background(), strings.Repeat("x", 65))
	if err == nil || !IsKind(err, KindValidation) {
		t.Errorf("long requestId should be ValidationError, got %v", err)
	}
}

func TestSizeZeroDefaults(t *testing.T) {
	// size=0 应默认成合法值并正常发请求
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		q := r.URL.Query()
		if r.Method == http.MethodGet && q.Get("size") != "10" {
			t.Errorf("GET size should default to 10, got %q", q.Get("size"))
		}
		return resp(200, envJSON("0000", map[string]any{"current": 1, "size": 10, "records": []any{}}, nil), nil), nil
	})
	_, err := c.ListTrademarks(context.Background(), TrademarkListInput{Size: 0})
	if err != nil {
		t.Errorf("size=0 should default, got %v", err)
	}
}

func TestMCPBareResponse(t *testing.T) {
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Path, "/api/v1/mcp/health") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		return resp(200, `{"status":"ok","toolCount":13}`, nil), nil
	})
	h, err := c.GetMCPHealth(context.Background())
	if err != nil {
		t.Fatalf("GetMCPHealth: %v", err)
	}
	if h.Status != "ok" || h.ToolCount != 13 {
		t.Errorf("unexpected health: %+v", h)
	}
}

func TestIdempotencyKeyCustom(t *testing.T) {
	wantKey := "my-business-order-001"
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Idempotency-Key"); got != wantKey {
			t.Errorf("idempotency key = %q, want %q", got, wantKey)
		}
		return resp(200, envJSON("0000", map[string]any{"total": 0}, nil), nil), nil
	})
	_, err := c.SearchTrademarks(context.Background(), TrademarkSearchInput{
		JurisdictionCode: "CN", Keyword: "x", IdempotencyKey: wantKey,
	})
	if err != nil {
		t.Fatalf("SearchTrademarks: %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := ParseRetryAfter("30"); got == nil || *got != 30 {
		t.Errorf("int retry-after failed: %v", got)
	}
	future := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	if got := ParseRetryAfter(future); got == nil || *got < 3 || *got > 5 {
		t.Errorf("http-date retry-after failed: %v", got)
	}
	if got := ParseRetryAfter("garbage"); got != nil {
		t.Errorf("garbage should return nil, got %v", got)
	}
}
