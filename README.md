# isud-sdk-go (Go)

[![Go Reference](https://pkg.go.dev/badge/github.com/isud-opensource/isud-sdk-go.svg)](https://pkg.go.dev/github.com/isud-opensource/isud-sdk-go)

优标宝开放 API 的 Go SDK。零第三方运行时依赖（仅标准库 `net/http`），契约驱动。

## 安装

```bash
go get github.com/isud-opensource/isud-sdk-go
```

## 三步跑通（Time-to-Hello-World < 3 分钟）

```bash
export ISUD_API_KEY=你的密钥   # 控制台领取，注册即送调用次数
```

```go
package main

import (
    "context"
    "fmt"
    "os"
    "github.com/isud-opensource/isud-sdk-go/isud"
)

func main() {
    client, _ := isud.NewClient() // 自动读 ISUD_API_KEY
    res, err := client.SearchTrademarks(context.Background(), isud.TrademarkSearchInput{
        JurisdictionCode: "CN", Keyword: "优标宝",
    })
    if err != nil { fmt.Fprintln(os.Stderr, err.(*isud.SdkError).Friendly()); return }
    fmt.Println(isud.FormatSearchResult(res))
}
```

```
检索: "优标宝"  法域=CN
结论: 存在 1 件高近似商标，建议调整
命中 3 条 | 高险 1 / 中险 1 / 低险 1
```

## 设计

见 [`SDK-DESIGN.md`](SDK-DESIGN.md)：信封解析、6 类异常映射、Retry-After 退避、幂等键、扣点回读、分页迭代器——与 Python/TypeScript 版本逐条对齐，跨语言零学习成本。

- **生成层** `isud/models.go`：从 `openapi.yaml` 映射的数据结构的，标注 GENERATED，勿手改。
- **体验层** `isud/client.go` `errors.go` `retry.go` `extras.go`：手写，实现全部 SDK 体验。

## 9 个端点

| 方法 | HTTP |
|---|---|
| `SearchTrademarks` | POST /ip/trademark/search |
| `SearchDesigns` | POST /ip/design/search |
| `ListTrademarks` / `IterTrademarks` | GET /ip/trademark/list |
| `ListReports` | GET /ip/report/list |
| `GetWorkbenchStats` | GET /ip/workbench/stats |
| `GetUsage` | GET /ip/usage |
| `QueryRequestLog` | GET /ip/requests |
| `GetMCPHealth` | GET /mcp/health |
| `CallMCP` | POST /mcp |
| `QueryVienna` | GET /ip/vienna/query（图形检索 VCL10 / 以图搜图） |
| `QueryGazette` | GET /ip/gazette/query（CN 公告 / 全球事件） |
| `QueryReview` | GET /ip/review/query（CNIPA 评审文书 298 万） |
| `QueryIpr` | GET /ip/ipr/query（海关备案 12 万，可选接入） |

四个数据能力方法均为 GET、读类 1 点/次，接收 `url.Values` 参数，返回 `any`（原始 `data` JSON，结构随 `mode` 变化）：

```go
codes, _ := client.QueryVienna(ctx, url.Values{"mode": {"codes"}, "q": {"王冠"}})
docs, _  := client.QueryReview(ctx, url.Values{"q": {"华为"}, "size": {"10"}})
filings, _ := client.QueryIpr(ctx, url.Values{"q": {"华为"}})
```

## 示例项目

- [`examples/conflict_check`](examples/conflict_check) — 近似商标排查
- [`examples/batch_screen`](examples/batch_screen) — 批量风险筛查（幂等键 + 退避 + 导出）

## 测试

```bash
go test ./...
```

契约测试用自定义 `RoundTripper` mock HTTP 响应，不依赖真实 key、不扣点。

## 模型层同步

`openapi.yaml` 更新后，重新映射 `isud/models.go` 的 struct 字段（保持 GENERATED 标注）。字段开放枚举用 `string`，不封闭成常量。

## License

MIT
