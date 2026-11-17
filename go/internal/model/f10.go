package model

// F10 个股公司基本面（东财 F10：公司概况 + 主营构成 + 财报指标）。
type F10 struct {
	OrgName       string       `json:"org_name"`       // 公司全称
	Industry      string       `json:"industry"`       // 所属行业（东财 EM2016 分类，如 食品饮料-饮料-白酒）
	Profile       string       `json:"profile"`        // 公司简介
	BusinessScope string       `json:"business_scope"` // 经营范围
	MainOpPeriod  string       `json:"main_op_period"` // 主营构成报告期（如 2026三季报）
	MainOps       []F10MainOp  `json:"main_ops"`       // 主营构成（最新报告期，产品/地区）
	Finance       []F10Finance `json:"finance"`        // 最近 4 期财报主要指标（新→旧）
}

// F10MainOp 主营构成条目（按行业/产品/地区拆分的收入结构）。
type F10MainOp struct {
	Kind   string  `json:"kind"`   // 分类：行业/产品/地区
	Name   string  `json:"name"`   // 项目名称
	Income float64 `json:"income"` // 主营收入（元）
	Ratio  float64 `json:"ratio"`  // 收入占比（%）
	Margin float64 `json:"margin"` // 毛利率（%），报告期未披露为 0
}

// F10Finance 单报告期主要财报指标。
type F10Finance struct {
	Period      string  `json:"period"`       // 报告期名（如 2026三季报）
	Revenue     float64 `json:"revenue"`      // 营业总收入（元，年初至今累计）
	RevenueYoy  float64 `json:"revenue_yoy"`  // 营收同比（%）
	Profit      float64 `json:"profit"`       // 归母净利润（元）
	ProfitYoy   float64 `json:"profit_yoy"`   // 归母净利同比（%）
	Deduct      float64 `json:"deduct"`       // 扣非净利润（元）
	DeductYoy   float64 `json:"deduct_yoy"`   // 扣非净利同比（%）
	Margin      float64 `json:"margin"`       // 销售毛利率（%）
	NetMargin   float64 `json:"net_margin"`   // 销售净利率（%）
	RoE         float64 `json:"roe"`          // 加权 ROE（%）
	EPS         float64 `json:"eps"`          // 基本每股收益（元）
	Bps         float64 `json:"bps"`          // 每股净资产（元）
	DebtRatio   float64 `json:"debt_ratio"`   // 资产负债率（%）
	OperateCash float64 `json:"operate_cash"` // 经营活动现金流净额（元）
	TotalAssets float64 `json:"total_assets"` // 总资产（元）
}
