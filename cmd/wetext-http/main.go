// Command wetext-http 用标准库把 wetext 包暴露成 HTTP JSON 接口，
// 方便非 Go 服务调用。只依赖标准库，不引入 gin/gRPC。
//
//	go run ./cmd/wetext-http -addr :8080
//
//	curl -s localhost:8080/normalize -d '{"text":["500kV变压器","3.14"],"lang":"zh"}'
//	# {"results":[{"orig":"500kV变压器","text":"五百千伏变压器"},{"orig":"3.14","text":"三点一四"}]}
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	wetext "github.com/xxjwxc/WeTextGo"
)

// request 请求体。hotRegex 沿用 [["pattern","replace"]] 的写法。
type request struct {
	Text     []string   `json:"text"`
	Lang     string     `json:"lang"`
	HotRegex [][]string `json:"hotRegex"`
}

type result struct {
	Orig string `json:"orig"`
	Text string `json:"text"`
}

type response struct {
	Results []result `json:"results"`
	Error   string   `json:"error,omitempty"`
}

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	modelDir := flag.String("model", "", "模型目录，默认用包内嵌模型")
	rulesFile := flag.String("rules", "", "规则 YAML 文件，默认用包内嵌规则")
	flag.Parse()

	opts := []wetext.Option{wetext.WithLogger(slog.Default())}
	if *modelDir != "" {
		opts = append(opts, wetext.WithModelDir(*modelDir))
	}
	if *rulesFile != "" {
		opts = append(opts, wetext.WithRulesFile(*rulesFile))
	}

	n, err := wetext.New(opts...)
	if err != nil {
		log.Fatalf("init wetext: %v", err)
	}
	defer n.Close()
	log.Printf("wetext ready, langs=%v", n.Langs())

	mux := http.NewServeMux()
	mux.HandleFunc("/normalize", handleNormalize(n))
	mux.HandleFunc("/langs", handleLangs(n))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	log.Printf("listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
}

func handleNormalize(n *wetext.Normalizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, response{Error: "bad json: " + err.Error()})
			return
		}

		hot := make([]wetext.HotRule, 0, len(req.HotRegex))
		for _, g := range req.HotRegex {
			if len(g) >= 2 {
				hot = append(hot, wetext.HotRule{Pattern: g[0], Replace: g[1]})
			}
		}

		res := n.Batch(req.Text, wetext.CallOptions{Lang: req.Lang, Hot: hot})
		out := response{Results: make([]result, 0, len(res))}
		for _, item := range res {
			out.Results = append(out.Results, result{Orig: item.Orig, Text: item.Text})
		}

		writeJSON(w, http.StatusOK, out)
	}
}

func handleLangs(n *wetext.Normalizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string][]string{"langs": n.Langs()})
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
