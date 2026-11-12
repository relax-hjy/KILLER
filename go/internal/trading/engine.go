package trading

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"

	"killer/internal/config"
	"killer/internal/model"
	"killer/internal/source"
)

// Engine 模拟交易引擎。负责下单、撮合、费用计算、持仓盈亏。
//
// 撮合规则（简化）：
//   - 市价单：用当前实时行情价立即成交。
//   - 限价单：挂单，等行情满足价格条件时成交（由 MatchPending 驱动）。
//   - 买入冻结现金，卖出冻结持仓（简化：卖出不冻结，成交时校验）。
type Engine struct {
	cfg   *config.Config
	store *Store

	mu sync.Mutex
}

// New 创建交易引擎。
func New(cfg *config.Config, st *Store) *Engine {
	return &Engine{cfg: cfg, store: st}
}

// fee 计算一笔成交的手续费（佣金 + 印花税）。返回佣金、印花税、合计。
func (e *Engine) fee(side string, amount float64) (commission, stampTax, total float64) {
	commission = amount * e.cfg.Trading.CommissionRate
	if commission < e.cfg.Trading.MinCommission {
		commission = e.cfg.Trading.MinCommission
	}
	if side == model.SideSell {
		stampTax = amount * e.cfg.Trading.StampTaxRate
	}
	total = commission + stampTax
	return
}

// PlaceOrder 下单。返回订单 ID 与结果。
// 市价单立即撮合；限价单挂单（pending），由 MatchPending 在行情轮询时撮合。
func (e *Engine) PlaceOrder(ctx context.Context, req model.Order) (int64, string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if req.Quantity <= 0 {
		return 0, "", fmt.Errorf("数量必须为正")
	}
	if req.Side != model.SideBuy && req.Side != model.SideSell {
		return 0, "", fmt.Errorf("非法方向: %s", req.Side)
	}
	if req.OrderType == "" {
		req.OrderType = model.OrderTypeMarket
	}
	if req.OrderType != model.OrderTypeMarket && req.OrderType != model.OrderTypeLimit {
		return 0, "", fmt.Errorf("非法订单类型: %s", req.OrderType)
	}

	// 市价单：获取实时价
	if req.OrderType == model.OrderTypeMarket || req.Price <= 0 {
		px, err := e.lastPrice(ctx, req.SecID)
		if err != nil {
			return 0, "", fmt.Errorf("获取实时价失败: %w", err)
		}
		req.Price = px
	}

	req.Status = model.OrderStatusPending
	req.FilledQty = 0
	req.FilledPrice = 0
	id, err := e.store.CreateOrder(&req)
	if err != nil {
		return 0, "", err
	}

	// 限价单：挂单，返回 pending
	if req.OrderType == model.OrderTypeLimit {
		// 挂单前校验可用资金/持仓（但不冻结成交）
		if req.Side == model.SideBuy {
			acc, _ := e.store.GetAccount(e.cfg.Trading.InitialCash)
			need := req.Price * float64(req.Quantity)
			_, _, feeTotal := e.fee(model.SideBuy, need)
			if acc.Cash < need+feeTotal {
				e.store.UpdateOrder(id, 0, 0, model.OrderStatusRejected, 0, "可用资金不足")
				return id, "rejected", fmt.Errorf("可用资金不足：需 %.2f，可用 %.2f", need+feeTotal, acc.Cash)
			}
		} else {
			p, _ := e.store.GetPosition(req.SecID)
			if p == nil || p.Available < req.Quantity {
				e.store.UpdateOrder(id, 0, 0, model.OrderStatusRejected, 0, "持仓不足")
				return id, "rejected", fmt.Errorf("持仓不足")
			}
		}
		return id, "pending", nil
	}

	// 市价单：立即撮合
	return e.fillOrder(ctx, id, req.Price)
}

// fillOrder 撮合一笔订单（立即成交）。需持有 e.mu。
func (e *Engine) fillOrder(ctx context.Context, orderID int64, fillPrice float64) (int64, string, error) {
	o, err := e.store.GetOrder(orderID)
	if err != nil {
		return orderID, "", err
	}
	if o.Status == model.OrderStatusFilled || o.Status == model.OrderStatusCanceled {
		return orderID, o.Status, nil
	}

	qty := o.Quantity
	amount := fillPrice * float64(qty)
	commission, stampTax, feeTotal := e.fee(o.Side, amount)

	if o.Side == model.SideBuy {
		acc, _ := e.store.GetAccount(e.cfg.Trading.InitialCash)
		totalCost := amount + feeTotal
		if acc.Cash < totalCost {
			e.store.UpdateOrder(orderID, 0, 0, model.OrderStatusRejected, 0, "可用资金不足")
			return orderID, "rejected", fmt.Errorf("可用资金不足：需 %.2f，可用 %.2f", totalCost, acc.Cash)
		}
		// 扣现金
		if _, err := e.store.updateCash(-totalCost, 0); err != nil {
			return orderID, "", err
		}
		// 加持仓
		if err := e.store.applyPosition(o.SecID, o.Code, o.Name, qty, fillPrice); err != nil {
			return orderID, "", err
		}
	} else { // sell
		p, _ := e.store.GetPosition(o.SecID)
		if p == nil || p.Available < qty {
			e.store.UpdateOrder(orderID, 0, 0, model.OrderStatusRejected, 0, "持仓不足")
			return orderID, "rejected", fmt.Errorf("持仓不足")
		}
		// 减持仓
		if err := e.store.applyPosition(o.SecID, o.Code, o.Name, -qty, fillPrice); err != nil {
			return orderID, "", err
		}
		// 加现金（扣除手续费）
		netCash := amount - feeTotal
		if _, err := e.store.updateCash(netCash, 0); err != nil {
			return orderID, "", err
		}
	}

	// 更新订单为已成交
	if err := e.store.UpdateOrder(orderID, qty, fillPrice, model.OrderStatusFilled, feeTotal, ""); err != nil {
		return orderID, "", err
	}
	// 记录成交
	t := &model.Trade{
		OrderID: orderID, SecID: o.SecID, Code: o.Code, Name: o.Name,
		Side: o.Side, Price: fillPrice, Quantity: qty, Amount: amount, Fee: feeTotal,
	}
	if _, err := e.store.RecordTrade(t); err != nil {
		return orderID, "", err
	}
	_ = commission
	_ = stampTax
	log.Printf("[Trading] %s %s %d股 @ %.2f 手续费 %.2f", o.Side, o.Name, qty, fillPrice, feeTotal)
	return orderID, "filled", nil
}

// CancelOrder 撤销挂单。
func (e *Engine) CancelOrder(orderID int64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	o, err := e.store.GetOrder(orderID)
	if err != nil {
		return err
	}
	if o.Status != model.OrderStatusPending {
		return fmt.Errorf("订单非挂单状态，无法撤销（当前 %s）", o.Status)
	}
	return e.store.UpdateOrder(orderID, 0, 0, model.OrderStatusCanceled, 0, "用户撤销")
}

// MatchPending 轮询撮合挂单（限价单）。由外部定时调用或行情更新触发。
// 买入：最新价 <= 委托价 成交；卖出：最新价 >= 委托价 成交。
func (e *Engine) MatchPending(ctx context.Context) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	pending, err := e.store.ListPendingOrders()
	if err != nil || len(pending) == 0 {
		return 0
	}

	filled := 0
	for _, o := range pending {
		px, err := e.lastPrice(ctx, o.SecID)
		if err != nil {
			continue
		}
		match := false
		if o.Side == model.SideBuy && px <= o.Price {
			match = true
		} else if o.Side == model.SideSell && px >= o.Price {
			match = true
		}
		if !match {
			continue
		}
		// 用委托价成交（限价单更优价成交，简化按委托价）
		fillPrice := o.Price
		if o.Side == model.SideBuy && px < o.Price {
			fillPrice = px // 买入更低价成交
		} else if o.Side == model.SideSell && px > o.Price {
			fillPrice = px // 卖出更高价成交
		}
		if _, status, err := e.fillOrder(ctx, o.ID, fillPrice); err == nil && status == "filled" {
			filled++
		}
	}
	return filled
}

// lastPrice 获取标的最新价（指数/板块/个股通用）。
func (e *Engine) lastPrice(ctx context.Context, secid string) (float64, error) {
	// 板块 secid 走腾讯批量行情
	if source.IsBoardSecid(secid) {
		quotes, err := source.FetchBoardQuotes(ctx, []string{secid})
		if err != nil {
			return 0, err
		}
		if len(quotes) == 0 || quotes[0].Price <= 0 {
			return 0, fmt.Errorf("无行情: %s", secid)
		}
		return quotes[0].Price, nil
	}
	// 指数/个股走批量行情（东财/腾讯/新浪路由）
	stocks, err := source.FetchUSQuotes(ctx, []string{secid})
	if err != nil {
		return 0, err
	}
	for _, s := range stocks {
		if s.SecID == secid && s.Price > 0 {
			return s.Price, nil
		}
	}
	return 0, fmt.Errorf("无行情: %s", secid)
}

// Account 返回账户概览（含持仓市值、总资产、盈亏）。
func (e *Engine) Account(ctx context.Context) (*model.Account, error) {
	acc, err := e.store.GetAccount(e.cfg.Trading.InitialCash)
	if err != nil {
		return nil, err
	}
	positions, err := e.store.ListPositions()
	if err != nil {
		return nil, err
	}
	// 汇总持仓市值
	mv := 0.0
	cost := 0.0
	for _, p := range positions {
		px, err := e.lastPrice(ctx, p.SecID)
		if err != nil {
			px = p.AvgCost // 无行情时按成本价兜底
		}
		mv += px * float64(p.Quantity)
		cost += p.AvgCost * float64(p.Quantity)
	}
	acc.MarketValue = mv
	acc.TotalAsset = acc.Cash + acc.FrozenCash + mv
	acc.TotalProfit = acc.TotalAsset - acc.InitialCash
	if acc.InitialCash > 0 {
		acc.TotalProfitPct = acc.TotalProfit / acc.InitialCash * 100
	}
	return acc, nil
}

// Positions 返回持仓列表（填最新价与盈亏）。
func (e *Engine) Positions(ctx context.Context) ([]model.Position, error) {
	positions, err := e.store.ListPositions()
	if err != nil {
		return nil, err
	}
	for i := range positions {
		p := &positions[i]
		px, err := e.lastPrice(ctx, p.SecID)
		if err != nil {
			px = p.AvgCost
		}
		p.LastPrice = px
		p.MarketValue = px * float64(p.Quantity)
		p.Profit = (px - p.AvgCost) * float64(p.Quantity)
		if p.AvgCost > 0 {
			p.ProfitPct = (px - p.AvgCost) / p.AvgCost * 100
		}
	}
	return positions, nil
}

// Orders 最近订单。
func (e *Engine) Orders(limit int) ([]model.Order, error) {
	return e.store.ListOrders(limit)
}

// PendingOrders 挂单列表。
func (e *Engine) PendingOrders() ([]model.Order, error) {
	return e.store.ListPendingOrders()
}

// Trades 最近成交。
func (e *Engine) Trades(limit int) ([]model.Trade, error) {
	return e.store.ListTrades(limit)
}

// round2 四舍五入到 2 位小数。
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
