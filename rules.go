package wetext

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// compiledRules 编译后的只读规则集，整体替换以保证并发安全
type compiledRules struct {
	version  int
	mapping  *replaceSet
	unit     *replaceSet
	currency map[string][]currencyEntry
	phone    []*regexp.Regexp
	post     []postEntry
}

// replaceSet 词表替换集合。
// 能写成正则的 key 合并成一条正则（长 key 在前），整段文本只扫一遍；
// 写不成正则的 key（含中文/符号）退化为子串替换。
type replaceSet struct {
	res   []*regexp.Regexp
	vals  map[string]string // 命中的原词 -> 读法，纯字母 key 额外存小写形式
	unit  bool              // 单位规则：读法用方括号保护，交给数字规则统一转换
	plain []mapEntry
}

type mapEntry struct {
	key string
	val string
}

type postEntry struct {
	name  string
	lang  string
	re    *regexp.Regexp
	rep   string
	guard []string
}

// ruleStore 规则容器：持有当前快照，可选对规则文件做轮询热加载
type ruleStore struct {
	file   string
	every  time.Duration
	logger *slog.Logger
	cur    atomic.Pointer[compiledRules]

	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func newRuleStore(c *config) (*ruleStore, error) {
	s := &ruleStore{logger: c.logger}

	var (
		cr  *compiledRules
		err error
	)
	switch {
	case c.noRules:
		cr = compileRules(&RuleSet{}, c.logger)
	case c.ruleSet != nil:
		cr = compileRules(c.ruleSet, c.logger)
	case c.rulesFile != "":
		s.file, s.every = c.rulesFile, c.watch
		cr, err = loadRulesFile(c.rulesFile, c.logger)
		if err != nil {
			return nil, err
		}
	default:
		cr, err = parseRules(c.rulesYAML, c.logger)
		if err != nil {
			return nil, err
		}
	}

	s.cur.Store(cr)

	if s.file != "" && s.every > 0 {
		s.startWatch()
	}

	return s, nil
}

// Current 取当前生效的规则快照
func (s *ruleStore) Current() *compiledRules {
	return s.cur.Load()
}

// Close 停止热加载协程
func (s *ruleStore) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.stop == nil {
			return
		}
		close(s.stop)
		<-s.done
	})
}

func (s *ruleStore) startWatch() {
	s.stop = make(chan struct{})
	s.done = make(chan struct{})

	var last time.Time
	if fi, err := os.Stat(s.file); err == nil {
		last = fi.ModTime()
	}

	go func() {
		defer close(s.done)

		t := time.NewTicker(s.every)
		defer t.Stop()

		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				fi, err := os.Stat(s.file)
				if err != nil || !fi.ModTime().After(last) {
					continue
				}
				last = fi.ModTime()

				cr, err := loadRulesFile(s.file, s.logger)
				if err != nil {
					s.logError("wetext: reload rules fail, keep old", err)
					continue
				}
				s.cur.Store(cr)
				s.logInfo("wetext: rules reloaded", "version", cr.version)
			}
		}
	}()
}

func (s *ruleStore) logInfo(msg string, args ...any) {
	if s.logger != nil {
		s.logger.Info(msg, args...)
	}
}

func (s *ruleStore) logError(msg string, err error) {
	if s.logger != nil {
		s.logger.Error(msg, "err", err)
	}
}

func loadRulesFile(file string, logger *slog.Logger) (*compiledRules, error) {
	bs, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("wetext: read rules: %w", err)
	}

	return parseRules(bs, logger)
}

func parseRules(data []byte, logger *slog.Logger) (*compiledRules, error) {
	var rs RuleSet
	if err := yaml.Unmarshal(data, &rs); err != nil {
		return nil, fmt.Errorf("wetext: parse rules: %w", err)
	}

	return compileRules(&rs, logger), nil
}

func compileRules(rs *RuleSet, logger *slog.Logger) *compiledRules {
	c := &compiledRules{
		version:  rs.Version,
		mapping:  compileReplaceSet(rs.Mapping, false),
		unit:     compileReplaceSet(rs.Unit, true),
		currency: compileCurrency(rs.Currency),
		phone:    compilePhone(rs.Phone, logger),
	}

	for _, p := range rs.Post {
		if p.Pattern == "" {
			continue
		}
		re, err := regexp.Compile(p.Pattern)
		if err != nil {
			if logger != nil {
				logger.Error("wetext: bad post rule, skipped", "name", p.Name, "err", err)
			}
			continue
		}
		c.post = append(c.post, postEntry{
			name:  p.Name,
			lang:  strings.ToLower(strings.TrimSpace(p.Lang)),
			re:    re,
			rep:   p.Replace,
			guard: p.Guard,
		})
	}

	return c
}

// compileReplaceSet 长 key 优先，避免 kV 抢在 kVA 前面命中。
// 同一份词表里数字 key 与字母 key 的边界规则不同，因此拆成两条正则，
// 各扫一遍（比每条 key 各扫一遍少一个数量级）。
func compileReplaceSet(m map[string]string, isUnit bool) *replaceSet {
	if len(m) == 0 {
		return nil
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})

	set := &replaceSet{unit: isUnit, vals: make(map[string]string, len(keys))}
	var digitAlts, letterAlts []string

	for _, k := range keys {
		switch {
		case isUnit, isDigits(k):
			// 单位：必须紧跟数字才生效，避免误转独立缩写；
			// 纯数字 key（110/120/985…）要求前后不是数字，
			// 否则 84354120 的尾数 120 也会被当成急救电话读掉
			digitAlts = append(digitAlts, regexp.QuoteMeta(k))
			set.vals[k] = m[k]
		case isASCIILetters(k):
			// 用捕获组替代前后向断言，Go regexp 不支持 (?<![A-Za-z])
			letterAlts = append(letterAlts, regexp.QuoteMeta(k))
			set.vals[strings.ToLower(k)] = m[k]
		default:
			set.plain = append(set.plain, mapEntry{key: k, val: m[k]})
		}
	}

	if len(digitAlts) > 0 {
		if isUnit {
			set.res = append(set.res, regexp.MustCompile(`(\d)\s*(`+strings.Join(digitAlts, "|")+`)([^A-Za-z]|$)`))
		} else {
			set.res = append(set.res, regexp.MustCompile(`(^|[^0-9])(`+strings.Join(digitAlts, "|")+`)([^0-9]|$)`))
		}
	}
	if len(letterAlts) > 0 {
		set.res = append(set.res, regexp.MustCompile(`(?i)(^|[^A-Za-z])(`+strings.Join(letterAlts, "|")+`)([^A-Za-z]|$)`))
	}

	return set
}

// apply 执行词表替换。前后缀捕获组原样输出，避免吃掉相邻字符。
func (s *replaceSet) apply(text string) string {
	if s == nil {
		return text
	}

	for _, re := range s.res {
		text = re.ReplaceAllStringFunc(text, func(m string) string {
			sub := re.FindStringSubmatch(m)
			if len(sub) != 4 {
				return m
			}
			val, ok := s.vals[sub[2]]
			if !ok {
				val, ok = s.vals[strings.ToLower(sub[2])]
				if !ok {
					return m
				}
			}
			if s.unit {
				return sub[1] + "[" + val + "]" + sub[3]
			}
			return sub[1] + val + sub[3]
		})
	}

	for _, e := range s.plain {
		if strings.Contains(text, e.key) {
			text = strings.ReplaceAll(text, e.key, e.val)
		}
	}

	return text
}

func isASCIILetters(s string) bool {
	for _, r := range s {
		if r > 127 || (r|0x20) < 'a' || (r|0x20) > 'z' {
			return false
		}
	}
	return s != ""
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
