package source

// 扩展数据源：A股板块/个股、美股、纳指100成分。
// 全部为东财 push2 clist/ulist 接口（2026-11-11 curl 实测通过）。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	sortpkg "sort"
	"strings"
	"time"

	"killer/internal/model"
)

const emListURL = "https://push2.eastmoney.com/api/qt/clist/get?pn=%d&pz=%d&po=%d&np=1&fltt=2&invt=2&fid=%s&fs=%s&fields=%s"

// listRoot 东财 clist 通用响应。diff 元素字段值可能是 "-"（如亏损股 PE），
// 统一用 json.RawMessage 延迟解析。
type listRoot struct {
	Data struct {
		Total int               `json:"total"`
		Diff  []json.RawMessage `json:"diff"`
	} `json:"data"`
}

func getJSONRaw(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Referer", "https://quote.eastmoney.com/")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// flt 把 "-" 等非数值解析为 0。
func flt(raw json.RawMessage) float64 {
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f
	}
	return 0
}

func str(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// fetchList 拉取 clist 分页列表，pageFn 处理单条 diff。
func fetchList(ctx context.Context, fs, fid string, po, page, pz int, fields string, pageFn func(raw json.RawMessage) error) (int, error) {
	url := fmt.Sprintf(emListURL, page, pz, po, fid, fs, fields)
	body, err := getJSONRaw(ctx, url)
	if err != nil {
		return 0, fmt.Errorf("clist 请求失败: %w", err)
	}
	var root listRoot
	if err := json.Unmarshal(body, &root); err != nil {
		return 0, fmt.Errorf("clist 解析失败: %w", err)
	}
	for _, raw := range root.Data.Diff {
		if err := pageFn(raw); err != nil {
			return 0, err
		}
	}
	return root.Data.Total, nil
}

// ---- A股板块 ----

const boardFields = "f2,f3,f4,f12,f14,f104,f105"

// FetchBoards 拉取板块列表。kind: "industry"(行业 t:2) | "concept"(概念 t:3)。
// sort: "pct_desc" | "pct_asc"（按涨跌幅）。
// 按路由顺序尝试（腾讯/新浪/东财），首个成功即返回。
func FetchBoards(ctx context.Context, kind, sort string) ([]model.Board, error) {
	var lastErr error
	for _, src := range pairFor(FeatBoards) {
		var boards []model.Board
		var err error
		switch src {
		case SrcTencent:
			boards, err = tencentBoards(ctx, kind)
		case SrcSina:
			boards, err = sinaBoards(ctx, kind, sort)
		default:
			boards, err = fetchBoardsEM(ctx, kind, sort)
		}
		if err == nil {
			return sortBoards(boards, sort), nil
		}
		lastErr = err
		log.Printf("[Source] 板块源 %s 失败: %v", src, err)
	}
	return nil, lastErr
}

// sortBoards 板块列表统一排序（腾讯返回原序，这里按涨跌幅重排）。
func sortBoards(boards []model.Board, sort string) []model.Board {
	sortpkg.Slice(boards, func(i, j int) bool {
		if sort == "pct_asc" {
			return boards[i].Pct < boards[j].Pct
		}
		return boards[i].Pct > boards[j].Pct
	})
	return boards
}

func fetchBoardsEM(ctx context.Context, kind, sort string) ([]model.Board, error) {
	t := "2"
	if kind == "concept" {
		t = "3"
	}
	fs := "m:90+t:" + t + "+f:!50"
	fid, po := "f3", 1 // 涨幅降序
	if sort == "pct_asc" {
		po = 0
	}
	var out []model.Board
	_, err := fetchList(ctx, fs, fid, po, 1, 100, boardFields, func(raw json.RawMessage) error {
		var d struct {
			F2   float64 `json:"f2"`
			F3   float64 `json:"f3"`
			F4   float64 `json:"f4"`
			F12  string  `json:"f12"`
			F14  string  `json:"f14"`
			F104 int     `json:"f104"`
			F105 int     `json:"f105"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		out = append(out, model.Board{
			SecID: d.F12, Name: d.F14, Price: d.F2, Pct: d.F3, Change: d.F4,
			UpCount: d.F104, DownCount: d.F105,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ---- 板块成分股 ----

// FetchBoardMembers 拉取板块成分股。首选腾讯（rank/hs/getBoardRankList），
// 东财兜底（b:BKxxxx）。secid 形如 pt02003891（腾讯）或 90.BK1128（东财）。
// 成分股的流通市值用于前端计算权重占比。
func FetchBoardMembers(ctx context.Context, secid string) ([]model.Stock, error) {
	// 腾讯板块代码（pt 前缀）直接走腾讯
	if IsBoardSecid(secid) {
		return tencentBoardMembers(ctx, secid)
	}
	// 东财板块代码（90.BK 前缀）走东财
	if strings.HasPrefix(secid, "90.BK") {
		return fetchBoardMembersEM(ctx, secid)
	}
	return nil, fmt.Errorf("非法板块 secid: %s（应为 pt 前缀或 90.BKxxxx）", secid)
}

func fetchBoardMembersEM(ctx context.Context, secid string) ([]model.Stock, error) {
	fs := "b:" + strings.TrimPrefix(secid, "90.") + "+f:!50"
	var out []model.Stock
	now := time.Now()
	for page := 1; page <= 5; page++ { // 单板块成分 ≤500，pz=100 最多 5 页
		total, err := fetchList(ctx, fs, "f3", 1, page, 100, stockFields, func(raw json.RawMessage) error {
			s, err := parseStock(raw, now)
			if err != nil {
				return err
			}
			out = append(out, s)
			return nil
		})
		if err != nil {
			return nil, err
		}
		if len(out) >= total {
			break
		}
	}
	return out, nil
}

// ---- A股个股 ----

const stockFields = "f2,f3,f4,f5,f6,f7,f8,f9,f12,f13,f14,f15,f16,f17,f18,f20,f21,f23"

// FetchStocks 拉取沪深 A 股分页列表。
// sortFid: f3涨幅 f6成交额 f8换手 f9市盈率 f20总市值 f21流通市值 f12代码。
// 返回 (stocks, total, err)。按路由顺序尝试（东财/新浪）。
func FetchStocks(ctx context.Context, page, pz int, sortFid string, asc bool) ([]model.Stock, int, error) {
	var lastErr error
	for _, src := range pairFor(FeatStocks) {
		var stocks []model.Stock
		var total int
		var err error
		if src == SrcSina {
			stocks, total, err = sinaStocks(ctx, page, pz, sortFid, asc)
		} else {
			stocks, total, err = fetchStocksEM(ctx, page, pz, sortFid, asc)
		}
		if err == nil {
			return stocks, total, nil
		}
		lastErr = err
		log.Printf("[Source] A股列表源 %s 失败: %v", src, err)
	}
	return nil, 0, lastErr
}

func fetchStocksEM(ctx context.Context, page, pz int, sortFid string, asc bool) ([]model.Stock, int, error) {
	fs := "m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23,m:0+t:81+s:2048" // 沪深A股+北交所
	po := 1
	if asc {
		po = 0
	}
	var out []model.Stock
	now := time.Now()
	total, err := fetchList(ctx, fs, sortFid, po, page, pz, stockFields, func(raw json.RawMessage) error {
		s, err := parseStock(raw, now)
		if err != nil {
			return err
		}
		out = append(out, s)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// ---- A股 ETF ----

// IsETFSecid 判断是否 A股 ETF secid（沪 1.5xxxxx / 深 0.159xxxx / 深 0.150xxxx 等）。
// 沪市 ETF：510/511/512/513/515/516/517/518/563/588（6 位代码前 3 位）；
//   510 宽基/512 行业/513 跨境/515 主题/516/517/518 商品/563 中证A50/588 科创
//   511 为国债/政金债/信用债 ETF（2026-11-13 补，原遗漏导致债券 ETF 不在全市场列表）
// 深市 ETF：159/150/152。前端/后端可据此过滤掉 fs 拉到的非 ETF 基金（LOF/开放基金等）。
func IsETFSecid(secid string) bool {
	code := ""
	switch {
	case strings.HasPrefix(secid, "1."):
		code = secid[2:]
	case strings.HasPrefix(secid, "0."):
		code = secid[2:]
	default:
		return false
	}
	if len(code) != 6 {
		return false
	}
	pre := code[:3]
	switch pre {
	case "510", "511", "512", "513", "515", "516", "517", "518", "563", "588", "159", "150", "152":
		return true
	}
	return false
}

// FetchETFs 拉取沪深 ETF 分页列表（东财 clist，fs=m:1+t:19,m:0+t:13）。
// 字段与个股共用 parseStock；sortFid 含义同 FetchStocks。
// 返回 (etfs, total, err)。东财限流期会失败，前端按 stocks 页同样降级提示。
// 注意 fs=m:0+t:13 包含深市全部基金（LOF/开放基金等），用 IsETFSecid 过滤后只留 ETF。
// 2026-11-13 实测：东财 push2 限流期返回空，恢复后可用；ETF 行情走腾讯批量通吃。
func FetchETFs(ctx context.Context, page, pz int, sortFid string, asc bool) ([]model.Stock, int, error) {
	var lastErr error
	for _, src := range pairFor(FeatETFs) {
		var etfs []model.Stock
		var total int
		var err error
		if src == SrcEastmoney {
			etfs, total, err = fetchETFsEM(ctx, page, pz, sortFid, asc)
		} else {
			lastErr = fmt.Errorf("ETF 列表仅东财支持（限流期暂不可用）")
			continue
		}
		if err == nil {
			return etfs, total, nil
		}
		lastErr = err
		log.Printf("[Source] ETF 列表源 %s 失败: %v", src, err)
	}
	return nil, 0, lastErr
}

// fetchETFsEM 东财 clist ETF 列表（沪 t:19 + 深 t:13，按 IsETFSecid 过滤）。
// 东财 clist total 反映过滤前的总数（含 LOF/开放基金），分页页码需 caller 按
// 返回的 items 长度估算；ETF 实际数量约 1000+，pz=50 单页足够。
func fetchETFsEM(ctx context.Context, page, pz int, sortFid string, asc bool) ([]model.Stock, int, error) {
	fs := "m:1+t:19,m:0+t:13" // 沪 ETF (t:19) + 深基金/ETF (t:13)
	po := 1
	if asc {
		po = 0
	}
	// 深市 t:13 含非 ETF 基金（LOF 等），多拉一些再用 IsETFSecid 过滤到 pz 条
	fetchPz := pz
	if fetchPz < 100 {
		fetchPz = pz * 2
	}
	var out []model.Stock
	now := time.Now()
	rawTotal, err := fetchList(ctx, fs, sortFid, po, page, fetchPz, stockFields, func(raw json.RawMessage) error {
		s, perr := parseStock(raw, now)
		if perr != nil {
			return perr
		}
		if !IsETFSecid(s.SecID) {
			return nil // 跳过 LOF/开放基金等
		}
		if len(out) < pz {
			out = append(out, s)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	// total 按 ETF 占比估算（约 60-70%），保守按 80% 折算
	estTotal := int(float64(rawTotal) * 0.7)
	if estTotal < len(out) {
		estTotal = len(out)
	}
	return out, estTotal, nil
}

// FetchETFQuotes 批量获取监控列表中固定几个 ETF 的实时行情（腾讯通吃）。
// 复用 FetchUSQuotes 路由（腾讯/新浪/东财），返回 Quote[] 供看门狗巡检。
func FetchETFQuotes(ctx context.Context, secids []string) ([]model.Quote, error) {
	stocks, err := FetchUSQuotes(ctx, secids)
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
		return nil, fmt.Errorf("ETF 行情返回空")
	}
	return out, nil
}

// parseStock 解析 clist 单条为 Stock（A股/美股字段一致）。
func parseStock(raw json.RawMessage, now time.Time) (model.Stock, error) {
	var d struct {
		F2  json.RawMessage `json:"f2"`  // 最新价
		F3  json.RawMessage `json:"f3"`  // 涨跌幅
		F4  json.RawMessage `json:"f4"`  // 涨跌额
		F6  json.RawMessage `json:"f6"`  // 成交额
		F8  json.RawMessage `json:"f8"`  // 换手率
		F9  json.RawMessage `json:"f9"`  // 市盈率
		F12 string          `json:"f12"` // 代码
		F13 int             `json:"f13"` // 市场
		F14 string          `json:"f14"` // 名称
		F15 json.RawMessage `json:"f15"` // 最高
		F16 json.RawMessage `json:"f16"` // 最低
		F17 json.RawMessage `json:"f17"` // 今开
		F18 json.RawMessage `json:"f18"` // 昨收
		F20 json.RawMessage `json:"f20"` // 总市值
		F21 json.RawMessage `json:"f21"` // 流通市值
		F23 json.RawMessage `json:"f23"` // 市净率
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return model.Stock{}, err
	}
	secid := d.F12
	if d.F13 == 1 {
		secid = "1." + d.F12
	} else if d.F13 == 0 {
		secid = "0." + d.F12
	} else {
		secid = fmt.Sprintf("%d.%s", d.F13, d.F12) // 美股 105/106/107
	}
	return model.Stock{
		SecID: secid, Code: d.F12, Name: d.F14,
		Price: flt(d.F2), Pct: flt(d.F3), Change: flt(d.F4),
		Open: flt(d.F17), High: flt(d.F15), Low: flt(d.F16), PrevClose: flt(d.F18),
		Amount: flt(d.F6), Turnover: flt(d.F8),
		PE: flt(d.F9), PB: flt(d.F23), MarketCap: flt(d.F20), FloatCap: flt(d.F21),
		UpdateTime: now,
	}, nil
}

// ---- 美股 ----

// usListFields 美股列表字段（与 A 股相同字段）。
const usListFields = stockFields

// FetchUSStocks 美股列表分页。market: "105"纳斯达克 "106"纽交所 "107"美交所，空=全部。
// sortFid 默认 f6 成交额（活跃度）。
func FetchUSStocks(ctx context.Context, page, pz int, sortFid, market string, asc bool) ([]model.Stock, int, error) {
	fs := "m:105+m:106+m:107+f:!50"
	if market != "" {
		fs = "m:" + market + "+f:!50"
	}
	po := 1
	if asc {
		po = 0
	}
	var out []model.Stock
	now := time.Now()
	total, err := fetchList(ctx, fs, sortFid, po, page, pz, usListFields, func(raw json.RawMessage) error {
		s, err := parseStock(raw, now)
		if err != nil {
			return err
		}
		out = append(out, s)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

// FetchUSQuotes 批量获取指定证券行情（A股/美股/板块 ETF 通吃）。
// 按路由顺序尝试（腾讯 qt.gtimg / 东财 ulist / 新浪 hq 批量）；
// 腾讯对 100.* 美股指数有专用码（usDJI/usNDX/usINX），新浪不支持该三只。
func FetchUSQuotes(ctx context.Context, secids []string) ([]model.Stock, error) {
	var lastErr error
	for _, src := range pairFor(FeatBatch) {
		var quotes []model.Stock
		var err error
		switch src {
		case SrcTencent:
			quotes, err = tencentBatchQuotes(ctx, secids)
		case SrcSina:
			quotes, err = sinaBatchQuotes(ctx, secids)
		default:
			quotes, err = fetchQuotesEM(ctx, secids)
		}
		if err == nil {
			return quotes, nil
		}
		lastErr = err
		log.Printf("[Source] 批量行情源 %s 失败: %v", src, err)
	}
	return nil, lastErr
}

func fetchQuotesEM(ctx context.Context, secids []string) ([]model.Stock, error) {
	if len(secids) == 0 {
		return nil, nil
	}
	url := fmt.Sprintf(
		"https://push2.eastmoney.com/api/qt/ulist.np/get?fltt=2&secids=%s&fields=f2,f3,f4,f6,f8,f9,f12,f13,f14,f15,f16,f17,f18,f20,f23",
		joinComma(secids),
	)
	body, err := getJSONRaw(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("ulist 请求失败: %w", err)
	}
	var root emRoot
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("ulist 解析失败: %w", err)
	}
	now := time.Now()
	out := make([]model.Stock, 0, len(root.Data.Diff))
	for _, d := range root.Data.Diff {
		secid := secidOf(d.F13, d.F12)
		out = append(out, model.Stock{
			SecID: secid, Code: d.F12, Name: d.F14,
			Price: d.F2, Pct: d.F3, Change: d.F4,
			Open: d.F17, High: d.F15, Low: d.F16, PrevClose: d.F18,
			UpdateTime: now,
		})
	}
	return out, nil
}

func joinComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}
