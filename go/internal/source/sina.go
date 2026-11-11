package source

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"killer/internal/model"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// SinaSource 新浪财经数据源（备用）。
// hq.sinajs.cn 返回 GBK 编码的 JS 变量赋值，字段为
// "名称,最新价,涨跌额,涨跌幅,成交量(手),成交额(元)"。
type SinaSource struct{}

func (s *SinaSource) Name() string { return "sina" }

var (
	sinaLineRe = regexp.MustCompile(`hq_str_(s_)?([a-z]{2}\d{6})="([^"]*)"`)
	gbkDecoder = simplifiedchinese.GBK.NewDecoder()
)

func (s *SinaSource) FetchQuotes(ctx context.Context, secids []string) ([]model.Quote, error) {
	symbols := make([]string, 0, len(secids))
	for _, id := range secids {
		symbols = append(symbols, "s_"+sinaSymbol(id))
	}
	url := "https://hq.sinajs.cn/list=" + strings.Join(symbols, ",")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sina 行情请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sina HTTP %d", resp.StatusCode)
	}

	buf := make([]byte, 1<<20)
	n, _ := resp.Body.Read(buf)
	raw := buf[:n]
	// GBK → UTF-8（纯 ASCII 部分不受影响）
	if decoded, err := gbkDecoder.Bytes(raw); err == nil {
		raw = decoded
	}

	now := time.Now()
	var out []model.Quote
	for _, m := range sinaLineRe.FindAllStringSubmatch(string(raw), -1) {
		symbol := m[2]      // 如 sh000001
		fields := strings.Split(m[3], ",")
		if len(fields) < 6 {
			continue
		}
		price, _ := strconv.ParseFloat(fields[1], 64)
		change, _ := strconv.ParseFloat(fields[2], 64)
		pct, _ := strconv.ParseFloat(fields[3], 64)
		// s_ 接口无开高低昨收 → 振幅置 0（备用源尽力而为）
		out = append(out, model.Quote{
			SecID:      secidFromSymbol(symbol),
			Code:       symbol[2:],
			Name:       fields[0],
			Price:      price,
			Change:     change,
			Pct:        pct,
			UpdateTime: now,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sina 返回空数据")
	}
	return out, nil
}

type sinaKlineBar struct {
	Day   string  `json:"day"`
	Open  float64 `json:"open"`
	High  float64 `json:"high"`
	Low   float64 `json:"low"`
	Close float64 `json:"close"`
}

// FetchKline 新浪日 K（scale=240 即日线）。
// JSONP 包裹，需截取括号内 JSON。
func (s *SinaSource) FetchKline(ctx context.Context, secid string, limit int) ([]model.Kline, error) {
	symbol := sinaSymbol(secid)
	url := fmt.Sprintf(
		"https://quotes.sina.cn/cn/api/jsonp_v2.php/v=%d/CN_MarketDataService.getKLineData?symbol=%s&scale=240&datalen=%d",
		time.Now().Unix(), symbol, limit,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sina K线请求失败: %w", err)
	}
	defer resp.Body.Close()

	buf := make([]byte, 8<<20)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	// 提取 jsonp 括号内的数组
	start := strings.Index(body, "[")
	end := strings.LastIndex(body, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("sina K线响应格式异常")
	}
	var bars []sinaKlineBar
	if err := json.Unmarshal([]byte(body[start:end+1]), &bars); err != nil {
		return nil, fmt.Errorf("sina K线解析失败: %w", err)
	}
	out := make([]model.Kline, 0, len(bars))
	for _, b := range bars {
		out = append(out, model.Kline{
			Date: b.Day, Open: b.Open, Close: b.Close, High: b.High, Low: b.Low,
		})
	}
	return out, nil
}

// FetchMinuteKline 新浪无 1 分钟分时接口（scale 仅支持 5/15/30/60）。
// 分时功能明确走主源，此处返回可识别的错误。
func (s *SinaSource) FetchMinuteKline(ctx context.Context, secid string) ([]model.Kline, error) {
	return nil, fmt.Errorf("sina 不支持分时数据（仅主源 eastmoney 提供）")
}

// sinaSymbol secid → 新浪 symbol（1.x=sh000001, 0.x=sz399001）。
func sinaSymbol(secid string) string {
	if strings.HasPrefix(secid, "1.") {
		return "sh" + secid[2:]
	}
	return "sz" + secid[2:]
}

// secidFromSymbol 新浪 symbol → secid。
func secidFromSymbol(sym string) string {
	if strings.HasPrefix(sym, "sh") {
		return "1." + sym[2:]
	}
	return "0." + sym[2:]
}
