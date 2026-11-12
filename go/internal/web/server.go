// Package web 提供 JSON API 与单页监控面板。
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"killer/internal/config"
	"killer/internal/model"
	"killer/internal/monitor"
	"killer/internal/source"
	"killer/internal/store"
	"killer/internal/trading"
)

//go:embed static
var staticFS embed.FS

// Server HTTP 服务。
type Server struct {
	cfg           *config.Config
	wd            *monitor.Watchdog
	st            *store.Store
	trade         *trading.Engine
	klineEM       source.Source // 东财 K 线（也是分时唯一源）
	klineSina     source.Source // 新浪日 K（仅 A 股 sh/sz）
	klineTencent  source.Source // 腾讯 K 线/分时（板块 pt 代码、美股指数）
	mux           *http.ServeMux
}

// New 构建路由。
func New(cfg *config.Config, wd *monitor.Watchdog, st *store.Store) (*Server, error) {
	kl, err := source.New("eastmoney")
	if err != nil {
		return nil, err
	}
	klSina, err := source.New("sina")
	if err != nil {
		return nil, err
	}
	klTencent, err := source.New("tencent")
	if err != nil {
		return nil, err
	}

	// 初始化交易引擎（数据持久化到独立目录）
	var tradeEng *trading.Engine
	if cfg.Trading.Enabled {
		tradeEng, err = initTrading(cfg)
		if err != nil {
			return nil, err
		}
	}

	s := &Server{cfg: cfg, wd: wd, st: st, trade: tradeEng, klineEM: kl, klineSina: klSina, klineTencent: klTencent, mux: http.NewServeMux()}

	sub, _ := fs.Sub(staticFS, "static")
	// 禁缓存：静态页内嵌于二进制，改版后必须让浏览器拿到最新文件
	// （此前无缓存控制，浏览器曾缓存旧 JS 导致按钮行为与代码不一致）
	s.mux.Handle("GET /", noStore(http.FileServerFS(sub)))

	s.mux.HandleFunc("GET /api/quotes", s.handleQuotes)
	s.mux.HandleFunc("GET /api/board-quotes", s.handleBoardQuotes)
	s.mux.HandleFunc("GET /api/board-members", s.handleBoardMembers)
	s.mux.HandleFunc("GET /api/history", s.handleHistory)
	s.mux.HandleFunc("GET /api/intraday", s.handleIntraday)
	s.mux.HandleFunc("GET /api/kline", s.handleKline)
	s.mux.HandleFunc("GET /api/alerts", s.handleAlerts)
	s.mux.HandleFunc("GET /api/status", s.handleStatus)

	// 市场扩展
	s.mux.HandleFunc("GET /api/boards", s.handleBoards)
	s.mux.HandleFunc("GET /api/stocks", s.handleStocks)
	s.mux.HandleFunc("GET /api/etfs", s.handleETFs)
	s.mux.HandleFunc("GET /api/etf-quotes", s.handleETFQuotes)
	s.mux.HandleFunc("GET /api/etf-groups", s.handleETFGroups)
	s.mux.HandleFunc("GET /api/us/overview", s.handleUSOverview)
	s.mux.HandleFunc("GET /api/us/stocks", s.handleUSStocks)
	s.mux.HandleFunc("GET /api/ndx", s.handleNDX)
	s.mux.HandleFunc("GET /api/watch", s.handleWatch)
	s.mux.HandleFunc("POST /api/watch", s.handleWatch)
	s.mux.HandleFunc("DELETE /api/watch", s.handleWatch)

	// 数据源治理
	s.mux.HandleFunc("GET /api/datasources", s.handleDataSources)
	s.mux.HandleFunc("POST /api/datasources/test", s.handleDataSourceTest)
	s.mux.HandleFunc("GET /api/datasources/routing", s.handleRouting)
	s.mux.HandleFunc("POST /api/datasources/routing", s.handleRouting)

	// 交易系统
	if tradeEng != nil {
		s.mux.HandleFunc("GET /api/trade/account", s.handleTradeAccount)
		s.mux.HandleFunc("GET /api/trade/positions", s.handleTradePositions)
		s.mux.HandleFunc("GET /api/trade/orders", s.handleTradeOrders)
		s.mux.HandleFunc("POST /api/trade/orders", s.handleTradePlace)
		s.mux.HandleFunc("DELETE /api/trade/orders", s.handleTradeCancel)
		s.mux.HandleFunc("GET /api/trade/trades", s.handleTradeTrades)
		s.mux.HandleFunc("GET /api/trade/signal", s.handleTradeSignal)
		s.mux.HandleFunc("POST /api/trade/backtest", s.handleTradeBacktest)
		s.mux.HandleFunc("GET /api/trade/strategies", s.handleTradeStrategies)
	}
	return s, nil
}

// initTrading 初始化交易引擎：确保数据目录存在并打开交易数据库。
func initTrading(cfg *config.Config) (*trading.Engine, error) {
	dataDir := cfg.Trading.DataDir
	if dataDir == "" {
		dataDir = `D:\CloudDriver\EVO\killer`
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建交易数据目录失败: %w", err)
	}
	dbPath := filepath.Join(dataDir, "trading.db")
	ts, err := trading.Open(dbPath)
	if err != nil {
		return nil, err
	}
	log.Printf("[Trading] 交易数据目录: %s", dataDir)
	return trading.New(cfg, ts), nil
}

// Serve 启动 HTTP 服务（阻塞）。
func (s *Server) Serve() error {
	log.Printf("[Web] 面板已启动: http://localhost%s", s.cfg.Server.Addr)
	return http.ListenAndServe(s.cfg.Server.Addr, s.mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[Web] JSON 编码失败: %v", err)
	}
}

// noStore 给响应加 Cache-Control: no-store（内嵌静态资源改版即生效）。
func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// handleQuotes GET /api/quotes — 全部指数最新行情。
func (s *Server) handleQuotes(w http.ResponseWriter, r *http.Request) {
	quotes := s.wd.Latest()
	if quotes == nil {
		quotes = []model.Quote{}
	}
	writeJSON(w, quotes)
}

// handleBoardQuotes GET /api/board-quotes — 全部监控板块最新行情。
func (s *Server) handleBoardQuotes(w http.ResponseWriter, r *http.Request) {
	quotes := s.wd.LatestBoards()
	if quotes == nil {
		quotes = []model.Quote{}
	}
	writeJSON(w, quotes)
}

// handleHistory GET /api/history?secid=1.000001&hours=4 — 当日/近期快照走势。
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	secid := r.URL.Query().Get("secid")
	if secid == "" {
		http.Error(w, "missing secid", http.StatusBadRequest)
		return
	}
	hours, _ := strconv.Atoi(r.URL.Query().Get("hours"))
	if hours <= 0 || hours > 24*7 {
		hours = 4
	}
	since := time.Now().Add(-time.Duration(hours) * time.Hour)
	quotes, err := s.st.RecentQuotes(secid, since, 5000)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if quotes == nil {
		quotes = []model.Quote{}
	}
	writeJSON(w, quotes)
}

// handleIntraday GET /api/intraday?secid=1.000001 — 当日分时（数据源分钟 K，休市/重启均可看全天）。
// 板块 secid（pt 前缀）走腾讯；其余先按路由尝试东财（首选），东财失败时降级腾讯
// （腾讯 minute/query 对 A股/ETF/美股指数通吃，2026-11-13 实测）——
// 这层兜底使东财限流期个股/ETF 分时图不再 502。
func (s *Server) handleIntraday(w http.ResponseWriter, r *http.Request) {
	secid := r.URL.Query().Get("secid")
	if secid == "" {
		http.Error(w, "missing secid", http.StatusBadRequest)
		return
	}
	var klines []model.Kline
	var err error
	if source.IsBoardSecid(secid) {
		klines, err = s.klineTencent.FetchMinuteKline(r.Context(), secid)
	} else {
		// 东财首选；失败降级腾讯（A股/ETF 通吃）
		klines, err = s.klineEM.FetchMinuteKline(r.Context(), secid)
		if err != nil {
			log.Printf("[Web] 分时东财失败 %s，降级腾讯: %v", secid, err)
			klines, err = s.klineTencent.FetchMinuteKline(r.Context(), secid)
		}
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if klines == nil {
		klines = []model.Kline{}
	}
	writeJSON(w, klines)
}

// isUSSecid 判断是否美股 secid（新浪 K 线仅支持 sh/sz）。
func isUSSecid(secid string) bool {
	return strings.HasPrefix(secid, "105.") || strings.HasPrefix(secid, "106.") || strings.HasPrefix(secid, "107.")
}

// handleKline GET /api/kline?secid=1.000001&limit=120 — 日 K 走势。
// 板块 secid（pt 前缀）走腾讯；其余按路由顺序尝试（东财/新浪）。
// 美股 secid 固定东财（新浪 getKLineData 仅支持 sh/sz）。
func (s *Server) handleKline(w http.ResponseWriter, r *http.Request) {
	secid := r.URL.Query().Get("secid")
	if secid == "" {
		http.Error(w, "missing secid", http.StatusBadRequest)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 120
	}

	// 板块 secid → 腾讯
	if source.IsBoardSecid(secid) {
		klines, err := s.klineTencent.FetchKline(r.Context(), secid, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if klines == nil {
			klines = []model.Kline{}
		}
		writeJSON(w, klines)
		return
	}

	order := []source.Source{s.klineEM, s.klineSina}
	if source.GetRouting(source.FeatKline) == source.SrcSina && !isUSSecid(secid) {
		order = []source.Source{s.klineSina, s.klineEM}
	}
	var lastErr error
	for _, src := range order {
		if src == s.klineSina && isUSSecid(secid) {
			continue // 新浪不支持美股 K 线
		}
		klines, err := src.FetchKline(r.Context(), secid, limit)
		if err == nil {
			if klines == nil {
				klines = []model.Kline{}
			}
			writeJSON(w, klines)
			return
		}
		lastErr = err
		log.Printf("[Web] K线源 %s 失败(%s): %v", src.Name(), secid, err)
	}
	http.Error(w, lastErr.Error(), http.StatusBadGateway)
}

// handleAlerts GET /api/alerts?n=50 — 最近告警。
func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 200 {
		n = 50
	}
	alerts, err := s.st.RecentAlerts(n)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if alerts == nil {
		alerts = []model.Alert{}
	}
	writeJSON(w, alerts)
}

// handleStatus GET /api/status — 巡检状态。
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.wd.Status())
}
