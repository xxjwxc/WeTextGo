package fst

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type goldenCase struct {
	Lang    string `json:"lang"`
	Input   string `json:"input"`
	Tagged  string `json:"tagged"`
	Ordered string `json:"ordered"`
	Output  string `json:"output"`
	Error   string `json:"error"`
}

func TestGolden(t *testing.T) {
	bs, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Skipf("no golden corpus: %v", err)
	}

	var cases []goldenCase
	if err := json.Unmarshal(bs, &cases); err != nil {
		t.Fatalf("parse golden: %v", err)
	}

	reg, err := Open("../../assets")
	if err != nil {
		t.Skipf("no models: %v", err)
	}

	for _, c := range cases {
		if c.Error != "" {
			continue
		}

		eng, ok := reg.Engine(c.Lang)
		if !ok {
			t.Errorf("%s: engine not available", c.Lang)
			continue
		}

		tagged, ok := eng.tagger.Transduce([]byte(c.Input))
		if !ok {
			t.Errorf("%s %q: tagger found no path", c.Lang, c.Input)
			continue
		}
		if got := strings.TrimSpace(string(tagged)); got != c.Tagged {
			t.Errorf("%s %q tagger:\n got %q\nwant %q", c.Lang, c.Input, got, c.Tagged)
			continue
		}

		if got := reorderTokens(c.Tagged, eng.orders); got != c.Ordered {
			t.Errorf("%s %q reorder:\n got %q\nwant %q", c.Lang, c.Input, got, c.Ordered)
			continue
		}

		got, ok := eng.Normalize(c.Input)
		if !ok {
			got = c.Input
		}
		if got != c.Output {
			t.Errorf("%s %q normalize:\n got %q\nwant %q", c.Lang, c.Input, got, c.Output)
		}
	}
}

func TestLangs(t *testing.T) {
	reg, err := Open("../../assets")
	if err != nil {
		t.Skipf("no models: %v", err)
	}

	langs := reg.Langs()
	want := map[string]bool{"zh": true, "en": true, "de": true, "es": true, "fr": true, "it": true, "hi": true, "ar": true}
	if len(langs) != len(want) {
		t.Fatalf("Langs() = %v, want %d languages", langs, len(want))
	}
	for _, l := range langs {
		if !want[l] {
			t.Errorf("unexpected lang %q", l)
		}
	}
}

func BenchmarkNormalizeZh(b *testing.B) {
	reg, err := Open("../../assets")
	if err != nil {
		b.Skip(err)
	}
	eng, ok := reg.Engine("zh")
	if !ok {
		b.Skip("no zh model")
	}

	text := "2024年5月11日，500kV变压器运行正常，负载率达到87.5%，共465篇报告约315万字，温度-5度。"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng.Normalize(text)
	}
}
