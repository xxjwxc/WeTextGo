# WeTextGo

[WeTextProcessing](https://github.com/wenet-e2e/WeTextProcessing) 文本归一化（TN）的 Go 实现：
把待合成文本里的数字、日期、时间、分数、百分比、金额、单位等改写成可直接朗读的自然语言，
供 TTS / 播报 / 语音交互使用。

- **纯 Go 运行**：模型是 WeTextProcessing 编译好的 FST（tagger + verbalizer），
  已随包内嵌，只用标准库解析，不依赖 Python、OpenFST 动态库或 CGO。
- **开箱即用**：`wetext.New()` 不需要任何参数，不读外部配置文件。
- **只提供 API**：没有 gRPC、etcd、nacos、gin 这些服务框架依赖，
  核心包只依赖 `gopkg.in/yaml.v3`。
- **并发安全**：模型按语种懒加载，批量调用按 CPU 核数并行。

支持语种：zh / en / de / es / fr / it / hi / ar。

## 安装

```bash
go get github.com/xxjwxc/WeTextGo
```

要求 Go 1.21+。

## 快速开始

```go
package main

import (
	"fmt"
	"log"

	"github.com/xxjwxc/WeTextGo"
)

func main() {
	n, err := wetext.New()
	if err != nil {
		log.Fatal(err)
	}
	defer n.Close()

	fmt.Println(n.Normalize("2024年5月11日，负载率87.5%"))
	// 二零二四年五月十一日，负载率百分之八十七点五

	fmt.Println(n.NormalizeLang("3.14", "en"))
	// three point one four
}
```

一次性调用可以用包级函数（内部复用全局默认实例）：

```go
out := wetext.Normalize("500kV变压器")   // 五百千伏变压器
```

## API

```go
// 构造
n, err := wetext.New(opts ...Option)     // 默认内嵌模型 + 默认规则
n := wetext.MustNew(opts ...Option)      // 失败 panic，便于包级变量初始化
n := wetext.Default()                    // 全局默认实例，可能为 nil

// 归一化
n.Normalize(text string) string                        // 语种自动判定
n.NormalizeLang(text, lang string) string              // 指定语种
n.NormalizeWith(text string, o CallOptions) string     // 语种 + 调用方热正则
n.Batch(texts []string, o CallOptions) []Result        // 批量，多协程并行
n.NormalizeOrig(texts []string) []Result               // 批量并保留原文

n.Langs() []string                       // 当前可用的语种
n.Close() error                          // 停止规则文件热加载，可重复调用

// 单次调用选项
type CallOptions struct {
	Lang string      // zh/en/de/...；空则按字符集自动判定
	Hot  []HotRule   // 先于所有内置规则执行的热正则
}

type HotRule struct{ Pattern, Replace string }   // Go RE2 语法
type Result struct{ Orig, Text string }
```

语种自动判定规则：含汉字 -> `zh`，阿拉伯文 -> `ar`，天城文 -> `hi`，其余 -> `en`。
纯数字、纯符号文本会走英文引擎，需要中文读法时请显式传 `Lang: "zh"`。

## 归一化流水线

```
调用方热正则 -> 号码按位读(中文) -> 词表/单位映射(中文) 或 货币符号(其它语种)
             -> WeTextProcessing(FST) -> 后置正则 -> 大写缩写拆分
```

- **WeTextProcessing 是主引擎**：数字/日期/时间/分数/百分比/金额等规则由 FST 承担。
- **补充规则**（默认来自内嵌的 `assets/norm_rules.yaml`）负责引擎覆盖不到的部分：
  领域词（`985`）、电力单位（`kV/MW/kWh`）、电话号码格式（`(0527)84354120` 按位读）、
  货币符号，以及修正读法的后置正则（`减 -> 至`、字母连字符 -> `杠`）。
- 模型缺失、语种不支持或某句话走不通 FST 时保留原文，调用方不会拿到半截结果。

### 自定义规则

改用外置规则文件（每 1 秒检查一次文件变更，自动热加载，无需重启）：

```go
n, err := wetext.New(
	wetext.WithRulesFile("conf/norm_rules.yaml"),
	wetext.WithWatchInterval(time.Second), // 传 0 关闭热加载
)
```

或直接在代码里指定：

```go
n, err := wetext.New(wetext.WithRuleSet(wetext.RuleSet{
	Mapping: map[string]string{"985": "九八五"},
	Unit:    map[string]string{"kV": "千伏"},
	Post: []wetext.PostRule{{
		Name:    "数字区间减号转至",
		Lang:    "zh",
		Pattern: `([0-9])减([0-9])`,
		Replace: `${1}至${2}`,
	}},
}))
```

也可以只保留引擎规则：

```go
n := wetext.MustNew(wetext.WithoutRules())
```

规则字段速查（完整示例见 `assets/norm_rules.yaml`）：

| 字段       | 作用                                            | 生效语种 |
| ---------- | ----------------------------------------------- | -------- |
| `mapping`  | 词/特号直接换成读法                             | zh       |
| `unit`     | 单位读法，仅当前面是数字时生效                  | zh       |
| `currency` | 货币符号读法：语种 -> 符号 -> 读法              | 多语种   |
| `phone`    | 号码正则，命中后整段数字逐位读                  | zh       |
| `post`     | 后置正则，按顺序执行，可带 `lang` / `guard`     | 可指定   |

> `post.pattern` / `phone` 使用 Go RE2 语法，不支持 `(?<=)` `(?=)` 这类前后向断言，
> 需要上下文时请改用捕获组，例如 `(?<!\d)5` 改写为 `(^|[^\d])5`。

## HTTP 服务（可选）

不想引入 Go 依赖时，可以用 `cmd/wetext-http` 起一个只用标准库的 JSON 服务：

```bash
go run ./cmd/wetext-http -addr :8080
# 可选：-model assets -rules assets/norm_rules.yaml

curl -s localhost:8080/normalize -d '{"text":["500kV变压器","3.14"],"lang":"zh"}'
# {"results":[{"orig":"500kV变压器","text":"五百千伏变压器"},{"orig":"3.14","text":"三点一四"}]}
```

请求体：`{"text":["..."],"lang":"zh","hotRegex":[["pattern","replace"]]}`。
另有 `GET /langs`、`GET /healthz`。

## 模型

运行期模型在 `assets/`，格式为 `.wtfst`（自定义紧凑二进制 + gzip），
Go 侧只做顺序读取。内容来自 WeTextProcessing 的预编译 FST（Apache-2.0）。

改了规则或升级 WTP 版本后重新生成：

```bash
python -m venv .venv
.venv/bin/pip install wetext kaldifst
.venv/bin/python tools/wetext/export_model.py            # 全部语种
.venv/bin/python tools/wetext/export_model.py --lang zh,en
```

用自建 WTP 规则重建 FST（需要 docker + pynini）：

```bash
tools/wetext/build.sh en,de        # 产出 .fst 并转成 .wtfst
```

## 测试

```bash
go test ./...
go test -bench . -run '^$' ./...
```

`internal/fst/testdata/golden.json` 是官方 `wetext` 包逐阶段（tagger / reorder /
verbalizer）的输出，用来保证 Go 引擎与官方实现一致。语料增加后重新生成：

```bash
.venv/bin/python tools/wetext/gen_golden.py
```

## 目录

```
assets/                 内嵌模型（.wtfst）与默认规则 norm_rules.yaml
assets.go               go:embed 入口
wetext.go               Normalizer 与对外 API
options.go              Option / CallOptions / RuleSet
pipeline.go             归一化流水线、语种判定、热正则
rules.go                补充规则编译与热加载
phone.go                号码按位读
currency.go             货币符号
postproc.go             后置处理（缩写拆分、年份按位读）
internal/fst/           FST 读取、最短路径、token 重排、语种引擎
cmd/wetext-http/        可选的 HTTP 服务（标准库）
tools/wetext/           模型导出、黄金语料生成、自建规则构建
```

## 已知边界

- 只实现 TN（文本 -> 读法）。ITN（读法 -> 文本）需要反向模型，当前未包含。
- 自动语种判定只按字符集区分 zh/ar/hi，其余拉丁字母文本默认按英文处理；
  德/西/法/意请显式传 `Lang`。
- 后置「大写缩写拆分」会把连续的 2~5 个纯大写字母逐字母拆开（`IGBT` -> `I G B T`），
  如果业务不需要，去掉该行为的规则请自行裁剪源码或在后置规则里还原。

## 许可证

Apache License 2.0，见 [LICENSE](LICENSE)。模型来自
[wenet-e2e/WeTextProcessing](https://github.com/wenet-e2e/WeTextProcessing)（Apache-2.0）。
