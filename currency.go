package wetext

import (
	"regexp"
	"sort"
	"strings"
)

// 货币符号读法：WeTextProcessing 不处理 $300 / 300$ 这类写法，
// 这里在引擎之前把符号换成词并把数字留给引擎读，例如
// "300$" -> "300 dollars" -> "three hundred dollars"。

type currencyEntry struct {
	sym   string
	word  string
	front *regexp.Regexp // $300
	back  *regexp.Regexp // 300$
}

// compileCurrency 语种 -> 符号 -> 读法，长符号优先（US$ 先于 $）
func compileCurrency(m map[string]map[string]string) map[string][]currencyEntry {
	out := make(map[string][]currencyEntry, len(m))

	for lang, table := range m {
		syms := make([]string, 0, len(table))
		for sym := range table {
			if sym != "" {
				syms = append(syms, sym)
			}
		}
		sort.Slice(syms, func(i, j int) bool {
			if len(syms[i]) != len(syms[j]) {
				return len(syms[i]) > len(syms[j])
			}
			return syms[i] < syms[j]
		})

		entries := make([]currencyEntry, 0, len(syms))
		for _, sym := range syms {
			q := regexp.QuoteMeta(sym)
			entries = append(entries, currencyEntry{
				sym:   sym,
				word:  table[sym],
				front: regexp.MustCompile(q + `\s*(\d[\d.,]*)`),
				back:  regexp.MustCompile(`(\d[\d.,]*)\s*` + q),
			})
		}

		out[strings.ToLower(strings.TrimSpace(lang))] = entries
	}

	return out
}

func replaceCurrency(text string, entries []currencyEntry) string {
	for _, e := range entries {
		if !strings.Contains(text, e.sym) {
			continue
		}
		word := " " + e.word
		text = e.front.ReplaceAllString(text, "${1}"+word)
		text = e.back.ReplaceAllString(text, "${1}"+word)
		// 剩下的独立符号直接换词
		text = strings.ReplaceAll(text, e.sym, e.word)
	}

	return text
}
