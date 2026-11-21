# 示例2：批量风险筛查

读 CSV（商标名列表）→ 逐个检索 → 汇总 high/medium/low → 导出 CSV。

## 运行

```bash
export ISUD_API_KEY=你的密钥
go run . marks.sample.csv out.csv CN
```

演示点：幂等键（业务单号，重跑不重复扣点）、429 退避、批量场景用量查询。
