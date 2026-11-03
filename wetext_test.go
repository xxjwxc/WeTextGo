package wetext

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testNormalizer(t *testing.T) *Normalizer {
	t.Helper()

	n, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { n.Close() })

	return n
}

func TestNormalizeZh(t *testing.T) {
	n := testNormalizer(t)

	cases := []struct {
		in   string
		want string
	}{
		// 数字/小数/百分比/分数/时间/日期全部由 WeTextProcessing 负责
		{"3.14", "三点一四"},
		{"30%", "百分之三十"},
		{"1/5", "五分之一"},
		{"12:35:36", "十二点三十五分三十六秒"},
		{"12:00", "十二点"},
		{"2023年5月11日", "二零二三年五月十一日"},
		{"2024", "二零二四"},
		{"13800138000", "幺三八零零幺三八零零零"},
		{"温度-5度", "温度负五度"},
		{"2020-2024年", "二零二零到二零二四年"},
		{"第1章", "第一章"},
		// 领域规则：yaml 词表/单位映射在引擎之前执行
		{"985高校", "九八五高校"},
		{"500kV变压器", "五百千伏变压器"},
		{"1.5MW", "一点五兆瓦"},
		// 后置正则修正 WTP 的中文读法
		{"50-100", "五十至一百"},
		{"3-2", "三至二"},
		{"RT-LAB", "R T杠LAB"},
		// 大写缩写逐字母拆分
		{"IGBT", "I G B T"},
		// 号码按位读：tagger 会把区号拆成 math token，用 phone 规则在引擎前先按位读
		{"好的，请记录，（0527）84354120", "好的，请记录，（零五二七）八四三五四幺二零"},
	}

	for _, c := range cases {
		if got := n.NormalizeLang(c.in, "zh"); got != c.want {
			t.Errorf("NormalizeLang(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	n := testNormalizer(t)

	cases := []struct {
		in   string
		want string
	}{
		// 手机号：无分隔 / 分组横线 / 分组空格 / 国际前缀 / 分机
		{"13800138000", "幺三八零零幺三八零零零"},
		{"15912345678", "幺五九幺二三四五六七八"},
		{"+8613800138000", "八六幺三八零零幺三八零零零"},
		{"+86 138 0013 8000", "八六幺三八零零幺三八零零零"},
		{"0086-138-0013-8000", "零零八六幺三八零零幺三八零零零"},
		{"138-0013-8000", "幺三八零零幺三八零零零"},
		{"138 0013 8000", "幺三八零零幺三八零零零"},
		{"13800138000转8001", "幺三八零零幺三八零零零转八零零幺"},
		// 固话：括号区号 / 横线 / 空格 / 分机 / 单独区号
		{"(0527)84354120", "(零五二七)八四三五四幺二零"},
		{"（0527）84354120", "（零五二七）八四三五四幺二零"},
		{"0527-84354120", "零五二七八四三五四幺二零"},
		{"0527 84354120", "零五二七八四三五四幺二零"},
		{"(010)12345678转123", "(零幺零)幺二三四五六七八转幺二三"},
		{"0527", "零五二七"},
		// 热线与国际号码
		{"400-123-4567", "四零零幺二三四五六七"},
		{"4001234567", "四零零幺二三四五六七"},
		{"+1-202-555-0123", "幺二零二五五五零幺二三"},
		// 区号 + 5~6 位短号码（政务服务热线等），不能按数量读成「一万二千三百四十五」
		{"021-12345", "零二幺幺二三四五"},
		{"010-95588", "零幺零九五五八八"},
		{"0755-123456", "零七五五幺二三四五六"},
		{"(021)12345", "(零二幺)幺二三四五"},
		{"你可以拨打上海政务服务热线021-12345咨询", "你可以拨打上海政务服务热线零二幺幺二三四五咨询"},
		// 上下文短号：拨打/致电/热线/电话 后的 5~8 位裸号码逐位读
		{"请拨打 12345 咨询", "请拨打 幺二三四五 咨询"},
		{"热线：95588", "热线：九五五八八"},
		{"致电 123456", "致电 幺二三四五六"},
		{"拨打 1234567", "拨打 幺二三四五六七"},
		{"客服电话 12345", "客服电话 幺二三四五"},
		// 尾随上下文短号：号码后面紧跟「…热线/…电话」，上下文词可在号码之前或之后
		{"通过12351工会服务热线咨询", "通过幺二三五幺工会服务热线咨询"},
		{"12351工会服务热线", "幺二三五幺工会服务热线"},
		{"12345政务服务便民热线", "幺二三四五政务服务便民热线"},
		{"12351电话", "幺二三五幺电话"},
		{"上海市总工会官网(https://www.shzgh.org/)或通过12351工会服务热线咨询",
			"上海市总工会官网(https://www.shzgh.org/)或通过幺二三五幺工会服务热线咨询"},
	}

	for _, c := range cases {
		if got := n.NormalizeLang(c.in, "zh"); got != c.want {
			t.Errorf("NormalizeLang(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 号码规则不能误伤普通数字
func TestNormalizePhoneNoSideEffect(t *testing.T) {
	n := testNormalizer(t)

	cases := []struct {
		in   string
		want string
	}{
		{"84354120", "八千四百三十五万四千一百二十"},
		{"300120元", "三十万零一百二十元"},
		{"0.0527%", "百分之零点零五二七"},
		{"电话 13800138000 请回拨", "电话 幺三八零零幺三八零零零 请回拨"},
		// 无上下文词的数量仍按数量读，上下文词后接量词不经号码规则
		{"共 12345 件", "共 一万二千三百四十五件"},
		{"咨询 12345 人次", "咨询 一万二千三百四十五人次"},
		{"电话费 12345 元", "电话费 一万二千三百四十五元"},
		// 号码后紧接量词再跟「热线」时，仍按数量/序号读
		{"共 12345 条热线", "共 一万二千三百四十五条热线"},
		{"第12345号热线稿件", "第一万二千三百四十五号热线稿件"},
	}

	for _, c := range cases {
		if got := n.NormalizeLang(c.in, "zh"); got != c.want {
			t.Errorf("NormalizeLang(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeEn(t *testing.T) {
	n := testNormalizer(t)

	cases := []struct {
		in   string
		want string
	}{
		{"3.14", "three point one four"},
		{"30%", "thirty percent"},
		{"2.5 sq mm wire", "two point five sq mm wire"},
	}

	for _, c := range cases {
		if got := n.NormalizeLang(c.in, "en"); got != c.want {
			t.Errorf("NormalizeLang(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeHotRegex(t *testing.T) {
	n := testNormalizer(t)

	got := n.NormalizeWith("今天 500KV", CallOptions{
		Lang: "zh",
		Hot:  []HotRule{{Pattern: `(?i)kv`, Replace: "千伏"}, {Pattern: "今天", Replace: "今日"}},
	})
	want := "今日 五百千伏"

	if got != want {
		t.Errorf("hot regex got %q, want %q", got, want)
	}
}

func TestDetectLang(t *testing.T) {
	cases := []struct {
		lang string
		text string
		want string
	}{
		{"", "你好", "zh"},
		{"", "hello 123", "en"},
		{"", "لدي 300 دولار", "ar"},
		{"", "हिन्दी 100", "hi"},
		{"zh-CN", "hello", "zh"},
		{"cn", "", "zh"},
		{"en-US", "", "en"},
	}

	for _, c := range cases {
		if got := detectLang(c.lang, c.text); got != c.want {
			t.Errorf("detectLang(%q, %q) = %q, want %q", c.lang, c.text, got, c.want)
		}
	}
}

func TestAutoLang(t *testing.T) {
	n := testNormalizer(t)

	if got, want := n.Normalize("3.14平方"), "三点一四平方"; got != want {
		t.Errorf("Normalize(zh) = %q, want %q", got, want)
	}
	if got, want := n.Normalize("3.14 dollars"), "three point one four dollars"; got != want {
		t.Errorf("Normalize(en) = %q, want %q", got, want)
	}
}

func TestBatch(t *testing.T) {
	n := testNormalizer(t)

	texts := []string{"3.14", "30%", "2024", "1/5", "hello 42", "500kV变压器"}
	res := n.Batch(texts, CallOptions{Lang: "zh"})

	if len(res) != len(texts) {
		t.Fatalf("len(Batch) = %d, want %d", len(res), len(texts))
	}
	for i, r := range res {
		if r.Orig != texts[i] {
			t.Errorf("res[%d].Orig = %q, want %q", i, r.Orig, texts[i])
		}
		if r.Text == "" {
			t.Errorf("res[%d].Text is empty", i)
		}
	}
	if got, want := res[0].Text, "三点一四"; got != want {
		t.Errorf("res[0].Text = %q, want %q", got, want)
	}
}

func TestWithRuleSet(t *testing.T) {
	n, err := New(WithRuleSet(RuleSet{
		Mapping: map[string]string{"985": "九八五"},
		Unit:    map[string]string{"kV": "千伏"},
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer n.Close()

	if got, want := n.NormalizeLang("985高校", "zh"), "九八五高校"; got != want {
		t.Errorf("mapping got %q, want %q", got, want)
	}
	if got, want := n.NormalizeLang("500kV", "zh"), "五百千伏"; got != want {
		t.Errorf("unit got %q, want %q", got, want)
	}
	// 未配置的默认规则不再生效
	if got, want := n.NormalizeLang("211高校", "zh"), "两百一十一高校"; got != want {
		t.Errorf("custom rules should replace default, got %q, want %q", got, want)
	}
}

func TestWithRulesYAML(t *testing.T) {
	n, err := New(WithRulesYAML([]byte(`
version: 2
mapping:
  "211": "二一一"
post:
  - name: 减转至
    lang: zh
    pattern: '([0-9])减([0-9])'
    replace: '${1}至${2}'
`)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer n.Close()

	if got, want := n.NormalizeLang("211高校", "zh"), "二一一高校"; got != want {
		t.Errorf("yaml mapping got %q, want %q", got, want)
	}
}

func TestWithoutRules(t *testing.T) {
	n, err := New(WithoutRules())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer n.Close()

	// 只用 FST：985 按数量读，kV 不会被换成中文
	if got, want := n.NormalizeLang("985", "zh"), "九百八十五"; got != want {
		t.Errorf("WithoutRules got %q, want %q", got, want)
	}
}

func TestRulesFileHotReload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "rules.yaml")

	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
			t.Fatalf("write rules: %v", err)
		}
	}

	write("version: 1\nmapping:\n  \"985\": \"九八五\"\n")

	n, err := New(WithRulesFile(file), WithWatchInterval(20*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer n.Close()

	if got, want := n.NormalizeLang("985", "zh"), "九八五"; got != want {
		t.Fatalf("before reload got %q, want %q", got, want)
	}

	// 改文件后应在几百毫秒内生效
	write("version: 2\nmapping:\n  \"985\": \"九八五高校\"\n")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if n.NormalizeLang("985", "zh") == "九八五高校" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("rules not reloaded, got %q", n.NormalizeLang("985", "zh"))
}

func TestWithModelDir(t *testing.T) {
	n, err := New(WithModelDir("assets"))
	if err != nil {
		t.Fatalf("New(WithModelDir): %v", err)
	}
	defer n.Close()

	if got, want := n.NormalizeLang("3.14", "zh"), "三点一四"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	langs := n.Langs()
	if len(langs) != 8 {
		t.Errorf("Langs() = %v, want 8 languages", langs)
	}
}

func TestUnknownLangKeepsText(t *testing.T) {
	n := testNormalizer(t)

	if got, want := n.NormalizeLang("3.14", "xx"), "3.14"; got != want {
		t.Errorf("unknown lang got %q, want %q", got, want)
	}
}

func TestCloseTwice(t *testing.T) {
	n, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	n.Close()
	n.Close()
}

func BenchmarkNormalizeZh(b *testing.B) {
	n, err := New()
	if err != nil {
		b.Fatal(err)
	}
	defer n.Close()

	text := "2024年5月11日，500kV变压器运行正常，负载率达到87.5%，共465篇报告约315万字，温度-5度。"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n.NormalizeLang(text, "zh")
	}
}

func BenchmarkBatchParallel(b *testing.B) {
	n, err := New()
	if err != nil {
		b.Fatal(err)
	}
	defer n.Close()

	texts := make([]string, 64)
	for i := range texts {
		texts[i] = "2024年5月11日，负载率87.5%，温度-5度。"
	}
	o := CallOptions{Lang: "zh"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n.Batch(texts, o)
	}
}
