package source

import (
	"strings"
	"testing"
)

// TestSinaSymbolMapping 验证 secid ↔ 新浪 symbol 双向映射。
func TestSinaSymbolMapping(t *testing.T) {
	cases := []struct {
		secid  string
		symbol string
	}{
		{"1.000001", "sh000001"},
		{"0.399001", "sz399001"},
		{"0.399006", "sz399006"},
		{"1.000300", "sh000300"},
	}
	for _, c := range cases {
		if got := sinaSymbol(c.secid); got != c.symbol {
			t.Errorf("sinaSymbol(%s)=%s want %s", c.secid, got, c.symbol)
		}
		if got := secidFromSymbol(c.symbol); got != c.secid {
			t.Errorf("secidFromSymbol(%s)=%s want %s", c.symbol, got, c.secid)
		}
	}
}

// TestSinaQuoteParse 验证新浪 GBK 行情行解析（regexp + 逗号切分）。
func TestSinaQuoteParse(t *testing.T) {
	// 模拟解码后的 UTF-8 响应（正则按行匹配）
	raw := `var hq_str_s_sh000001="上证指数,3680.6428,16.9193,0.46,4331200,73615479";
var hq_str_s_sz399006="创业板指,2572.07,77.670,3.11,5072578,20582648";`

	matches := sinaLineRe.FindAllStringSubmatch(raw, -1)
	if len(matches) != 2 {
		t.Fatalf("匹配到 %d 行，期望 2", len(matches))
	}
	if matches[1][2] != "sz399006" {
		t.Errorf("第2行 symbol=%s 期望 sz399006", matches[1][2])
	}
	// 字段：名称,最新价,涨跌额,涨跌幅,成交量,成交额
	fields := strings.Split(matches[0][3], ",")
	if fields[1] != "3680.6428" || fields[3] != "0.46" {
		t.Errorf("字段解析错误: %v", fields)
	}
	if secidFromSymbol(matches[0][2]) != "1.000001" {
		t.Errorf("secid 映射错误: %s", matches[0][2])
	}
}

// TestParseSinaNodeStocks 验证 Market_Center 节点列表解析（A股 hs_a 与 ETF etf_hq_fund 共用）。
// 覆盖：沪/深 symbol→secid、字符串与数字两种 JSON 形态（flexF）、市值万元→元换算。
func TestParseSinaNodeStocks(t *testing.T) {
	raw := `[
 {"symbol":"sh510300","code":"510300","name":"沪深300ETF华泰柏瑞","trade":"4.060","pricechange":-0.010,
  "changepercent":-0.25,"settlement":"4.070","open":"4.07","high":"4.08","low":"4.05",
  "amount":1234567890,"turnoverratio":1.23,"per":0,"pb":0,"mktcap":9792552.6},
 {"symbol":"sz159915","code":"159915","name":"创业板ETF易方达","trade":2.31,"pricechange":0.01,
  "changepercent":0.43,"settlement":2.30,"open":2.30,"high":2.32,"low":2.29,
  "amount":987654321,"turnoverratio":2.5,"per":0,"pb":0,"mktcap":"488936.95"}]`
	out, err := parseSinaNodeStocks([]byte(raw))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("得到 %d 条，期望 2", len(out))
	}
	a, b := out[0], out[1]
	if a.SecID != "1.510300" || b.SecID != "0.159915" {
		t.Errorf("secid 映射错误: %s / %s", a.SecID, b.SecID)
	}
	if a.Price != 4.060 || a.Pct != -0.25 || a.Turnover != 1.23 {
		t.Errorf("数值解析错误: price=%.3f pct=%.2f turnover=%.2f", a.Price, a.Pct, a.Turnover)
	}
	if b.Price != 2.31 { // 数字形态（非字符串）也要能解析
		t.Errorf("数字形态解析错误: %.3f", b.Price)
	}
	if a.MarketCap != 9792552.6*1e4 {
		t.Errorf("市值万元换算错误: got %v want %v", a.MarketCap, 9792552.6*1e4)
	}
	if b.MarketCap != 488936.95*1e4 {
		t.Errorf("字符串市值换算错误: got %v", b.MarketCap)
	}
	if _, err := parseSinaNodeStocks([]byte(`[]`)); err == nil {
		t.Error("空数组应返回错误")
	}
	if _, err := parseSinaNodeStocks([]byte(`not json`)); err == nil {
		t.Error("非法 JSON 应返回错误")
	}
}
