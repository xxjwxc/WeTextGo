// Package wetext 是 WeTextProcessing(https://github.com/wenet-e2e/WeTextProcessing)
// 文本归一化（TN）的 Go 实现：把文本里的数字、日期、时间、分数、百分比、金额、
// 单位等改写成可直接朗读的自然语言，供 TTS 使用。
//
// 模型是 WeTextProcessing 编译好的 FST（tagger + verbalizer），已随包内嵌，
// 只用标准库解析，不依赖 Python、OpenFST 动态库或 CGO。开箱即用：
//
//	n, err := wetext.New()
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Println(n.Normalize("2024年5月11日，负载率87.5%"))
//	// 二零二四年五月十一日，负载率百分之八十七点五
//
// 指定语种、批量处理、调用方热正则：
//
//	out := n.NormalizeLang("3.14", "en")     // three point one four
//	res := n.Batch(texts, wetext.CallOptions{Lang: "zh"})
//	out = n.NormalizeWith("500KV", wetext.CallOptions{
//		Hot: []wetext.HotRule{{Pattern: "(?i)kv", Replace: "千伏"}},
//	})
//
// 支持 zh/en/de/es/fr/it/hi/ar 八语种。Normalizer 并发安全，模型按语种懒加载，
// 某个语种模型缺失或某句话走不通 FST 时保留原文，不会让调用方拿到半截结果。
//
// 除引擎外还有一层可选的补充规则（assets/norm_rules.yaml）：领域词（如 985）、
// 电力单位（kV/MW/kWh）、号码按位读、货币符号、以及修正读法的后置正则。
// 可用 WithRulesFile 换成自己的文件（改完自动热加载），或用 WithRuleSet 在代码里指定。
package wetext

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/xxjwxc/WeTextGo/internal/fst"
)

// Normalizer 文本归一化器，内部持有懒加载的语种引擎与规则快照，可并发调用。
type Normalizer struct {
	reg         *fst.Registry
	rules       *ruleStore
	logger      *slog.Logger
	defaultLang string
}

// New 创建归一化器。默认加载包内嵌模型与默认规则，无需任何参数。
func New(opts ...Option) (*Normalizer, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}

	reg, err := fst.OpenFS(cfg.fsys, cfg.dir)
	if err != nil {
		return nil, err
	}

	rules, err := newRuleStore(&cfg)
	if err != nil {
		return nil, err
	}

	return &Normalizer{
		reg:         reg,
		rules:       rules,
		logger:      cfg.logger,
		defaultLang: cfg.defaultLang,
	}, nil
}

// Langs 返回可用的语种
func (n *Normalizer) Langs() []string { return n.reg.Langs() }

// Normalize 归一化单条文本，语种自动判定。
func (n *Normalizer) Normalize(text string) string {
	return n.NormalizeWith(text, CallOptions{})
}

// NormalizeLang 归一化单条文本并指定语种。
func (n *Normalizer) NormalizeLang(text, lang string) string {
	return n.NormalizeWith(text, CallOptions{Lang: lang})
}

// NormalizeWith 归一化单条文本，可带语种与调用方热正则。
func (n *Normalizer) NormalizeWith(text string, o CallOptions) string {
	return n.normalizeOne(text, o, n.rules.Current())
}

// NormalizeOrig 归一化一批文本并保留原文，语种自动判定。
func (n *Normalizer) NormalizeOrig(texts []string) []Result {
	return n.Batch(texts, CallOptions{})
}

// Batch 归一化一批文本。文本之间互不相关，会按 CPU 核数并行处理。
func (n *Normalizer) Batch(texts []string, o CallOptions) []Result {
	out := make([]Result, len(texts))
	rules := n.rules.Current()

	workers := parallelWorkers(len(texts))
	if workers <= 1 {
		for i, t := range texts {
			out[i] = Result{Orig: t, Text: n.normalizeOne(t, o, rules)}
		}
		return out
	}

	var next atomic.Int64
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for {
				i := int(next.Add(1)) - 1
				if i >= len(texts) {
					return
				}
				out[i] = Result{Orig: texts[i], Text: n.normalizeOne(texts[i], o, rules)}
			}
		}()
	}
	wg.Wait()

	return out
}

// Close 停止规则文件热加载。模型与规则本身无需释放，可重复调用。
func (n *Normalizer) Close() error {
	n.rules.Close()
	return nil
}

// logError 只在调用方显式传了 logger 时输出，库自身不打印任何东西。
func (n *Normalizer) logError(msg string, args ...any) {
	if n.logger != nil {
		n.logger.Error(msg, args...)
	}
}

// transduce 调用语种引擎；模型缺失或没有路径时保留原文，
// 保证服务可用性不依赖模型文件。
func (n *Normalizer) transduce(text, lang string) string {
	eng, ok := n.reg.Engine(lang)
	if !ok {
		return text
	}
	out, ok := eng.Normalize(text)
	if !ok {
		return text
	}

	return out
}

// 包级便捷函数：走全局默认实例（内嵌模型 + 默认规则），首次调用时初始化。
// 需要自定义模型、规则或日志时请改用 New。
var (
	defaultOnce sync.Once
	defaultN    *Normalizer
)

// Default 返回全局默认归一化器，初始化失败时返回 nil。
func Default() *Normalizer {
	defaultOnce.Do(func() {
		n, err := New()
		if err != nil {
			return
		}
		defaultN = n
	})

	return defaultN
}

// Normalize 用全局默认实例归一化单条文本，语种自动判定。
func Normalize(text string) string {
	return NormalizeLang(text, "")
}

// NormalizeLang 用全局默认实例归一化单条文本并指定语种。
func NormalizeLang(text, lang string) string {
	n := Default()
	if n == nil {
		return text
	}
	return n.NormalizeLang(text, lang)
}

// Batch 用全局默认实例归一化一批文本。
func Batch(texts []string, o CallOptions) []Result {
	n := Default()
	if n == nil {
		out := make([]Result, len(texts))
		for i, t := range texts {
			out[i] = Result{Orig: t, Text: t}
		}
		return out
	}
	return n.Batch(texts, o)
}

// MustNew 创建归一化器，失败时 panic，便于在 init/包级变量里使用。
func MustNew(opts ...Option) *Normalizer {
	n, err := New(opts...)
	if err != nil {
		panic(fmt.Sprintf("wetext: %v", err))
	}
	return n
}
