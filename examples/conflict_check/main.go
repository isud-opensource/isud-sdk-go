// 示例1：近似商标排查 —— 输入商标名 + 法域，返回风险分级与结论。
//
// 三步跑通：
//   1. go mod init（本仓库已有 go.mod，clone 后无需）
//   2. export ISUD_API_KEY=你的密钥（控制台领取，注册即送调用次数）
//   3. go run ./examples/conflict_check 优标宝 CN
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/l-zhiyuan/isud-sdk-go/isud"
)

func main() {
	mark := "优标宝"
	jurisdiction := "CN"
	if len(os.Args) > 1 && os.Args[1] != "" {
		mark = os.Args[1]
	}
	if len(os.Args) > 2 && os.Args[2] != "" {
		jurisdiction = os.Args[2]
	}

	client, err := isud.NewClient()
	if err != nil {
		fail(err)
	}

	result, err := client.SearchTrademarks(context.Background(), isud.TrademarkSearchInput{
		JurisdictionCode: jurisdiction,
		Keyword:          mark,
		SaveReport:       false,
	})
	if err != nil {
		fail(err)
	}

	fmt.Println(isud.FormatSearchResult(result))
	if client.LastMeta.CreditsCharged != nil {
		fmt.Printf("\n本次扣点 %d，余额 %v（requestId=%s）\n",
			*client.LastMeta.CreditsCharged, isud.DerefInt(client.LastMeta.CreditsRemaining),
			client.LastMeta.RequestID)
	}
}

// fail 友好报错，不裸 stack。
func fail(err error) {
	if se, ok := err.(*isud.SdkError); ok {
		fmt.Fprintln(os.Stderr, se.Friendly())
	} else {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
	}
	os.Exit(1)
}
