package wetext

import (
	"regexp"
	"strings"
)

// 纯大写缩写（2-5 个字母）逐字母拆开，便于下游逐字母朗读：IGBT -> I G B T。
// 只匹配连续大写字母，不影响正常单词；超过 5 个字母（如 INTERNATIONAL）不拆。
var reAbbr = regexp.MustCompile(`(^|[^A-Za-z])([A-Z]{2,5})([^A-Za-z]|$)`)

func splitAbbr(text string) string {
	return reAbbr.ReplaceAllStringFunc(text, func(s string) string {
		m := reAbbr.FindStringSubmatch(s)
		return m[1] + strings.Join(strings.Split(m[2], ""), " ") + m[3]
	})
}

var cnDigits = [...]string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}

// standaloneYear 整句就是一个 1900-2099 的四位年份时按位读（2024 -> 二零二四）。
// WTP 会把它当成数量读成「两千零二十四」，年份播报场景需要逐位。
func standaloneYear(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 4 {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return "", false
		}
	}

	if s < "1900" || s > "2099" {
		return "", false
	}

	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteString(cnDigits[int(s[i]-'0')])
	}

	return b.String(), true
}
