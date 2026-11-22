// 示例2：批量风险筛查 —— 读 CSV（商标名）→ 逐个检索 → 汇总风险 → 导出 CSV。
//
// 用法：
//   export ISUD_API_KEY=你的密钥
//   go run ./examples/batch_screen marks.csv out.csv CN
//
// 演示点：幂等键（重跑不重复扣点）、429 退避、分页、批量用量查询。
package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/isud-opensource/isud-sdk-go/isud"
)

type summary struct {
	mark   string
	level  string
	total  int
	reason string
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "用法: go run ./examples/batch_screen <输入.csv> <输出.csv> [法域]")
		os.Exit(2)
	}
	inPath, outPath := os.Args[1], os.Args[2]
	jurisdiction := "CN"
	if len(os.Args) > 3 {
		jurisdiction = os.Args[3]
	}

	marks, err := readMarks(inPath)
	if err != nil {
		fail(err)
	}
	client, err := isud.NewClient()
	if err != nil {
		fail(err)
	}
	// 展示退避过程（可选）
	client.OnRetry = func(se *isud.SdkError, n int, d time.Duration) {
		fmt.Fprintf(os.Stderr, "  限流，第 %d 次重试，等待 %v ...\n", n, d.Round(time.Millisecond))
	}

	var rows []summary
	for i, mark := range marks {
		fmt.Printf("[%d/%d] 筛查 %q ... ", i+1, len(marks), mark)
		res, err := client.SearchTrademarks(context.Background(), isud.TrademarkSearchInput{
			JurisdictionCode: jurisdiction,
			Keyword:          mark,
			// 业务单号作幂等键：重跑同一批不重复扣点
			IdempotencyKey: fmt.Sprintf("batch-%s-%04d", jurisdiction, i+1),
		})
		if err != nil {
			fmt.Println("失败")
			fmt.Fprintln(os.Stderr, "  "+err.Error())
			rows = append(rows, summary{mark: mark, level: "error", reason: err.Error()})
			continue
		}
		level := "low"
		if res != nil {
			if res.HighRisk > 0 {
				level = "high"
			} else if res.MediumRisk > 0 {
				level = "medium"
			}
		}
		total := 0
		if res != nil {
			total = res.Total
		}
		fmt.Printf("%s（%d 条）\n", level, total)
		rows = append(rows, summary{mark: mark, level: level, total: total})
	}

	if err := writeCSV(outPath, rows); err != nil {
		fail(err)
	}
	usage, err := client.GetUsage(context.Background())
	if err == nil {
		fmt.Printf("\n完成。余额 %d，今日调用 %d。结果已写入 %s\n",
			usage.Balance, usage.Today.Calls, outPath)
	} else {
		fmt.Printf("\n完成。结果已写入 %s\n", outPath)
	}
}

func readMarks(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开输入文件失败: %w", err)
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	records, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("读取 CSV 失败: %w", err)
	}
	var marks []string
	for _, row := range records {
		if len(row) == 0 {
			continue
		}
		m := strings.TrimSpace(row[0])
		if m == "" || strings.HasPrefix(m, "#") {
			continue
		}
		marks = append(marks, m)
	}
	return marks, nil
}

func writeCSV(path string, rows []summary) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建输出文件失败: %w", err)
	}
	defer f.Close()
	cw := csv.NewWriter(f)
	if err := cw.Write([]string{"mark", "riskLevel", "total", "reason"}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := cw.Write([]string{r.mark, r.level, strconv.Itoa(r.total), r.reason}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func fail(err error) {
	if se, ok := err.(*isud.SdkError); ok {
		fmt.Fprintln(os.Stderr, se.Friendly())
	} else {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
	}
	os.Exit(1)
}
