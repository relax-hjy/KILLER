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

// handleBoardMembers GET /api/board-members?secid=90.BK1128 — 板块成分股（东财专属）。
// 返回按涨幅降序的成分股（含流通市值，供前端计算权重占比）。
func (s *Server) handleBoardMembers(w http.ResponseWriter, r *http.Request) {
	secid := r.URL.Query().Get("secid")
	if !strings.HasPrefix(secid, "90.BK") {
		http.Error(w, "invalid secid, expect 90.BKxxxx", http.StatusBadRequest)
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
