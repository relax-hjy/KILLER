package web

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"killer/internal/analysis"
	"killer/internal/config"
	"killer/internal/model"
	"killer/internal/monitor"
	"killer/internal/source"
	"killer/internal/trading"
)

// analysisTTL 分析结果内存缓存 TTL：日K + 实时行情合成成本高，短时间内复用。
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

// assess 拉取日K与实时行情并做综合分析。
// 三级：内存缓存（90 秒）→ 盘后直接读当日存档日报（免请求数据源，重启后秒开）→ 现算并存档。
func (s *Server) assess(ctx context.Context, secid, name string) (*analysis.Assessment, error) {
	if a, ok := s.acache.get(secid); ok {
		return a, nil
	}
	if !monitor.MarketOpen(time.Now()) {
		if a, ok := s.storedReport(secid, monitor.LastSessionDate(time.Now())); ok {
			if a.Name == "" && name != "" {
				// 存档可能由不带 name 的单次分析写入，用调用方（配置）名称补齐并回存
				a.Name = name
				s.saveReport(a)
			}
			s.acache.set(secid, a)
			return a, nil
		}
	}
	klines, err := trading.LoadKlines(ctx, secid, 120)
	if err != nil {
		return nil, err
	}
	// 实时价尽力而为：拿不到就用最新收盘（Assess 内部回退）
	var price, pct float64
	if quotes, qErr := source.FetchUSQuotes(ctx, []string{secid}); qErr == nil && len(quotes) > 0 {
		price, pct = quotes[0].Price, quotes[0].Pct
		if name == "" {
			name = quotes[0].Name // 调用方没传名称时用行情名称，日报里不至于空着
		}
	}
	a, err := analysis.Assess(secid, name, klines, price, pct)
	if err != nil {
		return nil, err
	}
	s.acache.set(secid, a)
	s.saveReport(a)
	return a, nil
}

// saveReport 分析结论落库为当日日报（每家标的每交易日一份，同日覆盖）。
// 存档失败只记日志，不影响分析响应。
func (s *Server) saveReport(a *analysis.Assessment) {
	if a.KlineDate == "" {
		return
	}
	b, err := json.Marshal(a)
	if err != nil {
		log.Printf("[Analysis] %s 日报序列化失败: %v", a.SecID, err)
		return
	}
	err = s.st.SaveAnalysisReport(model.AnalysisReport{
		SecID:   a.SecID,
		Date:    a.KlineDate,
		Name:    a.Name,
		Score:   a.Score,
		Advice:  a.Advice,
		Payload: string(b),
	})
	if err != nil {
		log.Printf("[Analysis] %s 日报存档失败: %v", a.SecID, err)
	}
}

// storedReport 读取某标的某交易日存档日报（解析失败按未命中处理，交给现算）。
func (s *Server) storedReport(secid, date string) (*analysis.Assessment, bool) {
	r, err := s.st.GetAnalysisReport(secid, date)
	if err != nil {
		log.Printf("[Analysis] %s %s 日报读取失败: %v", secid, date, err)
		return nil, false
	}
	if r == nil {
		return nil, false
	}
	var a analysis.Assessment
	if err := json.Unmarshal([]byte(r.Payload), &a); err != nil {
		log.Printf("[Analysis] %s %s 日报反序列化失败: %v", secid, date, err)
		return nil, false
	}
	return &a, true
}

// handleAnalysisWatchlist GET /api/analysis/watchlist?group=存储 — 分组观察列表全量分析（并发拉取）。
// group 缺省取第一个分组。返回：{group, groups, items: [...完整结论], failed: [...], generated_at}
func (s *Server) handleAnalysisWatchlist(w http.ResponseWriter, r *http.Request) {
	group, list := s.groupItems(r.URL.Query().Get("group"))
	items, failed := s.assessWatchlist(r.Context(), list)
	writeJSON(w, map[string]any{
		"group":        group,
		"groups":       s.groupNames(),
		"items":        items,
		"failed":       failed,
		"generated_at": time.Now(),
	})
}

// groupItems 按名称取分组标的（from 引用的分组在 GroupMembers 里展开）；
// 未知或缺省分组回退第一个（同时回传实际生效的分组名）。
func (s *Server) groupItems(name string) (string, []config.Index) {
	for _, g := range s.cfg.Analysis.Groups {
		if g.Name == name {
			return g.Name, s.cfg.GroupMembers(g)
		}
	}
	if len(s.cfg.Analysis.Groups) == 0 {
		return "", nil
	}
	g := s.cfg.Analysis.Groups[0]
	return g.Name, s.cfg.GroupMembers(g)
}

// groupNames 全部分组名（前端 tab 顺序与配置一致）。
func (s *Server) groupNames() []string {
	names := make([]string, 0, len(s.cfg.Analysis.Groups))
	for _, g := range s.cfg.Analysis.Groups {
		names = append(names, g.Name)
	}
	return names
}

// allWatchlist 全部分组的标的汇总（收盘存档用，与分组无关）。
func (s *Server) allWatchlist() []config.Index {
	var out []config.Index
	for _, g := range s.cfg.Analysis.Groups {
		out = append(out, s.cfg.GroupMembers(g)...)
	}
	return out
}

// assessWatchlist 并发分析给定标的（结果保持传入顺序），返回成功结论与失败明细。
func (s *Server) assessWatchlist(ctx context.Context, list []config.Index) ([]*analysis.Assessment, []map[string]string) {
	out := make([]*analysis.Assessment, 0, len(list))
	failed := make([]map[string]string, 0)
	if len(list) == 0 {
		return out, failed
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
			a, err := s.assess(ctx, secid, name)
			ch <- result{idx: i, a: a, err: err}
		}(i, item.SecID, item.Name)
	}
	wg.Wait()
	close(ch)

	items := make([]*analysis.Assessment, len(list))
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
	// 压实失败位（保持传入顺序）
	for _, a := range items {
		if a != nil {
			out = append(out, a)
		}
	}
	return out, failed
}

// archiveInterval 收盘补档轮询间隔（盘后每 30 分钟看一次当日日报是否已入库）。
const archiveInterval = 30 * time.Minute

// startArchive 观察列表日报补档：无人打开分析面板时，也保证每个交易日每家的日报都存进库。
// 当日存档齐全时只查库即返回，不请求数据源。
func (s *Server) startArchive() {
	go func() {
		s.archiveOnce()
		t := time.NewTicker(archiveInterval)
		defer t.Stop()
		for range t.C {
			s.archiveOnce()
		}
	}()
}

// archiveOnce 补齐当日会话的日报（全部分组的标的）：缺档、或只有盘中快照的（面板在收盘前被打过），
// 都在收盘后重算一次。
func (s *Server) archiveOnce() {
	list := s.allWatchlist()
	if len(list) == 0 || monitor.MarketOpen(time.Now()) {
		return
	}
	date := monitor.LastSessionDate(time.Now())
	pending := make([]config.Index, 0, len(list))
	for _, it := range list {
		r, err := s.st.GetAnalysisReport(it.SecID, date)
		if err != nil {
			continue
		}
		if r == nil || intradaySnapshot(r, date) {
			pending = append(pending, it)
		}
	}
	if len(pending) == 0 {
		return
	}
	// 分批下发超时预算：全部分组累计已上百只，一整批只给 1 分钟会把后半批静默截断
	const batch = 12
	done := 0
	for i := 0; i < len(pending); i += batch {
		end := min(i+batch, len(pending))
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		items, _ := s.assessWatchlist(ctx, pending[i:end])
		cancel()
		done += len(items)
	}
	log.Printf("[Analysis] 收盘存档 %s：%d/%d 家日报已入库", date, done, len(pending))
}

// intradaySnapshot 存档是否为当日盘中快照（生成时刻早于收盘，需收盘后重算成终版日报）。
func intradaySnapshot(r *model.AnalysisReport, sessionDate string) bool {
	created := monitor.Beijing(r.CreatedAt)
	if created.Format("2006-01-02") != sessionDate {
		return false // 非当日生成（例如次日凌晨补算），已是收盘后结论
	}
	return created.Hour()*60+created.Minute() < 15*60+5
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

// handleAnalysisReports GET /api/analysis/reports?secid=1.688981&n=60 —
// 某标的历史日报索引（日期/评分/建议，不含研判详情）。
func (s *Server) handleAnalysisReports(w http.ResponseWriter, r *http.Request) {
	secid := r.URL.Query().Get("secid")
	if secid == "" {
		http.Error(w, "missing secid", http.StatusBadRequest)
		return
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	reports, err := s.st.ListAnalysisReports(secid, n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if reports == nil {
		reports = []model.AnalysisReport{}
	}
	writeJSON(w, reports)
}

// handleAnalysisReport GET /api/analysis/report?secid=1.688981&date=2026-11-14 —
// 某标的某交易日的完整存档日报（无存档 404）。
func (s *Server) handleAnalysisReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	secid, date := q.Get("secid"), q.Get("date")
	if secid == "" || date == "" {
		http.Error(w, "missing secid or date", http.StatusBadRequest)
		return
	}
	rpt, err := s.st.GetAnalysisReport(secid, date)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rpt == nil {
		http.Error(w, "no report for "+secid+" on "+date, http.StatusNotFound)
		return
	}
	var a analysis.Assessment
	if err := json.Unmarshal([]byte(rpt.Payload), &a); err != nil {
		log.Printf("[Analysis] %s %s 日报反序列化失败: %v", secid, date, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, a)
}

// handleAnalysisBacktest GET /api/analysis/backtest?group=&bars=300 — 评分有效性回测。
// 组内全部标的逐日滚动重算评分（评估日只用截至当日的K线），按评分分层统计未来 5/10/20 根收益。
// 结果缓存 30 分钟：滚动重算要逐只拉日K，不值得每次点击都现算。
func (s *Server) handleAnalysisBacktest(w http.ResponseWriter, r *http.Request) {
	group, list := s.groupItems(r.URL.Query().Get("group"))
	bars, _ := strconv.Atoi(r.URL.Query().Get("bars"))
	if bars <= 0 || bars > 500 {
		bars = 300
	}
	key := "abtest:" + group + ":" + strconv.Itoa(bars)
	v, err := marketCache.GetOrSet(key, 30*time.Minute, func() (any, error) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		return s.rollBacktest(ctx, group, list, bars)
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, v)
}

// rollBacktest 并发拉K线并滚动重算，返回分层统计。
func (s *Server) rollBacktest(ctx context.Context, group string, list []config.Index, bars int) (map[string]any, error) {
	type res struct{ evals []analysis.RollEval }
	ch := make(chan res, len(list))
	var wg sync.WaitGroup
	for _, it := range list {
		wg.Add(1)
		go func(secid string) {
			defer wg.Done()
			klines, err := trading.LoadKlines(ctx, secid, bars)
			if err != nil {
				log.Printf("[Analysis] 回测取K线失败 %s: %v", secid, err)
				return
			}
			ch <- res{analysis.RollScore(klines, analysis.RollWarmup)}
		}(it.SecID)
	}
	wg.Wait()
	close(ch)
	var evals []analysis.RollEval
	for r := range ch {
		evals = append(evals, r.evals...)
	}
	return map[string]any{
		"group":       group,
		"bars":        bars,
		"securities":  len(list),
		"evaluations": len(evals),
		"horizons":    analysis.RollHorizons,
		"buckets":     analysis.Aggregate(evals),
		"note":        "毛收益（未计手续费）· 信号收盘后生成，入场=次根收盘，出场=入场后第 5/10/20 根收盘 · 样本=组内全部标的滚动重算",
	}, nil
}
