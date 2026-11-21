# 示例1：近似商标排查

输入商标名 + 法域 → 调 `searchTrademarks` → 打印风险分级与结论。

## 运行

```bash
export ISUD_API_KEY=你的密钥
go run . 优标宝 CN
```

演示点：key 从环境变量读、信封解析、错误友好提示、扣点回读。
