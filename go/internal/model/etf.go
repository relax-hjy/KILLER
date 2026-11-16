package model

// Holding ETF/基金持仓明细行（东财天天基金 F10 jjcc，最新披露期前十大重仓）。
type Holding struct {
	Rank   int     `json:"rank"`   // 序号
	Code   string  `json:"code"`   // 股票代码（6 位）
	Name   string  `json:"name"`   // 股票名称
	Pct    float64 `json:"pct"`    // 占净值比（%）
	Shares float64 `json:"shares"` // 持股数（万股）
	Value  float64 `json:"value"`  // 持仓市值（万元）
}
