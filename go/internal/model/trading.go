package model

import "time"

// 交易方向。
const (
	SideBuy  = "buy"  // 买入
	SideSell = "sell" // 卖出
)

// 订单类型。
const (
	OrderTypeMarket = "market" // 市价单
	OrderTypeLimit  = "limit"  // 限价单
)

// 订单状态。
const (
	OrderStatusPending   = "pending"    // 挂单中（限价单未成交）
	OrderStatusFilled    = "filled"     // 已全部成交
	OrderStatusPartially = "partially"  // 部分成交
	OrderStatusCanceled  = "canceled"   // 已撤销
	OrderStatusRejected  = "rejected"   // 已拒绝（资金/持仓不足等）
)

// Account 模拟账户（单账户，含现金 + 持仓市值）。
type Account struct {
	InitialCash   float64   `json:"initial_cash"`   // 初始资金
	Cash          float64   `json:"cash"`           // 可用现金
	FrozenCash    float64   `json:"frozen_cash"`    // 冻结资金（限价买单占用）
	MarketValue   float64   `json:"market_value"`   // 持仓市值
	TotalAsset    float64   `json:"total_asset"`    // 总资产 = 现金 + 冻结 + 市值
	TotalProfit   float64   `json:"total_profit"`   // 累计盈亏
	TotalProfitPct float64  `json:"total_profit_pct"` // 累计收益率（%）
	UpdatedAt     time.Time `json:"updated_at"`
}

// Position 持仓（按 secid 聚合）。
type Position struct {
	SecID        string    `json:"secid"`
	Code         string    `json:"code"`
	Name         string    `json:"name"`
	Quantity     int64     `json:"quantity"`      // 持仓数量（股，指数/板块为份额）
	Available    int64     `json:"available"`     // 可用数量（T+1 制度下今买不可卖，暂简化为全可用）
	AvgCost      float64   `json:"avg_cost"`      // 平均成本价
	LastPrice    float64   `json:"last_price"`    // 最新价（实时）
	MarketValue  float64   `json:"market_value"`  // 市值
	Profit       float64   `json:"profit"`        // 浮动盈亏
	ProfitPct    float64   `json:"profit_pct"`    // 浮动盈亏率（%）
	UpdatedAt    time.Time `json:"updated_at"`
}

// Order 交易订单。
type Order struct {
	ID          int64     `json:"id"`
	SecID       string    `json:"secid"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Side        string    `json:"side"`         // buy | sell
	OrderType   string    `json:"order_type"`   // market | limit
	Price       float64   `json:"price"`        // 委托价（市价单为下单时参考价）
	Quantity    int64     `json:"quantity"`     // 委托数量
	FilledQty   int64     `json:"filled_qty"`   // 已成交数量
	FilledPrice float64   `json:"filled_price"` // 成交均价
	Status      string    `json:"status"`       // pending/filled/partially/canceled/rejected
	Fee         float64   `json:"fee"`          // 手续费
	Reason      string    `json:"reason"`       // 拒绝/撤销原因
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Trade 成交记录。
type Trade struct {
	ID        int64     `json:"id"`
	OrderID   int64     `json:"order_id"`
	SecID     string    `json:"secid"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Side      string    `json:"side"`
	Price     float64   `json:"price"`
	Quantity  int64     `json:"quantity"`
	Amount    float64   `json:"amount"` // 成交金额 = 价 × 量
	Fee       float64   `json:"fee"`
	TradedAt  time.Time `json:"traded_at"`
}

// Signal 策略信号。
type Signal struct {
	SecID      string    `json:"secid"`
	Code       string    `json:"code"`
	Name       string    `json:"name"`
	Strategy   string    `json:"strategy"`  // 策略名
	Action     string    `json:"action"`    // buy | sell | hold
	Price      float64   `json:"price"`     // 信号触发价
	Strength   float64   `json:"strength"`  // 信号强度（0-1）
	Reason     string    `json:"reason"`    // 触发原因描述
	GeneratedAt time.Time `json:"generated_at"`
}

// BacktestResult 回测结果汇总。
type BacktestResult struct {
	Strategy      string    `json:"strategy"`
	SecID         string    `json:"secid"`
	Name          string    `json:"name"`
	InitialCash   float64   `json:"initial_cash"`
	FinalAsset    float64   `json:"final_asset"`
	TotalReturn   float64   `json:"total_return"`   // 总收益率（%）
	AnnualReturn  float64   `json:"annual_return"`  // 年化收益率（%）
	MaxDrawdown   float64   `json:"max_drawdown"`   // 最大回撤（%）
	WinRate       float64   `json:"win_rate"`       // 胜率（%）
	Sharpe        float64   `json:"sharpe"`         // 夏普比率
	TradeCount    int       `json:"trade_count"`    // 交易次数
	EquityCurve   []EquityPoint `json:"equity_curve"` // 权益曲线
	Trades        []Trade    `json:"trades"`         // 交易明细
	StartedAt     time.Time  `json:"started_at"`
	EndedAt       time.Time  `json:"ended_at"`
}

// EquityPoint 权益曲线点。
type EquityPoint struct {
	Date  string  `json:"date"`
	Asset float64 `json:"asset"`
}
