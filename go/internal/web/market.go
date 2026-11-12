package web

// 市场扩展 API：
//   GET    /api/boards?kind=&sort=        A股板块（行业/概念）
//   GET    /api/board-members?secid=      板块成分股（东财专属）
//   GET    /api/stocks?page=&sort=&asc=   A股个股分页
//   GET    /api/us/overview               美股三大指数 + SPDR 板块
//   GET    /api/us/stocks?page=&market=   美股个股分页
//   GET    /api/ndx                       纳指100成分+行情
//   GET    /watch                         自选个股 CRUD

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"

	"killer/internal/cache"
	"killer/internal/model"
	"killer/internal/source"
)

// marketCache 全局缓存（对上游限流保护）。
var marketCache = cache.New()

// handleBoards GET /api/boards?kind=industry|concept&sort=pct_desc|pct_asc
func (s *Server) handleBoards(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "concept" {
		kind = "industry"
	}
	sort := r.URL.Query().Get("sort")
	key := "boards:" + kind + ":" + sort
	v, err := marketCache.GetOrSet(key, source.TTLBoards, func() (any, error) {
		return source.FetchBoards(r.Context(), kind, sort)
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	boards, _ := v.([]model.Board)
	if boards == nil {
		boards = []model.Board{}
	}
	writeJSON(w, boards)
}

// handleBoardMembers GET /api/board-members?secid=pt02003891 — 板块成分股（腾讯首选，东财兜底）。
// 返回成分股（含流通市值，供前端计算权重占比）。
func (s *Server) handleBoardMembers(w http.ResponseWriter, r *http.Request) {
	secid := r.URL.Query().Get("secid")
	if !source.IsBoardSecid(secid) && !strings.HasPrefix(secid, "90.BK") {
		http.Error(w, "invalid secid, expect pt-prefixed or 90.BKxxxx", http.StatusBadRequest)
		return
	}
	v, err := marketCache.GetOrSet("bmembers:"+secid, source.TTLBoards, func() (any, error) {
		return source.FetchBoardMembers(r.Context(), secid)
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	members, _ := v.([]model.Stock)
	if members == nil {
		members = []model.Stock{}
	}
	writeJSON(w, members)
}

// handleStocks GET /api/stocks?page=1&sort=f3&asc=0
func (s *Server) handleStocks(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	sort := r.URL.Query().Get("sort")
	valid := map[string]bool{"f3": true, "f6": true, "f8": true, "f9": true, "f20": true, "f21": true, "f12": true}
	if !valid[sort] {
		sort = "f3"
	}
	asc := r.URL.Query().Get("asc") == "1"
	key := "stocks:" + strconv.Itoa(page) + ":" + sort + ":" + strconv.FormatBool(asc)
	v, err := marketCache.GetOrSet(key, source.TTLStocks, func() (any, error) {
		stocks, total, err := source.FetchStocks(r.Context(), page, 50, sort, asc)
		if err != nil {
			return nil, err
		}
		return []any{stocks, total}, nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	type resp struct {
		Items []model.Stock `json:"items"`
		Total int           `json:"total"`
		Page  int           `json:"page"`
	}
	r2 := v.([]any)
	stocks, _ := r2[0].([]model.Stock)
	total, _ := r2[1].(int)
	if stocks == nil {
		stocks = []model.Stock{}
	}
	writeJSON(w, resp{Items: stocks, Total: total, Page: page})
}

// handleETFs GET /api/etfs?page=1&sort=f3&asc=0 — 全市场 ETF 分页（东财 clist）。
// sort 同 stocks：f3涨幅 f6成交额 f8换手 f9市盈率 f20总市值 f21流通市值 f12代码。
// 东财限流期返回 502，前端展示提示（与 stocks 页同命运）。
func (s *Server) handleETFs(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	sort := r.URL.Query().Get("sort")
	valid := map[string]bool{"f3": true, "f6": true, "f8": true, "f9": true, "f20": true, "f21": true, "f12": true}
	if !valid[sort] {
		sort = "f6" // ETF 默认按成交额（活跃度），与 stocks 默认涨幅不同
	}
	asc := r.URL.Query().Get("asc") == "1"
	key := "etfs:" + strconv.Itoa(page) + ":" + sort + ":" + strconv.FormatBool(asc)
	v, err := marketCache.GetOrSet(key, source.TTLStocks, func() (any, error) {
		etfs, total, err := source.FetchETFs(r.Context(), page, 50, sort, asc)
		if err != nil {
			return nil, err
		}
		return []any{etfs, total}, nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	type resp struct {
		Items []model.Stock `json:"items"`
		Total int           `json:"total"`
		Page  int           `json:"page"`
	}
	r2 := v.([]any)
	etfs, _ := r2[0].([]model.Stock)
	total, _ := r2[1].(int)
	if etfs == nil {
		etfs = []model.Stock{}
	}
	writeJSON(w, resp{Items: etfs, Total: total, Page: page})
}

// handleETFQuotes GET /api/etf-quotes — 全部监控 ETF 最新行情（按 config 顺序）。
// 与 /api/quotes（指数）/api/board-quotes（板块）对称，供 ETF 看板卡片渲染。
func (s *Server) handleETFQuotes(w http.ResponseWriter, r *http.Request) {
	quotes := s.wd.LatestETFs()
	if quotes == nil {
		quotes = []model.Quote{}
	}
	writeJSON(w, quotes)
}

// handleETFGroups GET /api/etf-groups — 市场热门 ETF 组合（按 config.etf_groups 分组 + 实时行情）。
// 与 /api/etf-quotes（自监控固定 ETF）的区别：这里按类别分组展示各类代表 ETF，
// 纯看盘不告警。汇总所有组 secid 去重后一次腾讯批量拉行情，再按 config 顺序分组返回。
// 行情失败降级：返回分组结构（quotes 为空），前端展示组名+占位提示。
func (s *Server) handleETFGroups(w http.ResponseWriter, r *http.Request) {
	groups := s.cfg.ETFGroups
	if len(groups) == 0 {
		writeJSON(w, []any{})
		return
	}
	type groupResp struct {
		Name   string        `json:"name"`
		Desc   string        `json:"desc"`
		Quotes []model.Quote `json:"quotes"`
	}
	v, err := marketCache.GetOrSet("etf-groups", source.TTLQuotes, func() (any, error) {
		// 汇总所有组 secid 去重（同一 ETF 可能出现在多组，批量拉一次）
		seen := map[string]bool{}
		var all []string
		nameBySecid := map[string]string{}
		for _, g := range groups {
			for _, e := range g.ETFs {
				if e.SecID == "" || seen[e.SecID] {
					continue
				}
				seen[e.SecID] = true
				all = append(all, e.SecID)
				if e.Name != "" {
					nameBySecid[e.SecID] = e.Name
				}
			}
		}
		quotes, qerr := source.FetchETFQuotes(r.Context(), all)
		bySecid := map[string]model.Quote{}
		if qerr == nil {
			for _, q := range quotes {
				bySecid[q.SecID] = q
			}
		} else {
			log.Printf("[Web] ETF 组合行情失败，降级返回组结构: %v", qerr)
		}
		// 按 config 顺序分组，用 config 显示名覆盖（腾讯返回的中文名也准，但 config 更可控）
		out := make([]groupResp, 0, len(groups))
		for _, g := range groups {
			gr := groupResp{Name: g.Name, Desc: g.Desc, Quotes: []model.Quote{}}
			for _, e := range g.ETFs {
				if q, ok := bySecid[e.SecID]; ok {
					if e.Name != "" {
						q.Name = e.Name
					}
					gr.Quotes = append(gr.Quotes, q)
				}
			}
			out = append(out, gr)
		}
		// 行情失败也返回（降级），不报错
		return out, nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	grs, _ := v.([]groupResp)
	if grs == nil {
		grs = []groupResp{}
	}
	writeJSON(w, grs)
}

// handleUSOverview GET /api/us/overview — 三大指数 + 11 板块（一次批量）。
func (s *Server) handleUSOverview(w http.ResponseWriter, r *http.Request) {
	type overview struct {
		Indices []model.Stock `json:"indices"`
		Sectors []model.Stock `json:"sectors"`
	}
	v, err := marketCache.GetOrSet("us:overview", source.TTLQuotes, func() (any, error) {
		secids := source.USIndexSecids()
		for _, etf := range source.USSectorSPDR() {
			secids = append(secids, etf.SecID)
		}
		quotes, err := source.FetchUSQuotes(r.Context(), secids)
		if err != nil {
			return nil, err
		}
		// 按输入顺序补齐名称（东财返回的中文名更全，优先用返回值）
		bySecid := map[string]model.Stock{}
		for _, q := range quotes {
			bySecid[q.SecID] = q
		}
		var indices, sectors []model.Stock
		for _, id := range source.USIndexSecids() {
			if q, ok := bySecid[id]; ok {
				indices = append(indices, q)
			}
		}
		for _, etf := range source.USSectorSPDR() {
			if q, ok := bySecid[etf.SecID]; ok {
				q.Name = etf.Name // 用中文行业名覆盖
				sectors = append(sectors, q)
			}
		}
		return overview{Indices: indices, Sectors: sectors}, nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, v)
}

// handleUSStocks GET /api/us/stocks?page=1&sort=f6&market=105|106|107&asc=
func (s *Server) handleUSStocks(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page <= 0 {
		page = 1
	}
	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = "f6"
	}
	market := r.URL.Query().Get("market")
	if market != "105" && market != "106" && market != "107" {
		market = ""
	}
	asc := r.URL.Query().Get("asc") == "1"
	key := "usstocks:" + strconv.Itoa(page) + ":" + sort + ":" + market + ":" + strconv.FormatBool(asc)
	v, err := marketCache.GetOrSet(key, source.TTLStocks, func() (any, error) {
		stocks, total, err := source.FetchUSStocks(r.Context(), page, 50, sort, market, asc)
		if err != nil {
			return nil, err
		}
		return []any{stocks, total}, nil
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	type resp struct {
		Items []model.Stock `json:"items"`
		Total int           `json:"total"`
		Page  int           `json:"page"`
	}
	r2 := v.([]any)
	stocks, _ := r2[0].([]model.Stock)
	total, _ := r2[1].(int)
	if stocks == nil {
		stocks = []model.Stock{}
	}
	writeJSON(w, resp{Items: stocks, Total: total, Page: page})
}

// handleNDX GET /api/ndx — 成分表（缓存6h）+ 批量行情（缓存10s）。
func (s *Server) handleNDX(w http.ResponseWriter, r *http.Request) {
	v, err := marketCache.GetOrSet("ndx:members", source.TTLNDX, func() (any, error) {
		return source.FetchNDXMembers(r.Context())
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	members := v.([]model.Stock)
	if len(members) == 0 {
		writeJSON(w, []model.Stock{})
		return
	}

	secids := make([]string, 0, len(members))
	for _, m := range members {
		secids = append(secids, m.SecID)
	}
	qv, err := marketCache.GetOrSet("ndx:quotes", source.TTLQuotes, func() (any, error) {
		return source.FetchUSQuotes(r.Context(), secids)
	})
	if err != nil {
		// 行情失败降级：返回成分表（无价格），前端展示提示
		log.Printf("[Web] NDX 行情失败，降级返回成分表: %v", err)
		writeJSON(w, members)
		return
	}
	quotes := qv.([]model.Stock)
	bySecid := map[string]model.Stock{}
	for _, q := range quotes {
		bySecid[q.SecID] = q
	}
	out := make([]model.Stock, 0, len(members))
	for _, m := range members {
		if q, ok := bySecid[m.SecID]; ok {
			if m.Name != "" {
				q.Name = m.Name // 优先展示成分表英文名（东财中文名也保留字段以备）
			}
			out = append(out, q)
		} else {
			out = append(out, m) // 无行情的成员（停牌等）
		}
	}
	writeJSON(w, out)
}

// ---- 自选个股 ----

// handleWatch GET 列表 / POST 添加 / DELETE ?secid= 移除。
func (s *Server) handleWatch(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := s.st.ListWatchStocks()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []model.WatchStock{}
		}
		// 附带最新行情（批量）
		secids := make([]string, 0, len(list))
		for _, item := range list {
			secids = append(secids, item.SecID)
		}
		quotes := map[string]model.Stock{}
		if len(secids) > 0 {
			if qs, err := source.FetchUSQuotes(r.Context(), secids); err == nil {
				for _, q := range qs {
					quotes[q.SecID] = q
				}
			} else {
				log.Printf("[Web] 自选行情失败: %v", err)
			}
		}
		type row struct {
			model.WatchStock
			Quote *model.Stock `json:"quote,omitempty"`
		}
		rows := make([]row, 0, len(list))
		for _, item := range list {
			r3 := row{WatchStock: item}
			if q, ok := quotes[item.SecID]; ok {
				qq := q
				r3.Quote = &qq
			}
			rows = append(rows, r3)
		}
		writeJSON(w, rows)

	case http.MethodPost:
		var body struct {
			SecID     string  `json:"secid"`
			Code      string  `json:"code"`
			Name      string  `json:"name"`
			Threshold float64 `json:"threshold"`
			Notes     string  `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SecID == "" {
			http.Error(w, "invalid body: need secid", http.StatusBadRequest)
			return
		}
		if body.Code == "" {
			body.Code = body.SecID
		}
		if err := s.st.AddWatchStock(model.WatchStock{
			SecID: body.SecID, Code: body.Code, Name: body.Name,
			Threshold: body.Threshold, Notes: body.Notes,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})

	case http.MethodDelete:
		secid := r.URL.Query().Get("secid")
		if secid == "" {
			http.Error(w, "missing secid", http.StatusBadRequest)
			return
		}
		if err := s.st.RemoveWatchStock(secid); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
