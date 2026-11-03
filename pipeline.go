package wetext

import (
	"regexp"
	"runtime"
	"strings"
	"sync"

	"github.com/xxjwxc/WeTextGo/internal/fst"
)

// 归一化流水线（顺序即优先级）：
//  1. 调用方热正则
//  2. 号码逐位读（中文，如 (0527)84354120）
//  3. 词表/单位映射（中文读法专用，如 985 -> 九八五、500kV -> 500[千伏]）
//  4. WeTextProcessing 引擎（数字/日期/时间/分数/百分比/金额等全部交给 FST 规则）
//  5. 后置正则（yaml 可配，如 减 -> 至、字母连字符 -> 杠）
//  6. 中文大写缩写拆分

var (
	reHan        = regexp.MustCompile(`\p{Han}`)
	reArabic     = regexp.MustCompile(`\p{Arabic}`)
	reDevanagari = regexp.MustCompile(`\p{Devanagari}`)
)

// 单位映射用方括号保护读法，引擎跑完再统一去掉
var brackets = strings.NewReplacer("[", "", "]", "")

// normalizeOne 单条归一化
func (n *Normalizer) normalizeOne(text string, o CallOptions, rules *compiledRules) (res string) {
	res = text
	defer func() {
		if err := recover(); err != nil {
			n.logError("wetext: panic while normalizing, return origin text", "panic", err)
			res = text
		}
	}()

	lang := o.Lang
	if lang == "" {
		lang = n.defaultLang
	}
	code := detectLang(lang, res)

	res = applyHot(res, o.Hot) // 1. 调用方热正则

	if code == "zh" {
		res = spellPhone(res, rules.phone) // 号码先按位读，避免引擎把区号当数量
		res = rules.mapping.apply(res)     // 2. 词表映射（中文读法）
		res = rules.unit.apply(res)        // 3. 单位映射（中文读法）

		// 整句只是一个年份时按位读，引擎看到汉字就会原样保留
		if year, ok := standaloneYear(res); ok {
			res = year
		}
	} else {
		res = replaceCurrency(res, rules.currency[code]) // 3. 货币符号先换成词，数字交给引擎
	}

	res = n.transduce(res, code) // 4. WeTextProcessing

	res = applyPost(res, rules.post, code) // 5. 后置正则
	res = splitAbbr(res)                   // 6. 大写缩写逐字母拆开

	return brackets.Replace(res)
}

// detectLang 归一化语种标识。lang 为空时按文本字符集判定，
// 非中文一律回落到英文引擎，保证数字至少能被读出来。
func detectLang(lang, text string) string {
	code := fst.NormalizeLang(lang)
	if code != "" {
		return code
	}

	switch {
	case reHan.MatchString(text):
		return "zh"
	case reArabic.MatchString(text):
		return "ar"
	case reDevanagari.MatchString(text):
		return "hi"
	}

	return "en"
}

// applyHot 调用方热正则替换。正则编译结果按 pattern 缓存，
// 否则同一批文本每条都要重新编译一遍。
func applyHot(text string, hot []HotRule) string {
	for _, g := range hot {
		if g.Pattern == "" {
			continue
		}
		re, ok := compileHot(g.Pattern)
		if !ok {
			continue
		}
		text = re.ReplaceAllString(text, g.Replace)
	}

	return text
}

var (
	hotMu    sync.Mutex
	hotCache = map[string]*regexp.Regexp{}
	hotBad   = map[string]bool{}
)

// hotCacheMax 限制缓存规模，避免调用方每次传新 pattern 时无界增长
const hotCacheMax = 256

func compileHot(pattern string) (*regexp.Regexp, bool) {
	hotMu.Lock()
	defer hotMu.Unlock()

	if re, ok := hotCache[pattern]; ok {
		return re, true
	}
	if hotBad[pattern] {
		return nil, false
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		if len(hotBad) < hotCacheMax {
			hotBad[pattern] = true
		}
		return nil, false
	}
	if len(hotCache) < hotCacheMax {
		hotCache[pattern] = re
	}

	return re, true
}

// applyPost 后置规则，按配置顺序执行。lang 非空时只对匹配语种生效，guard 命中才生效。
func applyPost(text string, rules []postEntry, code string) string {
	for _, p := range rules {
		if p.lang != "" && p.lang != code {
			continue
		}
		if len(p.guard) > 0 && !hasAny(text, p.guard) {
			continue
		}
		text = p.re.ReplaceAllString(text, p.rep)
	}

	return text
}

func hasAny(text string, words []string) bool {
	for _, w := range words {
		if w != "" && strings.Contains(text, w) {
			return true
		}
	}

	return false
}

// parallelWorkers 文本太少时没必要开协程，调度开销反而不划算
func parallelWorkers(n int) int {
	if n < minParallelTexts {
		return 1
	}
	w := runtime.GOMAXPROCS(0)
	if w > n {
		w = n
	}

	return w
}

const minParallelTexts = 4
