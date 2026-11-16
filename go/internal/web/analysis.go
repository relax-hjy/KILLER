package web

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"killer/internal/analysis"
	"killer/internal/source"
	"killer/internal/trading"
)

// 分析结果缓存 TTL：日K + 实时行情合成成本高，短时间内复用。
const analysisTTL = 90 * time.Second

// analysisCache 分析结论内存缓存（按 secid）。
type analysisCache struct {
	mu sync.Mutex
	m  map[string]analysisCell
}

type analysisCell struct {
	a  *analysis.Assessment
	at time.Time
}

func newAnalysisCache() *analysisCache {
	return &analysisCache{m: map[string]analysisCell{}}
}

func (c *analysisCache) get(secid string) (*analysis.Assessment, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cell, ok := c.m[secid]
	if !ok || time.Since(cell.at) > analysisTTL {
		return nil, false
	}
	return cell.a, true
}

func (c *analysisCache) set(secid string, a *analysis.Assessment) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[secid] = analysisCell{a: a, at: time.Now()}
}

// assess 拉取日K与实时行情并做综合分析（缓存 TTL 内直接返回）。
func (s *Server) assess(ctx context.Context, secid, name string) (*analysis.Assessment, error) {
	if a, ok := s.acache.get(secid); ok {
		return a, nil
	}
	klines, err := trading.LoadKlines(ctx, secid, 120)
	if err != nil {
		return nil, err
	}
	// 实时价尽力而为：拿不到就用最新收盘（Assess 内部回退）
	var price, pct float64
	if quotes, qErr := source.FetchUSQuotes(ctx, []string{secid}); qErr == nil && len(quotes) > 0 {
		price, pct = quotes[0].Price, quotes[0].Pct
	}
	a, err := analysis.Assess(secid, name, klines, price, pct)
	if err != nil {
		return nil, err
	}
	s.acache.set(secid, a)
	return a, nil
}

// handleAnalysisWatchlist GET /api/analysis/watchlist — 配置观察列表全量分析（并发拉取）。
// 返回：{items: [...完整结论], failed: [{secid,name,error}], generated_at}
func (s *Server) handleAnalysisWatchlist(w http.ResponseWriter, r *http.Request) {
	list := s.cfg.Analysis.Watchlist
	if len(list) == 0 {
		writeJSON(w, map[string]any{
			"items":        []any{},
			"failed":       []any{},
			"generated_at": time.Now(),
		})
		return
	}
	type result struct {
		idx int
		a   *analysis.Assessment
		err error
	}
	ch := make(chan result, len(list))
	var wg sync.WaitGroup
	for i, item := range list {
		wg.Add(1)
		go func(i int, secid, name string) {
			defer wg.Done()
			a, err := s.assess(r.Context(), secid, name)
			ch <- result{idx: i, a: a, err: err}
		}(i, item.SecID, item.Name)
	}
	wg.Wait()
	close(ch)

	items := make([]*analysis.Assessment, len(list))
	failed := make([]map[string]string, 0)
	for res := range ch {
		if res.err != nil {
			failed = append(failed, map[string]string{
				"secid": list[res.idx].SecID,
				"name":  list[res.idx].Name,
				"error": res.err.Error(),
			})
			continue
		}
		items[res.idx] = res.a
	}
	// 压实失败位（保持 config 顺序）
	out := make([]*analysis.Assessment, 0, len(list))
	for _, a := range items {
		if a != nil {
			out = append(out, a)
		}
	}
	writeJSON(w, map[string]any{
		"items":        out,
		"failed":       failed,
		"generated_at": time.Now(),
	})
}

// handleAnalysis GET /api/analysis?secid=1.688981&name=中芯国际 — 单标的完整分析。
func (s *Server) handleAnalysis(w http.ResponseWriter, r *http.Request) {
	secid := r.URL.Query().Get("secid")
	if secid == "" {
		http.Error(w, "missing secid", http.StatusBadRequest)
		return
	}
	a, err := s.assess(r.Context(), secid, r.URL.Query().Get("name"))
	if err != nil {
		log.Printf("[Analysis] %s 分析失败: %v", secid, err)
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, a)
}
