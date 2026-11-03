package wetext

import (
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"time"
)

// Result 单条归一化结果
type Result struct {
	Orig string // 原始文本
	Text string // 归一化后文本
}

// HotRule 调用方热正则：先于所有内置规则执行，Pattern 使用 Go RE2 语法。
type HotRule struct {
	Pattern string
	Replace string
}

// CallOptions 单次调用选项
type CallOptions struct {
	// Lang 语种，如 zh/en/de/es/fr/it/hi/ar，兼容 zh-CN、cn 这类写法。
	// 为空时按字符集自动判定：汉字 -> zh，阿拉伯文 -> ar，天城文 -> hi，其余 -> en。
	Lang string
	// Hot 调用方热正则，按顺序执行。
	Hot []HotRule
}

// PostRule 一条后置正则规则，Pattern 使用 Go RE2 语法。
// 不支持 (?<=) (?=) 这类前后向断言，需要上下文时请改用捕获组，
// 例如 (?<!\d)5 改写为 (^|[^\d])5。
type PostRule struct {
	Name    string   `yaml:"name" json:"name"`
	Lang    string   `yaml:"lang" json:"lang"` // 只对指定语种生效，为空则所有语种生效
	Pattern string   `yaml:"pattern" json:"pattern"`
	Replace string   `yaml:"replace" json:"replace"`
	Guard   []string `yaml:"guard" json:"guard"` // 句中包含其中任一词才生效，为空则总是生效
}

// RuleSet 补充规则集，结构与 assets/norm_rules.yaml 一致。
//
// 它只用于补齐 WeTextProcessing 没覆盖的场景（领域词、电力单位、号码按位读、
// 货币符号、修正读法的后置正则），数字/日期/时间/分数等仍由 FST 负责。
type RuleSet struct {
	Version  int                          `yaml:"version" json:"version"`
	Mapping  map[string]string            `yaml:"mapping" json:"mapping"`   // 词/特号 -> 读法，仅中文
	Unit     map[string]string            `yaml:"unit" json:"unit"`         // 单位：仅当前面是数字时生效，仅中文
	Currency map[string]map[string]string `yaml:"currency" json:"currency"` // 货币符号读法：语种 -> 符号 -> 读法
	Phone    []string                     `yaml:"phone" json:"phone"`       // 号码正则：命中整段数字逐位读，仅中文
	Post     []PostRule                   `yaml:"post" json:"post"`         // 后置正则：按顺序执行
}

// config 归一化器配置
type config struct {
	fsys        fs.FS         // 模型文件系统
	dir         string        // 模型目录
	rulesYAML   []byte        // 规则 YAML 内容
	rulesFile   string        // 规则文件（给了文件就以文件为准，并支持热加载）
	ruleSet     *RuleSet      // 代码内直接指定规则
	noRules     bool          // 关闭补充规则，只用 WeTextProcessing 引擎
	watch       time.Duration // 规则文件轮询间隔，0 表示不热加载
	defaultLang string        // 调用方未指定语种时使用的语种
	logger      *slog.Logger  // 可选日志
}

func defaultConfig() config {
	bs, _ := assets.ReadFile(defaultRulesTxt)

	return config{
		fsys:      assets,
		dir:       assetsDir,
		rulesYAML: bs,
		watch:     time.Second,
	}
}

// Option 构造选项
type Option func(*config) error

// WithModelDir 从磁盘目录加载模型，目录内需包含 <lang>_tagger_tn.wtfst 等文件。
// 默认使用包内嵌的模型，通常不需要这个选项。
func WithModelDir(dir string) Option {
	return func(c *config) error {
		if dir == "" {
			return fmt.Errorf("wetext: empty model dir")
		}
		fi, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("wetext: model dir: %w", err)
		}
		if !fi.IsDir() {
			return fmt.Errorf("wetext: %s is not a directory", dir)
		}
		c.fsys = os.DirFS(dir)
		c.dir = "."
		return nil
	}
}

// WithModelFS 从任意 fs.FS（如 embed.FS、zip 包）加载模型，dir 为空表示根目录。
func WithModelFS(fsys fs.FS, dir string) Option {
	return func(c *config) error {
		if fsys == nil {
			return fmt.Errorf("wetext: nil model fs")
		}
		c.fsys = fsys
		c.dir = dir
		return nil
	}
}

// WithRulesFile 从 YAML 文件加载补充规则，并默认每 1 秒检查一次文件变更热加载。
// 传了本选项后，包内嵌的默认规则不再生效。
func WithRulesFile(file string) Option {
	return func(c *config) error {
		if file == "" {
			return fmt.Errorf("wetext: empty rules file")
		}
		c.rulesFile = file
		return nil
	}
}

// WithRulesYAML 直接指定 YAML 规则内容，不做热加载。
func WithRulesYAML(data []byte) Option {
	return func(c *config) error {
		c.rulesYAML = data
		return nil
	}
}

// WithRuleSet 直接指定规则对象，适合代码里动态拼装规则。
func WithRuleSet(rs RuleSet) Option {
	return func(c *config) error {
		c.ruleSet = &rs
		return nil
	}
}

// WithoutRules 关闭补充规则，只保留 WeTextProcessing 的 FST 规则。
func WithoutRules() Option {
	return func(c *config) error {
		c.noRules = true
		return nil
	}
}

// WithWatchInterval 设置规则文件轮询间隔，0 表示关闭热加载。
func WithWatchInterval(d time.Duration) Option {
	return func(c *config) error {
		c.watch = d
		return nil
	}
}

// WithDefaultLang 设置默认语种：调用方未传 Lang 时不再自动判定。
func WithDefaultLang(lang string) Option {
	return func(c *config) error {
		c.defaultLang = lang
		return nil
	}
}

// WithLogger 设置日志输出，用于规则加载/热加载失败的提示。默认不输出任何日志。
func WithLogger(l *slog.Logger) Option {
	return func(c *config) error {
		c.logger = l
		return nil
	}
}
