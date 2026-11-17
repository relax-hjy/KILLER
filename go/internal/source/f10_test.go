package source

import "testing"

func TestF10Codes(t *testing.T) {
	cases := []struct {
		secid, em, secu string
		ok              bool
	}{
		{"1.600519", "SH600519", "600519.SH", true},
		{"0.000858", "SZ000858", "000858.SZ", true},
		{"0.300750", "SZ300750", "300750.SZ", true},
		{"0.920002", "BJ920002", "920002.BJ", true}, // 北交所
		{"105.AAPL", "", "", false},                 // 美股不支持
		{"1.000001", "", "", false},                 // 指数
		{"1.60", "", "", false},                     // 位数不对
		{"600519", "", "", false},                   // 缺市场前缀
	}
	for _, c := range cases {
		em, secu, ok := f10Codes(c.secid)
		if ok != c.ok || em != c.em || secu != c.secu {
			t.Errorf("f10Codes(%q) = (%q,%q,%v)，期望 (%q,%q,%v)", c.secid, em, secu, ok, c.em, c.secu, c.ok)
		}
	}
}

func TestParseF10Company(t *testing.T) {
	body := []byte(`{"jbzl":[{"SECUCODE":"600519.SH","ORG_NAME":"贵州茅台酒股份有限公司",
		"EM2016":"食品饮料-饮料-白酒","ORG_PROFILE":"  贵州茅台酒股份有限公司成立于1999年。 ",
		"BUSINESS_SCOPE":"茅台酒及系列酒的生产与销售"}]}`)
	c, err := parseF10Company(body)
	if err != nil {
		t.Fatalf("parseF10Company: %v", err)
	}
	if c.orgName != "贵州茅台酒股份有限公司" {
		t.Errorf("orgName = %q", c.orgName)
	}
	if c.industry != "食品饮料-饮料-白酒" {
		t.Errorf("industry = %q", c.industry)
	}
	if c.profile != "  贵州茅台酒股份有限公司成立于1999年。 " {
		t.Errorf("profile 应原样返回（TrimSpace 在 FetchF10 做）: %q", c.profile)
	}
	if _, err := parseF10Company([]byte(`{"jbzl":[]}`)); err == nil {
		t.Error("空 jbzl 应报错")
	}
}

func TestParseF10MainOps(t *testing.T) {
	body := []byte(`{"result":{"data":[
		{"MAINOP_TYPE":"2","ITEM_NAME":"茅台酒","MAIN_BUSINESS_INCOME":112925452300,"MBI_RATIO":0.862219,"GROSS_RPOFIT_RATIO":0.895,"REPORT_NAME":"2026三季报","REPORT_DATE":"2026-09-30 00:00:00"},
		{"MAINOP_TYPE":"3","ITEM_NAME":"国内","MAIN_BUSINESS_INCOME":129252586100,"MBI_RATIO":0.986881,"GROSS_RPOFIT_RATIO":null,"REPORT_NAME":"2026三季报","REPORT_DATE":"2026-09-30 00:00:00"},
		{"MAINOP_TYPE":"2","ITEM_NAME":"老产品","MAIN_BUSINESS_INCOME":1000,"MBI_RATIO":0.5,"GROSS_RPOFIT_RATIO":0.5,"REPORT_NAME":"2026中报","REPORT_DATE":"2026-06-30 00:00:00"}
	]}}`)
	period, ops, err := parseF10MainOps(body)
	if err != nil {
		t.Fatalf("parseF10MainOps: %v", err)
	}
	if period != "2026三季报" {
		t.Errorf("period = %q", period)
	}
	if len(ops) != 2 { // 只留最新期，中报那条被截断
		t.Fatalf("ops = %d 条，期望 2", len(ops))
	}
	if ops[0].Kind != "产品" || ops[0].Name != "茅台酒" || ops[0].Ratio < 86.2 || ops[0].Ratio > 86.3 {
		t.Errorf("ops[0] = %+v", ops[0])
	}
	if ops[0].Margin < 89.4 || ops[0].Margin > 89.6 {
		t.Errorf("毛利率应 ×100: %+v", ops[0])
	}
	if ops[1].Kind != "地区" || ops[1].Margin != 0 { // null 毛利率 → 0
		t.Errorf("ops[1] = %+v", ops[1])
	}
	_, _, err = parseF10MainOps([]byte(`{"result":{"data":[]}}`))
	if err != nil {
		t.Errorf("空数据应返回 nil 错误（新股无披露），实得 %v", err)
	}
}

func TestParseF10Finance(t *testing.T) {
	body := []byte(`{"result":{"data":[
		{"REPORT_DATE_NAME":"2026三季报","TOTALOPERATEREVE":133283652508.95,"TOTALOPERATEREVETZ":1.817,
		 "PARENTNETPROFIT":64467425981.95,"PARENTNETPROFITTZ":-0.2465,"KCFJCXSYJLR":64324824599.18,
		 "KCFJCXSYJLRTZ":-0.55,"XSMLL":89.58,"XSJLL":50.94,"ROEJQ":24.47,"EPSJB":51.53,
		 "BPS":216.95,"ZCFZL":11.90,"NETCASH_OPERATE_PK":84058979832.29,"TOTAL_ASSETS_PK":318304891882.72},
		{"REPORT_DATE_NAME":"2026中报","TOTALOPERATEREVE":91000000000,"TOTALOPERATEREVETZ":9.1,
		 "PARENTNETPROFIT":45000000000,"PARENTNETPROFITTZ":8.2,"KCFJCXSYJLR":44900000000,
		 "KCFJCXSYJLRTZ":8.0,"XSMLL":90.1,"XSJLL":49.5,"ROEJQ":16.2,"EPSJB":35.8}
	]}}`)
	fin, err := parseF10Finance(body)
	if err != nil {
		t.Fatalf("parseF10Finance: %v", err)
	}
	if len(fin) != 2 {
		t.Fatalf("fin = %d 期，期望 2", len(fin))
	}
	f := fin[0]
	if f.Period != "2026三季报" || f.Revenue != 133283652508.95 || f.RevenueYoy != 1.817 ||
		f.ProfitYoy != -0.2465 || f.Deduct != 64324824599.18 || f.DeductYoy != -0.55 ||
		f.Margin != 89.58 || f.NetMargin != 50.94 || f.RoE != 24.47 || f.EPS != 51.53 ||
		f.Bps != 216.95 || f.DebtRatio != 11.90 ||
		f.OperateCash != 84058979832.29 || f.TotalAssets != 318304891882.72 {
		t.Errorf("fin[0] = %+v", f)
	}
	if _, err := parseF10Finance([]byte(`{"result":{"data":[]}}`)); err == nil {
		t.Error("空财报数据应报错")
	}
}
