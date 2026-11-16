package web

// ETF 持仓明细 API：
//   GET /api/etf-holdings?code=510300 — ETF 最新披露期前十大重仓（成分股）

import (
	"net/http"
	"regexp"
	"strings"
	"time"

	"killer/internal/source"
)

var etfCodeRe = regexp.MustCompile(`^\d{6}$`)

// handleETFHoldings GET /api/etf-holdings?code=510300 — ETF 前十大重仓股。
// code 亦接受 secid 形态（1.510300 / 0.159915），自动截取 6 位基金代码。
// 持仓为季报低频数据，缓存 6 小时节流（防东财限流）。
func (s *Server) handleETFHoldings(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if i := strings.Index(code, "."); i >= 0 {
		code = code[i+1:] // secid（1.510300 / 0.159915）→ 尾段基金代码
	}
	if !etfCodeRe.MatchString(code) {
		http.Error(w, "invalid code, expect 6-digit fund code", http.StatusBadRequest)
		return
	}
	v, err := marketCache.GetOrSet("etfhold:"+code, 6*time.Hour, func() (any, error) {
		return source.FetchETFHoldings(r.Context(), code)
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, v)
}
