package wetext_test

import (
	"fmt"
	"log"

	wetext "github.com/xxjwxc/WeTextGo"
)

// 最简用法：创建归一化器，语种自动判定。
func Example() {
	n, err := wetext.New()
	if err != nil {
		log.Fatal(err)
	}
	defer n.Close()

	fmt.Println(n.Normalize("2024年5月11日，负载率87.5%"))
	// Output: 二零二四年五月十一日，负载率百分之八十七点五
}

// 指定语种、批量处理、调用方热正则。
func ExampleNormalizer_Batch() {
	n, err := wetext.New()
	if err != nil {
		log.Fatal(err)
	}
	defer n.Close()

	res := n.Batch([]string{"3.14", "500kV变压器"}, wetext.CallOptions{Lang: "zh"})
	for _, r := range res {
		fmt.Printf("%s -> %s\n", r.Orig, r.Text)
	}

	fmt.Println(n.NormalizeWith("今天 500KV", wetext.CallOptions{
		Lang: "zh",
		Hot:  []wetext.HotRule{{Pattern: "(?i)kv", Replace: "千伏"}},
	}))
	// Output:
	// 3.14 -> 三点一四
	// 500kV变压器 -> 五百千伏变压器
	// 今天 五百千伏
}

// 只保留 WeTextProcessing 的 FST 规则，不要补充规则。
func ExampleWithoutRules() {
	n := wetext.MustNew(wetext.WithoutRules())
	defer n.Close()

	fmt.Println(n.NormalizeLang("985", "zh"))
	// Output: 九百八十五
}
