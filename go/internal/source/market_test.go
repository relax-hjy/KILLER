package source

import "testing"

// TestIsAShareSecid 锁定搜索联想的 A 股过滤口径。
// 东财分类字段不可靠（科创板 Classify 是数字"23"、京 A 是"NEEQ"），所以按 secid 代码段判定，
// 必须同时命中沪深主板/科创板/创业板/北交所，并挡掉指数、ETF、B 股、港股美股、板块。
func TestIsAShareSecid(t *testing.T) {
	cases := []struct {
		secid string
		want  bool
	}{
		{"1.600519", true},   // 沪主板
		{"1.688256", true},   // 科创板（曾因按 Classify 白名单而整体漏掉）
		{"1.689009", true},   // 科创板 CDR
		{"0.002594", true},   // 深主板
		{"0.301308", true},   // 创业板
		{"0.920002", true},   // 北交所
		{"1.510300", false},  // 沪 ETF
		{"0.159915", false},  // 深 ETF
		{"1.000300", false},  // 沪深300 指数
		{"0.399006", false},  // 创业板指
		{"1.900901", false},  // 沪 B 股
		{"0.200011", false},  // 深 B 股
		{"0.835185", false},  // 新三板老代码
		{"116.00981", false}, // 港股
		{"105.AAPL", false},  // 美股
		{"90.BK0935", false}, // 板块
		{"600519", false},    // 缺市场号
		{"", false},
	}
	for _, c := range cases {
		if got := isAShareSecid(c.secid); got != c.want {
			t.Errorf("isAShareSecid(%q) = %v, want %v", c.secid, got, c.want)
		}
	}
}
