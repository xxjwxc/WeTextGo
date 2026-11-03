package fst

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
)

// Engine 一个语种的归一化流水线：tag -> reorder -> verbalize。
type Engine struct {
	lang       string
	tagger     *Model
	verbalizer *Model
	orders     map[string][]string
	needDigit  bool // zh/ja 的 TN 只在含数字时才有意义
	preserveWS bool
}

// Lang 语种码
func (e *Engine) Lang() string { return e.lang }

// Normalize 归一化单条文本。第二个返回值表示流水线是否走通，
// 走不通（无路径/模型缺失/解析失败）时调用方应保留原文。
func (e *Engine) Normalize(text string) (string, bool) {
	if text == "" {
		return text, false
	}
	if e.needDigit && !HasDigit(text) {
		return text, false
	}

	tagged, ok := e.tagger.Transduce([]byte(text))
	if !ok {
		return text, false
	}

	ordered := reorderTokens(strings.TrimSpace(string(tagged)), e.orders)

	out, ok := e.verbalizer.Transduce([]byte(ordered))
	if !ok {
		return text, false
	}

	if e.preserveWS {
		return string(out), true
	}

	return strings.TrimSpace(string(out)), true
}

// HasDigit 文本里是否含 ASCII 数字
func HasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			return true
		}
	}
	return false
}

// Registry 按语种懒加载引擎。模型可以来自磁盘，也可以来自 embed.FS。
type Registry struct {
	fsys   fs.FS
	dir    string
	orders map[string]map[string][]string

	mu    sync.Mutex
	cache sync.Map // lang -> *Engine，nil 表示该语种不可用
}

// Open 打开磁盘上的模型目录
func Open(dir string) (*Registry, error) {
	return OpenFS(os.DirFS(dir), ".")
}

// OpenFS 从任意 fs.FS 打开模型目录，dir 为空表示根目录。
// orders.json 缺失时仍可用，只是不做字段重排。
func OpenFS(fsys fs.FS, dir string) (*Registry, error) {
	dir = path.Clean(strings.TrimSpace(dir))
	if dir == "" || dir == "/" {
		dir = "."
	}

	fi, err := fs.Stat(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("wetext: model dir %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("wetext: %s is not a directory", dir)
	}

	r := &Registry{fsys: fsys, dir: dir}

	if bs, err := fs.ReadFile(fsys, path.Join(dir, "orders.json")); err == nil {
		if err := json.Unmarshal(bs, &r.orders); err != nil {
			return nil, fmt.Errorf("wetext: orders.json: %w", err)
		}
	}

	return r, nil
}

// Engine 取得语种引擎；模型不存在时返回 false。
// 加载后走无锁读，避免每次调用都抢同一把互斥锁。
func (r *Registry) Engine(lang string) (*Engine, bool) {
	lang = NormalizeLang(lang)
	if lang == "" {
		return nil, false
	}

	if v, ok := r.cache.Load(lang); ok {
		e, _ := v.(*Engine)
		return e, e != nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if v, ok := r.cache.Load(lang); ok {
		e, _ := v.(*Engine)
		return e, e != nil
	}

	tagger, err := r.loadModel(lang + "_tagger_tn.wtfst")
	if err != nil {
		r.cache.Store(lang, (*Engine)(nil))
		return nil, false
	}
	verbalizer, err := r.loadModel(lang + "_verbalizer_tn.wtfst")
	if err != nil {
		r.cache.Store(lang, (*Engine)(nil))
		return nil, false
	}

	e := &Engine{
		lang:       lang,
		tagger:     tagger,
		verbalizer: verbalizer,
		orders:     r.orders[lang],
		needDigit:  lang == "zh" || lang == "ja",
		preserveWS: lang == "ko",
	}
	r.cache.Store(lang, e)

	return e, true
}

func (r *Registry) loadModel(name string) (*Model, error) {
	bs, err := fs.ReadFile(r.fsys, path.Join(r.dir, name))
	if err != nil {
		return nil, err
	}
	m, err := ParseModel(bs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}

	return m, nil
}

// Langs 返回当前目录里可用的语种
func (r *Registry) Langs() []string {
	matches, _ := fs.Glob(r.fsys, path.Join(r.dir, "*_tagger_tn.wtfst"))

	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, strings.TrimSuffix(path.Base(m), "_tagger_tn.wtfst"))
	}

	return out
}

// NormalizeLang 归一化语种标识：小写、去掉区域后缀、cn -> zh
func NormalizeLang(lang string) string {
	code := strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexAny(code, "-_"); i > 0 {
		code = code[:i]
	}
	switch code {
	case "cn", "chinese":
		return "zh"
	}

	return code
}
