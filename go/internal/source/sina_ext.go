package source

// 新浪兜底实现：东财限流/故障时保功能可用。
// 2026-11-11 实测通过的接口：
//   - A股列表: money.finance.sina.com.cn Market_Center.getHQNodeData（UTF-8 JSON）
//   - A股总数: Market_Center.getHQNodeStockCount
//   - 行业板块: vip.stock.finance.sina.com.cn q/view/newSinaHy.php（GBK JS 赋值）
//   - 概念板块: vip.stock.finance.sina.com.cn q/view/newFLJK.php?param=class（GBK JS 赋值）
//   - 行情批查: hq.sinajs.cn/list=sh600519,gb_aapl（GBK，个股与美股混排）

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"killer/internal/model"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// hqRe 匹配 hq.sinajs.cn 任意 symbol 行（sh600519 / gb_aapl 通吃）。
var hqRe = regexp.MustCompile(`hq_str_([a-z0-9_.]+)="([^"]*)"`)

const (
	sinaStockListURL  = "https://money.finance.sina.com.cn/quotes_service/api/json_v2.php/Market_Center.getHQNodeData?page=%d&num=%d&sort=%s&asc=%d&node=hs_a&symbol="
	sinaStockCountURL = "https://money.finance.sina.com.cn/quotes_service/api/json_v2.php/Market_Center.getHQNodeStockCount?node=hs_a"
	sinaIndustryURL   = "https://vip.stock.finance.sina.com.cn/q/view/newSinaHy.php"
	sinaConceptURL    = "https://vip.stock.finance.sina.com.cn/q/view/newFLJK.php?param=class"
	sinaHQURL         = "https://hq.sinajs.cn/list="
)

// sinaGet 带新浪必需 Referer 的 GET（GBK 响应原样返回字节）。
func sinaGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sina HTTP %d", resp.StatusCode)
	}
	// ReadAll 而非单次 Read：个股列表等大响应会因 TCP 分段截断
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func gbkToUTF8(b []byte) []byte {
	if dec, err := simplifiedchinese.GBK.NewDecoder().Bytes(b); err == nil {
		return dec
	}
	return b
}

func f(str string) float64 {
	v, _ := strconv.ParseFloat(str, 64)
	return v
}

// flexF 兼容 JSON 数字与字符串两种形态（新浪列表同一字段两种都出现）。
type flexF float64

func (x *flexF) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, `"`)
	if len(b) == 0 || string(b) == "null" || string(b) == "--" {
		*x = 0
		return nil
	}
	v, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		*x = 0
		return nil
	}
	*x = flexF(v)
	return nil
}

// ---- A股个股列表兜底 ----

// sinaStocks 新浪 A 股列表（沪深 5300+，含排序）。
func sinaStocks(ctx context.Context, page, pz int, sortFid string, asc bool) ([]model.Stock, int, error) {
	sortMap := map[string]string{
		"f3": "changepercent", "f6": "amount", "f8": "turnoverratio",
		"f20": "mktcap", "f21": "nmc", "f9": "per", "f12": "symbol",
	}
	s := sortMap[sortFid]
	if s == "" {
		s = "changepercent"
	}
	a := 0
	if asc {
		a = 1
	}
	url := fmt.Sprintf(sinaStockListURL, page, pz, s, a)
	body, err := sinaGet(ctx, url)
	if err != nil {
		return nil, 0, err
	}
	var rows []struct {
		Symbol        string `json:"symbol"`
		Code          string `json:"code"`
		Name          string `json:"name"`
		Trade         flexF  `json:"trade"`
		Pricechange   flexF  `json:"pricechange"`
		Changepercent flexF  `json:"changepercent"`
		Settlement    flexF  `json:"settlement"`
		Open          flexF  `json:"open"`
		High          flexF  `json:"high"`
		Low           flexF  `json:"low"`
		Amount        flexF  `json:"amount"`
		Turnoverratio flexF  `json:"turnoverratio"`
		Per           flexF  `json:"per"`
		Pb            flexF  `json:"pb"`
		Mktcap        flexF  `json:"mktcap"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, 0, fmt.Errorf("sina 列表解析失败: %w", err)
	}
	now := time.Now()
	out := make([]model.Stock, 0, len(rows))
	for _, r := range rows {
		secid := sinaSymbolToSecid(r.Symbol)
		out = append(out, model.Stock{
			SecID: secid, Code: r.Code, Name: r.Name,
			Price: float64(r.Trade), Pct: float64(r.Changepercent), Change: float64(r.Pricechange),
			Open: float64(r.Open), High: float64(r.High), Low: float64(r.Low), PrevClose: float64(r.Settlement),
			Amount: float64(r.Amount), Turnover: float64(r.Turnoverratio),
			PE: float64(r.Per), PB: float64(r.Pb),
			MarketCap:  float64(r.Mktcap) * 1e4, // 新浪 mktcap 单位万元
			UpdateTime: now,
		})
	}
	if len(out) == 0 {
		return nil, 0, fmt.Errorf("sina 列表返回空")
	}
	// 总数（失败则用估算）
	total := 0
	if b2, err := sinaGet(ctx, sinaStockCountURL); err == nil {
		var s2 string
		if json.Unmarshal(b2, &s2) == nil {
			total, _ = strconv.Atoi(s2)
		}
	}
	if total == 0 {
		total = (page-1)*pz + len(out)
		if len(out) == pz {
			total += 1 // 还有下一页
		}
	}
	return out, total, nil
}

// sinaSymbolToSecid sh600519→1.600519 / sz000001→0.000001 / bj→0.
func sinaSymbolToSecid(sym string) string {
	if strings.HasPrefix(sym, "sh") {
		return "1." + sym[2:]
	}
	return "0." + sym[2:]
}

// ---- 板块兜底 ----

// sinaBoards 新浪行业/概念板块。value 格式：
// code,name,家数,均价,均价涨跌,均价涨跌幅,成交量,成交额,领涨股,领涨涨幅,...
// 新浪无板块内涨/跌家数 → UpCount/DownCount 置 -1（前端显示 —）。
func sinaBoards(ctx context.Context, kind, sortStr string) ([]model.Board, error) {
	url := sinaIndustryURL
	if kind == "concept" {
		url = sinaConceptURL
	}
	raw, err := sinaGet(ctx, url)
	if err != nil {
		return nil, err
	}
	text := string(gbkToUTF8(raw))
	// 提取 JS 赋值中的 JSON 对象
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("sina 板块响应格式异常")
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(text[start:end+1]), &m); err != nil {
		return nil, fmt.Errorf("sina 板块解析失败: %w", err)
	}
	out := make([]model.Board, 0, len(m))
	for _, v := range m {
		p := strings.Split(v, ",")
		if len(p) < 6 {
			continue
		}
		out = append(out, model.Board{
			SecID: p[0], Name: p[1],
			Price: f(p[3]), Change: f(p[4]), Pct: f(p[5]),
			UpCount: -1, DownCount: -1,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if sortStr == "pct_asc" {
			return out[i].Pct < out[j].Pct
		}
		return out[i].Pct > out[j].Pct
	})
	return out, nil
}

// ---- 行情批查兜底（A股 + 美股混排）----

// sinaBatchQuotes 新浪批量行情：
//   - "1.600519"/"0.000001" → sh600519/sz000001 个股全字段（[0]名 [1]开 [2]昨收 [3]现价 [4]高 [5]低）
//   - "105/106/107.AAPL"   → gb_aapl 美股（[0]名 [1]现价 [2]涨跌幅 [4]涨跌额 [5]开 [6]高 [7]低 [12]市值）
//   - "100.*" 指数不支持（新浪对应码为空，交给东财主源）
func sinaBatchQuotes(ctx context.Context, secids []string) ([]model.Stock, error) {
	// 入参 secid → hq symbol 映射（用于响应归位，保留原交易所前缀 105/106/107）
	symbolToSecid := map[string]string{}
	var symbols []string
	for _, id := range secids {
		if sym := sinaHQSymbol(id); sym != "" {
			symbols = append(symbols, sym)
			symbolToSecid[sym] = id
		}
	}
	if len(symbols) == 0 {
		return nil, fmt.Errorf("sina 无可用 symbol")
	}
	body, err := sinaGet(ctx, sinaHQURL+strings.Join(symbols, ","))
	if err != nil {
		return nil, err
	}
	text := string(gbkToUTF8(body))
	now := time.Now()

	re := hqRe // `hq_str_(sym)="([^"]*)"`
	var out []model.Stock
	for _, mt := range re.FindAllStringSubmatch(text, -1) {
		sym, payload := mt[1], mt[2]
		if payload == "" {
			continue
		}
		secid, ok := symbolToSecid[sym]
		if !ok {
			continue // 不在请求范围内的 symbol（新浪可能返回别名），丢弃
		}
		p := strings.Split(payload, ",")
		if strings.HasPrefix(sym, "gb_") {
			// 美股
			if len(p) < 8 {
				continue
			}
			symbol := strings.ToUpper(sym[3:])
			price, pct := f(p[1]), f(p[2])
			out = append(out, model.Stock{
				SecID:      secid,
				Code:       symbol,
				Name:       p[0],
				Price:      price,
				Pct:        pct,
				Change:     f(p[4]),
				Open:       f(p[5]),
				High:       f(p[6]),
				Low:        f(p[7]),
				PrevClose:  price - f(p[4]),
				MarketCap:  marketCapOf(p),
				UpdateTime: now,
			})
		} else if strings.HasPrefix(sym, "sh") || strings.HasPrefix(sym, "sz") {
			// A股个股全字段
			if len(p) < 6 {
				continue
			}
			prev := f(p[2])
			price := f(p[3])
			pct := 0.0
			if prev > 0 {
				pct = (price - prev) / prev * 100
			}
			out = append(out, model.Stock{
				SecID:      secid,
				Code:       sym[2:],
				Name:       p[0],
				Price:      price,
				Pct:        pct,
				Change:     price - prev,
				Open:       f(p[1]),
				PrevClose:  prev,
				High:       f(p[4]),
				Low:        f(p[5]),
				UpdateTime: now,
			})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("sina 行情返回空")
	}
	return out, nil
}

// sinaHQSymbol secid → hq.sinajs.cn symbol；不支持返回 ""。
func sinaHQSymbol(secid string) string {
	if strings.HasPrefix(secid, "1.") {
		return "sh" + secid[2:]
	}
	if strings.HasPrefix(secid, "0.") {
		return "sz" + secid[2:]
	}
	if strings.HasPrefix(secid, "105.") || strings.HasPrefix(secid, "106.") || strings.HasPrefix(secid, "107.") {
		return "gb_" + strings.ToLower(secid[4:])
	}
	return ""
}

// marketCapOf gb_ [12]=总市值（美元），非数值返回 0。
func marketCapOf(p []string) float64 {
	if len(p) > 12 {
		return f(p[12])
	}
	return 0
}
