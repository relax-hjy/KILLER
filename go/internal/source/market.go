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
// 按路由顺序尝试（东财/新浪），首个成功即返回。
func FetchBoards(ctx context.Context, kind, sort string) ([]model.Board, error) {
	var lastErr error
	for _, src := range pairFor(FeatBoards) {
		var boards []model.Board
		var err error
		switch src {
		case SrcSina:
			boards, err = sinaBoards(ctx, kind, sort)
		default:
			boards, err = fetchBoardsEM(ctx, kind, sort)
		}
		if err == nil {
			return boards, nil
		}
		lastErr = err
		log.Printf("[Source] 板块源 %s 失败: %v", src, err)
	}
	return nil, lastErr
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

// FetchBoardMembers 拉取板块成分股（东财专属，新浪无对应接口），按涨幅降序分页取全。
// secid 形如 90.BK1128；成分股的流通市值(f21)用于前端计算权重占比。
func FetchBoardMembers(ctx context.Context, secid string) ([]model.Stock, error) {
	if !strings.HasPrefix(secid, "90.BK") {
		return nil, fmt.Errorf("非法板块 secid: %s（应为 90.BKxxxx）", secid)
	}
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
