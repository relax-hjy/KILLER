package source

import (
	"testing"
)

// 真实 payload 截取自 2026-11-12 qt.gtimg.cn 实测响应（88 字段），
// 保留到 [45]（总市值）——解析所需的最大下标。
const (
	fxTencentAStock = "1~贵州茅台~600519~1410.27~1402.30~1395.13~32373~15499~16874~1410.27~17~1410.22~1~1410.21~4~1410.19~4~1410.00~1~1410.43~2~1410.45~1~1410.46~1~1410.50~5~1410.55~1~~20261112161452~7.97~0.57~1411.00~1388.11~1410.27/32373/4536814518~32373~453681~0.26~21.46~~1411.00~1388.11~1.63~17629.53~17629.53"
	fxTencentIndex  = "1~上证指数~000001~3655.83~3680.64~3673.06~496533033~0~0~0.00~0~0.00~0~0.00~0~0.00~0~0.00~0~0.00~0~0.00~0~0.00~0~0.00~0~0.00~0~~20261112161427~-24.81~-0.67~3699.58~3647.44~3655.83/496533033/879537113560~496533033~87953711~1.02~16.62~~3699.58~3647.44~1.42~574837.86~642315.87"
	fxTencentUS     = "200~苹果~AAPL.OQ~355.63~348.51~349.55~34453477~0~0~356.13~40~0~0~0~0~0~0~0~0~356.40~200~0~0~0~0~0~0~0~0~~2026-11-11 16:00:01~7.12~2.04~355.91~349.33~USD~34453477~12211384813~0.24~39.60~~39.60~~1.89~51648.53922~51680.79885"
)

// TestTencentSymbolMapping 验证 secid → 腾讯 symbol 映射：
// 沪深加 sh/sz 前缀、美股加 us 前缀、美股三大指数特例、板块不支持。
func TestTencentSymbolMapping(t *testing.T) {
	cases := []struct {
		secid string
		sym   string
		ok    bool
	}{
		{"1.000001", "sh000001", true},
		{"0.399001", "sz399001", true},
		{"1.000300", "sh000300", true},
		{"105.AAPL", "usAAPL", true},
		{"107.XLK", "usXLK", true},
		{"100.DJIA", "usDJI", true},
		{"100.NDX", "usNDX", true},
		{"100.SPX", "usINX", true},
		{"90.BK1128", "", false}, // 东财板块腾讯不支持
	}
	for _, c := range cases {
		got, ok := tencentSymbol(c.secid)
		if got != c.sym || ok != c.ok {
			t.Errorf("tencentSymbol(%s)=(%q,%v) want (%q,%v)", c.secid, got, ok, c.sym, c.ok)
		}
	}
}

// TestParseTencentStockA 验证 A股/指数行情字段解析：
// 价格/涨跌幅/昨收/高/低、[35] 三段式成交额、[45] 总市值（亿→元）。
func TestParseTencentStockA(t *testing.T) {
	st, ok := parseTencentStock(fxTencentAStock)
	if !ok {
		t.Fatal("A股解析失败")
	}
	if st.Name != "贵州茅台" || st.Price != 1410.27 {
		t.Errorf("名称/价格错误: %+v", st)
	}
	if st.PrevClose != 1402.30 || st.Pct != 0.57 || st.Change != 7.97 {
		t.Errorf("昨收/涨跌幅/涨跌额错误: %+v", st)
	}
	if st.High != 1411.00 || st.Low != 1388.11 || st.Open != 1395.13 {
		t.Errorf("高/低/开错误: %+v", st)
	}
	if st.Amount != 4536814518 {
		t.Errorf("成交额=%v want 4536814518（[35] 第三段）", st.Amount)
	}
	if st.MarketCap != 17629.53e8 {
		t.Errorf("市值=%v want %v（[45] 亿→元）", st.MarketCap, 17629.53e8)
	}
	if st.Turnover != 0.26 || st.PE != 21.46 || st.PB != 1.63 {
		t.Errorf("换手/PE/PB 错误: %+v", st)
	}

	// 指数：字段布局一致，[45]=总市值（上证全市场）
	idx, ok := parseTencentStock(fxTencentIndex)
	if !ok || idx.Name != "上证指数" || idx.Pct != -0.67 {
		t.Errorf("指数解析错误: %+v ok=%v", idx, ok)
	}
	if idx.Amount != 879537113560 {
		t.Errorf("指数成交额=%v want 879537113560", idx.Amount)
	}
}

// TestParseTencentStockUS 验证美股字段差异：[35]=币种（非三段式），
// 成交额走 [37] 兜底。
func TestParseTencentStockUS(t *testing.T) {
	st, ok := parseTencentStock(fxTencentUS)
	if !ok {
		t.Fatal("美股解析失败")
	}
	if st.Name != "苹果" || st.Price != 355.63 || st.Pct != 2.04 {
		t.Errorf("名称/价格/涨跌幅错误: %+v", st)
	}
	if st.Amount != 12211384813 {
		t.Errorf("成交额=%v want 12211384813（[37] 兜底）", st.Amount)
	}
	if st.PB != 1.89 {
		t.Errorf("PB=%v want 1.89", st)
	}
	if st.MarketCap != 51680.79885e8 {
		t.Errorf("市值=%v want %v", st.MarketCap, 51680.79885e8)
	}
}

// TestParseTencentBatch 验证批量响应按 symbol→secid 归位、
// 未知行（pv_none_match）与空 payload 跳过。
func TestParseTencentBatch(t *testing.T) {
	body := `v_sh000001="` + fxTencentIndex + `"
v_usAAPL="` + fxTencentUS + `"
v_sz000002="1~万科A~000002~"
v_pv_none_match="1"
`
	symToSecid := map[string]string{"sh000001": "1.000001", "usAAPL": "105.AAPL", "sz000002": "0.000002"}
	out := parseTencentBatch([]byte(body), symToSecid)
	if len(out) != 2 {
		t.Fatalf("得到 %d 条，期望 2（空 payload 与未知行应跳过）: %+v", len(out), out)
	}
	if out[0].SecID != "1.000001" || out[0].Code != "000001" || out[0].Price != 3655.83 {
		t.Errorf("指数归位错误: %+v", out[0])
	}
	if out[1].SecID != "105.AAPL" || out[1].Code != "AAPL" {
		t.Errorf("美股归位错误: %+v", out[1])
	}
}

// TestParseTencentMinute 验证分时解析：date 拼日期、"HHMM 价 量 额" 切字段、
// OHLC 同值；日期缺失（美股休市）报错。
func TestParseTencentMinute(t *testing.T) {
	body := `{"code":0,"msg":"","data":{"sh000001":{"data":{"data":["0930 3673.06 4175936 6758673963.30","0931 3676.37 9547337 14652455816.60","1500 3655.83 496533033 879537113560.40"],"date":"20261112"}}}}`
	out, err := parseTencentMinute([]byte(body), "sh000001")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("得到 %d 条，期望 3", len(out))
	}
	if out[0].Date != "2026-11-12 09:30" || out[0].Close != 3673.06 {
		t.Errorf("首条错误: %+v", out[0])
	}
	if out[2].Date != "2026-11-12 15:00" {
		t.Errorf("末条时间错误: %s", out[2].Date)
	}
	if out[0].Open != out[0].Close || out[0].High != out[0].Close {
		t.Errorf("OHLC 应同值: %+v", out[0])
	}

	// 美股休市仅一条收盘快照且 date 为空 → 报错
	usBody := `{"code":0,"msg":"","data":{"usAAPL":{"data":{"data":["1600 355.63 34453477"],"date":""}}}}`
	if _, err := parseTencentMinute([]byte(usBody), "usAAPL"); err == nil {
		t.Error("数据不足应报错")
	}
}

// TestParseTencentKline 验证日K解析：qfqday 优先（个股）、day 兜底（指数）、
// 条目 [日期,开,收,高,低,量]。
func TestParseTencentKline(t *testing.T) {
	body := `{"data":{"sh600519":{"qfqday":[["2026-11-11","1402.30","1410.27","1411.00","1388.11","32373"]],"day":[["2026-11-10","1400.00","1402.30","1415.00","1395.00","30000"]]}}}`
	out, err := parseTencentKline([]byte(body), "sh600519")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(out) != 1 || out[0].Date != "2026-11-11" || out[0].Close != 1410.27 {
		t.Errorf("qfqday 应优先: %+v", out)
	}

	// 指数只有 day
	idxBody := `{"data":{"sh000001":{"day":[["2026-11-11","3673.06","3655.83","3699.58","3647.44","496533033"]]}}}`
	out2, err := parseTencentKline([]byte(idxBody), "sh000001")
	if err != nil || len(out2) != 1 || out2[0].High != 3699.58 {
		t.Errorf("day 兜底错误: %+v err=%v", out2, err)
	}

	// 缺 symbol 报错
	if _, err := parseTencentKline([]byte(idxBody), "sz399001"); err == nil {
		t.Error("缺 symbol 应报错")
	}
}
