package source

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

// EastmoneySource 东方财富 push2/push2his 数据源（主源）。
// 接口结构实测于 2026-11：ulist.np/get 返回 data.diff 数组。
type EastmoneySource struct{}

func (e *EastmoneySource) Name() string { return "eastmoney" }

const (
	emQuoteURL = "https://push2.eastmoney.com/api/qt/ulist.np/get?fltt=2&secids=%s" +
		"&fields=f2,f3,f4,f12,f13,f14,f15,f16,f17,f18,f5"
	emKlineURL = "https://push2his.eastmoney.com/api/qt/stock/kline/get?secid=%s" +
		"&klt=%d&fqt=0&lmt=%d&end=20500101" +
		"&fields1=f1,f2,f3,f4,f5,f6&fields2=f51,f52,f53,f54,f55,f56,f57"
)

type emRoot struct {
	Data struct {
		Diff []struct {
			F2  float64 `json:"f2"`  // 最新价
			F3  float64 `json:"f3"`  // 涨跌幅 %
			F4  float64 `json:"f4"`  // 涨跌额
			F5  float64 `json:"f5"`  // 成交额（手？实测 f5 为成交额字段，取不到为 0）
			F12 string  `json:"f12"` // 代码
			F13 int     `json:"f13"` // 市场 1=沪 0=深
			F14 string  `json:"f14"` // 名称
			F15 float64 `json:"f15"` // 最高
			F16 float64 `json:"f16"` // 最低
			F17 float64 `json:"f17"` // 今开
			F18 float64 `json:"f18"` // 昨收
		} `json:"diff"`
	} `json:"data"`
}

func getJSON(ctx context.Context, url string, ua string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ua)
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
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

func (e *EastmoneySource) FetchQuotes(ctx context.Context, secids []string) ([]model.Quote, error) {
	url := fmt.Sprintf(emQuoteURL, strings.Join(secids, ","))
	body, err := getJSON(ctx, url, "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	if err != nil {
		return nil, fmt.Errorf("eastmoney 行情请求失败: %w", err)
	}
	var root emRoot
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("eastmoney 行情解析失败: %w", err)
	}
	now := time.Now()
	out := make([]model.Quote, 0, len(root.Data.Diff))
	for _, d := range root.Data.Diff {
		secid := secidOf(d.F13, d.F12)
		q := model.Quote{
			SecID:     secid,
			Code:      d.F12,
			Name:      d.F14,
			Price:     d.F2,
			Change:    d.F4,
			Pct:       d.F3,
			Open:      d.F17,
			High:      d.F15,
			Low:       d.F16,
			PrevClose: d.F18,
			Volume:    d.F5,
			UpdateTime: now,
		}
		if d.F18 > 0 {
			q.Amplitude = (d.F15 - d.F16) / d.F18 * 100
		}
		out = append(out, q)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("eastmoney 返回空数据")
	}
	return out, nil
}

type emKlineRoot struct {
	Data struct {
		Klines []string `json:"klines"` // "date,open,close,high,low,vol,amount"
	} `json:"data"`
}

func (e *EastmoneySource) FetchKline(ctx context.Context, secid string, limit int) ([]model.Kline, error) {
	return e.fetchKline(ctx, secid, 101, limit)
}

// FetchMinuteKline 当日分时（klt=1，1 分钟线）。
// 接口返回含历史所有分钟线（lmt 从最新往回数），过滤出最新交易日。
func (e *EastmoneySource) FetchMinuteKline(ctx context.Context, secid string) ([]model.Kline, error) {
	// 一天约 241 分钟（含集合竞价后 9:31 起），取 800 根覆盖停牌/半日等场景
	all, err := e.fetchKline(ctx, secid, 1, 800)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("eastmoney 分时返回空")
	}
	// 最新一根的日期即最新交易日（格式 "2026-11-11 09:31" → 取前 10 位）
	latestDay := all[len(all)-1].Date[:10]
	out := make([]model.Kline, 0, 300)
	for _, k := range all {
		if len(k.Date) >= 10 && k.Date[:10] == latestDay {
			out = append(out, k)
		}
	}
	return out, nil
}

func (e *EastmoneySource) fetchKline(ctx context.Context, secid string, klt, limit int) ([]model.Kline, error) {
	url := fmt.Sprintf(emKlineURL, secid, klt, limit)
	body, err := getJSON(ctx, url, "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	if err != nil {
		return nil, fmt.Errorf("eastmoney K线请求失败: %w", err)
	}
	var root emKlineRoot
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("eastmoney K线解析失败: %w", err)
	}
	out := make([]model.Kline, 0, len(root.Data.Klines))
	for _, line := range root.Data.Klines {
		parts := strings.Split(line, ",")
		if len(parts) < 6 {
			continue
		}
		k := model.Kline{Date: parts[0]}
		k.Open, _ = strconv.ParseFloat(parts[1], 64)
		k.Close, _ = strconv.ParseFloat(parts[2], 64)
		k.High, _ = strconv.ParseFloat(parts[3], 64)
		k.Low, _ = strconv.ParseFloat(parts[4], 64)
		k.Volume, _ = strconv.ParseFloat(parts[5], 64)
		out = append(out, k)
	}
	return out, nil
}

// secidOf 拼装 secid：1=沪 0=深。
func secidOf(market int, code string) string {
	if market == 1 {
		return "1." + code
	}
	return "0." + code
}
