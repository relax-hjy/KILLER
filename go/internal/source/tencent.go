package source

// 腾讯财经数据源（2026-11-12 实测）：
//   - 实时行情: qt.gtimg.cn/q=sh000001,...（GBK 文本，~ 分隔字段）
//   - 日K线:    web.ifzq.gtimg.cn/appstock/app/fqkline/get（JSON，day/qfqday）
//   - 当日分时: web.ifzq.gtimg.cn/appstock/app/minute/query（JSON，分钟条）
//
// 接线状态：
//   - batch（批量行情）已接入 FetchUSQuotes 路由，为首选源
//   - Source 接口三方法（指数行情/日K/分时）已实现并由单测覆盖，
//     接入点在 web/server.go 与 monitor/watchdog.go——因并发会话占用暂缓接线

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"killer/internal/model"
)

const (
	tencentQuoteURL  = "https://qt.gtimg.cn/q=%s"
	tencentKlineURL  = "https://web.ifzq.gtimg.cn/appstock/app/fqkline/get?param=%s,day,,,%d,qfq"
	tencentMinuteURL = "https://web.ifzq.gtimg.cn/appstock/app/minute/query?code=%s"
	tencentUA        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64)"
)

// tencentLineRe 匹配 `v_SYMBOL="字段串"` 行（美股代码含点号，如 usBRK.B）。
var tencentLineRe = regexp.MustCompile(`v_([a-zA-Z0-9.]+)="([^"]*)"`)

// TencentSource 腾讯财经数据源。
type TencentSource struct{}

// Name 数据源标识。
func (t *TencentSource) Name() string { return "tencent" }

// tencentSymbol secid → 腾讯 symbol。不支持返回 ("", false)（如东财板块 90.BK）。
// 美股三大指数东财用 100.* 市场号，腾讯为专用码 usDJI/usNDX/usINX。
func tencentSymbol(secid string) (string, bool) {
	switch {
	case strings.HasPrefix(secid, "1."):
		return "sh" + secid[2:], true
	case strings.HasPrefix(secid, "0."):
		return "sz" + secid[2:], true
	case strings.HasPrefix(secid, "105."), strings.HasPrefix(secid, "106."), strings.HasPrefix(secid, "107."):
		return "us" + secid[4:], true
	}
	switch secid {
	case "100.DJIA":
		return "usDJI", true
	case "100.NDX":
		return "usNDX", true
	case "100.SPX":
		return "usINX", true
	}
	return "", false
}

// tencentGet GET 腾讯接口并做 GBK → UTF-8 转换。
func tencentGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", tencentUA)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tencent 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tencent HTTP %d", resp.StatusCode)
	}
	// 必须 ReadAll：单次 Read 在大响应（如 101 只批量）下会因 TCP 分段只读到部分
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("tencent 读取响应失败: %w", err)
	}
	return gbkToUTF8(body), nil
}

// parseTencentStock 解析腾讯行情单条（~ 分隔）为 Stock。
// 关键下标（2026-11-12 实测 A股/美股一致）：
//
//	[1]名称 [2]代码 [3]最新价 [4]昨收 [5]今开 [31]涨跌额 [32]涨跌幅
//	[33]最高 [34]最低 [35]A股="价/量/额" 或 美股=币种 [37]美股成交额
//	[38]换手 [39]PE [43]PB [45]总市值（亿）
func parseTencentStock(payload string) (model.Stock, bool) {
	p := strings.Split(payload, "~")
	if len(p) < 36 { // 至少到 [35]（成交额/币种字段）
		return model.Stock{}, false
	}
	st := model.Stock{
		Name:       p[1],
		Price:      f(p[3]),
		PrevClose:  f(p[4]),
		Open:       f(p[5]),
		Change:     f(p[31]),
		Pct:        f(p[32]),
		High:       f(p[33]),
		Low:        f(p[34]),
		UpdateTime: time.Now(),
	}
	// 成交额（元）：A股 [35] 第三段即精确元值；美股 [35]=币种，[37] 即金额
	if parts := strings.Split(p[35], "/"); len(parts) == 3 {
		st.Amount = f(parts[2])
	} else if len(p) > 37 {
		st.Amount = f(p[37])
	}
	if len(p) > 38 {
		st.Turnover = f(p[38])
	}
	if len(p) > 39 {
		st.PE = f(p[39])
	}
	if len(p) > 43 {
		st.PB = f(p[43])
	}
	if len(p) > 45 {
		st.MarketCap = f(p[45]) * 1e8 // 亿 → 元
	}
	return st, true
}

// parseTencentBatch 解析批量行情响应，按 symbol→secid 归位。
// 不在请求范围的行（如 pv_none_match）与空 payload 跳过。
func parseTencentBatch(body []byte, symToSecid map[string]string) []model.Stock {
	var out []model.Stock
	for _, m := range tencentLineRe.FindAllStringSubmatch(string(body), -1) {
		sym, payload := m[1], m[2]
		secid, ok := symToSecid[sym]
		if !ok || payload == "" {
			continue
		}
		st, ok := parseTencentStock(payload)
		if !ok {
			continue
		}
		st.SecID = secid
		st.Code = secid[strings.Index(secid, ".")+1:]
		out = append(out, st)
	}
	return out
}

// tencentBatchQuotes 腾讯批量行情（A股/美股/ETF/美股指数通吃，2026-11-12 实测 14/14 通）。
// 全部 secid 不支持或全部无数据时返回错误 → 路由切下一源。
func tencentBatchQuotes(ctx context.Context, secids []string) ([]model.Stock, error) {
	symToSecid := map[string]string{}
	syms := make([]string, 0, len(secids))
	for _, id := range secids {
		if sym, ok := tencentSymbol(id); ok {
			syms = append(syms, sym)
			symToSecid[sym] = id
		}
	}
	if len(syms) == 0 {
		return nil, fmt.Errorf("tencent 无可用 symbol")
	}
	body, err := tencentGet(ctx, fmt.Sprintf(tencentQuoteURL, strings.Join(syms, ",")))
	if err != nil {
		return nil, err
	}
	out := parseTencentBatch(body, symToSecid)
	if len(out) == 0 {
		return nil, fmt.Errorf("tencent 行情返回空")
	}
	return out, nil
}

// FetchQuotes 批量指数实时行情（Source 接口，接线点在 watchdog）。
func (t *TencentSource) FetchQuotes(ctx context.Context, secids []string) ([]model.Quote, error) {
	symToSecid := map[string]string{}
	syms := make([]string, 0, len(secids))
	for _, id := range secids {
		if sym, ok := tencentSymbol(id); ok {
			syms = append(syms, sym)
			symToSecid[sym] = id
		}
	}
	if len(syms) == 0 {
		return nil, fmt.Errorf("tencent 无可用 symbol")
	}
	body, err := tencentGet(ctx, fmt.Sprintf(tencentQuoteURL, strings.Join(syms, ",")))
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []model.Quote
	for _, st := range parseTencentBatch(body, symToSecid) {
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
		return nil, fmt.Errorf("tencent 行情返回空")
	}
	return out, nil
}

type tencentKlineRoot struct {
	Data map[string]struct {
		Day    [][]string `json:"day"`
		Qfqday [][]string `json:"qfqday"`
	} `json:"data"`
}

// parseTencentKline 解析日K响应。条目格式 [日期,开,收,高,低,量]，升序。
// 个股返回 qfqday（前复权），指数返回 day，两者都试。
func parseTencentKline(body []byte, sym string) ([]model.Kline, error) {
	var root tencentKlineRoot
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("tencent K线解析失败: %w", err)
	}
	d, ok := root.Data[sym]
	if !ok {
		return nil, fmt.Errorf("tencent K线缺少 %s 数据", sym)
	}
	rows := d.Qfqday
	if len(rows) == 0 {
		rows = d.Day
	}
	out := make([]model.Kline, 0, len(rows))
	for _, r := range rows {
		if len(r) < 6 {
			continue
		}
		k := model.Kline{Date: r[0]}
		k.Open, _ = strconv.ParseFloat(r[1], 64)
		k.Close, _ = strconv.ParseFloat(r[2], 64)
		k.High, _ = strconv.ParseFloat(r[3], 64)
		k.Low, _ = strconv.ParseFloat(r[4], 64)
		k.Volume, _ = strconv.ParseFloat(r[5], 64)
		out = append(out, k)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("tencent K线返回空")
	}
	return out, nil
}

// FetchKline 日K线（Source 接口，接线点在 server.handleKline）。
func (t *TencentSource) FetchKline(ctx context.Context, secid string, limit int) ([]model.Kline, error) {
	sym, ok := tencentSymbol(secid)
	if !ok {
		return nil, fmt.Errorf("tencent 不支持 secid %s", secid)
	}
	body, err := tencentGet(ctx, fmt.Sprintf(tencentKlineURL, sym, limit))
	if err != nil {
		return nil, err
	}
	return parseTencentKline(body, sym)
}

type tencentMinuteRoot struct {
	Data map[string]struct {
		Data struct {
			Data []string `json:"data"`
			Date string   `json:"date"`
		} `json:"data"`
	} `json:"data"`
}

// parseTencentMinute 解析当日分时。条目 "0930 3673.06 量 额"，date="20261112"。
// 分钟只有最新价 → OHLC 同值（前端分时仅用 close 画线）。
// 日期为空或不足 2 条（美股休市仅一条收盘快照）报错 → 路由切下一源。
func parseTencentMinute(body []byte, sym string) ([]model.Kline, error) {
	var root tencentMinuteRoot
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("tencent 分时解析失败: %w", err)
	}
	d, ok := root.Data[sym]
	if !ok || len(d.Data.Date) != 8 || len(d.Data.Data) < 2 {
		return nil, fmt.Errorf("tencent 分时数据不足")
	}
	date := d.Data.Date
	day := date[:4] + "-" + date[4:6] + "-" + date[6:8]
	out := make([]model.Kline, 0, len(d.Data.Data))
	for _, line := range d.Data.Data {
		p := strings.Fields(line)
		if len(p) < 2 || len(p[0]) != 4 {
			continue
		}
		price := f(p[1])
		k := model.Kline{
			Date: day + " " + p[0][:2] + ":" + p[0][2:],
			Open: price, High: price, Low: price, Close: price,
		}
		if len(p) > 2 {
			k.Volume = f(p[2])
		}
		out = append(out, k)
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("tencent 分时数据不足")
	}
	return out, nil
}

// FetchMinuteKline 当日分时（Source 接口，接线点在 server.handleIntraday）。
func (t *TencentSource) FetchMinuteKline(ctx context.Context, secid string) ([]model.Kline, error) {
	sym, ok := tencentSymbol(secid)
	if !ok {
		return nil, fmt.Errorf("tencent 不支持 secid %s", secid)
	}
	body, err := tencentGet(ctx, fmt.Sprintf(tencentMinuteURL, sym))
	if err != nil {
		return nil, err
	}
	return parseTencentMinute(body, sym)
}
