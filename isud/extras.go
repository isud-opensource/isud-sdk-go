// 便捷层：人类可读格式化与 CSV 导出，给示例项目用。
package isud

import (
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

// FormatSearchResult 把检索结果渲染成人类可读摘要。
func FormatSearchResult(r *TrademarkSearchResult) string {
	if r == nil {
		return "（无结果）"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "检索: %q  法域=%s\n", r.Keyword, r.JurisdictionCode)
	fmt.Fprintf(&b, "结论: %s\n", orDefault(r.Conclusion, "（无）"))
	fmt.Fprintf(&b, "命中 %d 条 | 高险 %d / 中险 %d / 低险 %d\n",
		r.Total, r.HighRisk, r.MediumRisk, r.LowRisk)
	if len(r.Items) > 0 {
		b.WriteString("前几条：\n")
		limit := len(r.Items)
		if limit > 5 {
			limit = 5
		}
		for _, it := range r.Items[:limit] {
			level := it.RiskLevel
			if level == "" {
				level = "?"
			}
			fmt.Fprintf(&b, "  [%s] %s  %s  %s\n",
				level, it.MarkText, it.ApplicationNumber, it.OwnerName)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// ExportTrademarksCSV 把商标条目导出成 CSV。
func ExportTrademarksCSV(w io.Writer, items []TrademarkItem) error {
	cw := csv.NewWriter(w)
	header := []string{"markText", "applicationNumber", "registrationNumber", "niceClasses", "ownerName", "status", "riskLevel", "riskReason"}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, it := range items {
		row := []string{
			it.MarkText, it.ApplicationNumber, it.RegistrationNumber,
			strings.Join(it.NiceClasses, "|"), it.OwnerName, it.Status,
			it.RiskLevel, it.RiskReason,
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
