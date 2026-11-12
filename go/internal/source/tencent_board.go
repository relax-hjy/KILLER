package source

// 腾讯财经板块数据源（2026-11-13 实测，用于彻底替代东财板块能力）：
//   - 板块列表: proxy.finance.qq.com/cgi/cgi-bin/rank/pt/getRank?board_type=hy|gn
//      返回 code/name/zxj(点位)/zd(涨跌额)/zdf(涨跌幅)/zgb("涨/跌"家数)/lzg(领涨股)/zsz(总市值)
//   - 板块行情: qt.gtimg.cn/q=pt0xxx（~ 分隔，复用 parseTencentStock 的通用字段）
//   - 板块成分股: proxy.finance.qq.com/cgi/cgi-bin/rank/hs/getBoardRankList?board_code=pt0xxx
//      返回 code/name/zxj(现价)/zdf(涨跌幅)/zd(涨跌额)/hsl(换手)/ltsz(流通市值)/zsz(总市值)/pe_ttm
//   - 板块日K: web.ifzq.gtimg.cn/appstock/app/fqkline/get?param=pt0xxx,day,,,n,qfq
//   - 板块分时: web.ifzq.gtimg.cn/appstock/app/minute/query?code=pt0xxx
//
// 板块代码命名规律：行业板块 pt0180xxxx（stock_type= BK-HY-*），
// 概念板块 pt02GNxxxx / pt0210xxxx（stock_type= BK-GN-*）。
// 统一用 "pt" 前缀识别板块 secid（与东财 "90.BK" 前缀解耦）。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"killer/internal/model"
)

const (
	tencentBoardListURL = "https://proxy.finance.qq.com/cgi/cgi-bin/rank/pt/getRank?board_type=%s&sort_type=price&direct=down&offset=0&count=100"
	tencentBoardMemURL  = "https://proxy.finance.qq.com/cgi/cgi-bin/rank/hs/getBoardRankList?board_code=%s&sort_type=price&direct=down&offset=0&count=100"
	tencentReferer      = "https://gu.qq.com/"
)

// IsBoardSecid 判断是否板块 secid（腾讯 pt 前缀）。
func IsBoardSecid(secid string) bool {
	return strings.HasPrefix(secid, "pt")
}

// tencentGetJSON GET 腾讯 JSON 接口（带 Referer）。
func tencentGetJSON(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", tencentUA)
	req.Header.Set("Referer", tencentReferer)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tencent 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tencent HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("tencent 读取响应失败: %w", err)
	}
	return body, nil
}

// tencentBoardRankItem 板块列表单条（rank/pt/getRank 响应）。
type tencentBoardRankItem struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Zxj       string `json:"zxj"` // 板块指数点位
	Zd        string `json:"zd"`  // 涨跌额
	Zdf       string `json:"zdf"` // 涨跌幅(%)
	Zgb       string `json:"zgb"` // 涨跌家数 "涨/跌"
	Lzg       *struct {
		Code string `json:"code"`
		Name string `json:"name"`
		Zd   string `json:"zd"`
		Zdf  string `json:"zdf"`
		Zxj  string `json:"zxj"`
	} `json:"lzg"`
	StockType string `json:"stock_type"`
}

// tencentBoardRankRoot 板块列表响应。
type tencentBoardRankRoot struct {
	Code int `json:"code"`
	Data struct {
		RankList []tencentBoardRankItem `json:"rank_list"`
	} `json:"data"`
}

// tencentBoards 腾讯行业/概念板块列表。kind: industry|concept。
// 腾讯按 sort_type=price+direct=down 返回，前端自行按涨跌幅排序，这里取原序。
func tencentBoards(ctx context.Context, kind string) ([]model.Board, error) {
	bt := "hy"
	if kind == "concept" {
		bt = "gn"
	}
	body, err := tencentGetJSON(ctx, fmt.Sprintf(tencentBoardListURL, bt))
	if err != nil {
		return nil, err
	}
	var root tencentBoardRankRoot
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("tencent 板块列表解析失败: %w", err)
	}
	if root.Code != 0 {
		return nil, fmt.Errorf("tencent 板块列表返回错误 code=%d", root.Code)
	}
	out := make([]model.Board, 0, len(root.Data.RankList))
	for _, it := range root.Data.RankList {
		b := model.Board{
			SecID:  it.Code,
			Name:   it.Name,
			Price:  f(it.Zxj),
			Change: f(it.Zd),
			Pct:    f(it.Zdf),
		}
		b.UpCount, b.DownCount = parseBoardZgb(it.Zgb)
		out = append(out, b)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tencent 板块列表返回空")
	}
	return out, nil
}

// parseBoardZgb 解析 "49/122" → (上涨49, 下跌122)。
func parseBoardZgb(s string) (int, int) {
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 {
		return -1, -1
	}
	up, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	dn, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return -1, -1
	}
	return up, dn
}

// ---- 板块成分股 ----

// tencentBoardMemItem 成分股单条。
type tencentBoardMemItem struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Zxj    string `json:"zxj"`  // 现价
	Zdf    string `json:"zdf"`  // 涨跌幅(%)
	Zd     string `json:"zd"`   // 涨跌额
	Hsl    string `json:"hsl"`  // 换手率(%)
	Ltsz   string `json:"ltsz"` // 流通市值(亿)
	Zsz    string `json:"zsz"`  // 总市值(亿)
	PeTTM  string `json:"pe_ttm"`
	Volume string `json:"volume"`
	Turnover string `json:"turnover"`
}

// tencentBoardMemRoot 成分股响应。
type tencentBoardMemRoot struct {
	Code int `json:"code"`
	Data struct {
		RankList []tencentBoardMemItem `json:"rank_list"`
	} `json:"data"`
}

// tencentBoardMembers 板块成分股（腾讯）。返回按现价降序（腾讯默认排序）。
// 流通市值(ltsz)/总市值(zsz)单位亿元 → 元，供前端计算权重占比。
func tencentBoardMembers(ctx context.Context, boardCode string) ([]model.Stock, error) {
	body, err := tencentGetJSON(ctx, fmt.Sprintf(tencentBoardMemURL, boardCode))
	if err != nil {
		return nil, err
	}
	var root tencentBoardMemRoot
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("tencent 成分股解析失败: %w", err)
	}
	if root.Code != 0 {
		return nil, fmt.Errorf("tencent 成分股返回错误 code=%d", root.Code)
	}
	now := time.Now()
	out := make([]model.Stock, 0, len(root.Data.RankList))
	for _, it := range root.Data.RankList {
		secid := tencentStockToSecid(it.Code)
		code := strings.TrimPrefix(strings.TrimPrefix(it.Code, "sh"), "sz")
		out = append(out, model.Stock{
			SecID:      secid,
			Code:       code,
			Name:       it.Name,
			Price:      f(it.Zxj),
			Pct:        f(it.Zdf),
			Change:     f(it.Zd),
			Turnover:   f(it.Hsl),
			MarketCap:  f(it.Zsz) * 1e8,  // 亿 → 元
			FloatCap:   f(it.Ltsz) * 1e8, // 亿 → 元
			PE:         f(it.PeTTM),
			Amount:     f(it.Turnover),
			UpdateTime: now,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tencent 成分股返回空")
	}
	return out, nil
}

// tencentStockToSecid 腾讯成分股 code → secid。sz002371→0.002371 / sh603536→1.603536。
func tencentStockToSecid(code string) string {
	if strings.HasPrefix(code, "sh") {
		return "1." + code[2:]
	}
	return "0." + code[2:]
}

// FetchBoardQuotes 批量获取板块行情（腾讯 pt 代码），返回 []model.Quote 供看门狗使用。
// 板块行情字段与个股一致（~ 分隔），复用 tencentBatchQuotes 的解析，再转 Quote。
func FetchBoardQuotes(ctx context.Context, secids []string) ([]model.Quote, error) {
	// 仅保留板块 secid
	var boards []string
	for _, id := range secids {
		if IsBoardSecid(id) {
			boards = append(boards, id)
		}
	}
	if len(boards) == 0 {
		return nil, fmt.Errorf("无板块 secid")
	}
	stocks, err := tencentBatchQuotes(ctx, boards)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]model.Quote, 0, len(stocks))
	for _, st := range stocks {
		q := model.Quote{
			SecID:      st.SecID,
			Code:       st.Code,
			Name:       st.Name,
			Price:      st.Price,
			Change:     st.Change,
			Pct:        st.Pct,
			Open:       st.Open,
			High:       st.High,
			Low:        st.Low,
			PrevClose:  st.PrevClose,
			Volume:     st.Amount,
			UpdateTime: now,
		}
		if q.PrevClose > 0 {
			q.Amplitude = (q.High - q.Low) / q.PrevClose * 100
		}
		out = append(out, q)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("板块行情返回空")
	}
	return out, nil
}
