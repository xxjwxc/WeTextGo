// 号码按位读补充规则。
//
// WeTextProcessing 的中文 tagger 不认识「(0527)84354120」这种区号括号写法，
// 会把它拆成 math{0} math{527} math{84354120}，读成「零」「五百二十七」
// 「八千四百三十五万四千一百二十」。这里在引擎之前把命中串的数字改成逐位汉字，
// 引擎看到汉字就会原样保留，从而读出号码本来的读法。

package wetext

import (
	"log/slog"
	"regexp"
	"strings"
	"unicode"

	"github.com/xxjwxc/WeTextGo/internal/fst"
)

// phoneDigits 号码逐位读法：0 读零、1 读幺，与 WTP 读 13800138000 的读法保持一致
var phoneDigits = [...]string{"零", "幺", "二", "三", "四", "五", "六", "七", "八", "九"}

// compilePhone 编译号码正则，单个写错只跳过该条，不影响其余规则
func compilePhone(patterns []string, logger *slog.Logger) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if p == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			if logger != nil {
				logger.Error("wetext: bad phone regex, skipped", "pattern", p, "err", err)
			}
			continue
		}
		out = append(out, re)
	}

	return out
}

// spellPhone 把命中号码的数字逐位读音，非数字字符（括号、横线、空格）原样保留
func spellPhone(text string, patterns []*regexp.Regexp) string {
	if !fst.HasDigit(text) {
		return text // 没有数字就不可能命中，省掉几条复杂正则的扫描
	}

	for _, re := range patterns {
		text = re.ReplaceAllStringFunc(text, spellDigits)
	}

	return text
}

// spellDigits 逐位读一段号码：数字改汉字，括号原样保留；
// 「+」直接丢弃，数字之间的横线/空格也丢弃，避免 TTS 读出「加」「减」；
// 号码前后的分隔符（如「电话 138…」里的空格）保持原样。
func spellDigits(s string) string {
	rs := []rune(s)

	var b strings.Builder
	b.Grow(len(s) * 3)

	for i, r := range rs {
		switch {
		case r >= '0' && r <= '9':
			b.WriteString(phoneDigits[r-'0'])
		case r == '+':
			// 国际前缀，丢弃
		case r == '-' || r == '－' || unicode.IsSpace(r):
			if !inNumber(rs, i) {
				b.WriteRune(r)
			}
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

// inNumber 判断分隔符是否夹在数字中间（号码内部）
func inNumber(rs []rune, i int) bool {
	return i > 0 && i+1 < len(rs) && isDigitRune(rs[i-1]) && isDigitRune(rs[i+1])
}

func isDigitRune(r rune) bool { return r >= '0' && r <= '9' }
