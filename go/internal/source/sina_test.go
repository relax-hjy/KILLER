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
