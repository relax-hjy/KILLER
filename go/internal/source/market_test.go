package source

import "testing"

// TestIsETFSecid 锁定 ETF 代码段白名单口径（列表过滤/看门狗分类/前端卡片同用）。
// 2026-11-19 补 560/561/562（2023 年起沪市新发段，原遗漏导致东财列表过滤掉 116 只）。
func TestIsETFSecid(t *testing.T) {
	cases := []struct {
		secid string
		want  bool
	}{
		{"1.510300", true},  // 沪宽基
		{"1.511880", true},  // 沪债券
		{"1.588000", true},  // 沪科创
		{"1.563000", true},  // 中证A50
		{"1.560010", true},  // 沪新段：中证1000ETF广发
		{"1.561980", true},  // 沪新段：半导体设备ETF招商
		{"1.562500", true},  // 沪新段：机器人ETF华夏
		{"0.159915", true},  // 深创业板
		{"0.150001", true},  // 深老段
		{"0.152010", true},  // 深中证A50
		{"1.600519", false}, // 沪主板股票
		{"0.000001", false}, // 深主板股票
		{"1.000300", false}, // 沪深300 指数
		{"0.399006", false}, // 创业板指
		{"105.AAPL", false}, // 美股
		{"1.51030", false},  // 非 6 位
		{"510300", false},   // 缺市场号
		{"", false},
	}
	for _, c := range cases {
		if got := IsETFSecid(c.secid); got != c.want {
			t.Errorf("IsETFSecid(%q) = %v, want %v", c.secid, got, c.want)
		}
	}
}

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
