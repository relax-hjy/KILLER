package trading

import (
	"context"
	"testing"

	"killer/internal/config"
	"killer/internal/model"
)

// newTestEngine 临时库上的引擎，期初现金 1 万。
func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	st, err := Open(t.TempDir() + "/trade.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Default()
	cfg.Trading.InitialCash = 10000
	cfg.Trading.CommissionRate = 0.00025
	cfg.Trading.MinCommission = 5
	return New(cfg, st)
}

func acct(t *testing.T, e *Engine) *model.Account {
	t.Helper()
	a, err := e.Account(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestBuyOrderFreeze 买入挂单冻结委托资金，成交按实际成本多退少补。
// 100股@10 委托（金额 1000 + 佣金 5 → 冻结 1005），以 9.5 成交（950 + 5 = 955）应退回 50。
func TestBuyOrderFreeze(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()

	id, status, err := e.PlaceOrder(ctx, model.Order{
		SecID: "0.000001", Code: "000001", Name: "测试股",
		Side: model.SideBuy, OrderType: model.OrderTypeLimit, Price: 10, Quantity: 100,
	})
	if err != nil || status != "pending" {
		t.Fatalf("挂单失败: %v %s", err, status)
	}
	a := acct(t, e)
	if a.Cash != 8995 || a.FrozenCash != 1005 {
		t.Fatalf("挂单后 cash=%.2f frozen=%.2f, want 8995/1005", a.Cash, a.FrozenCash)
	}

	if _, status, err := e.fillOrder(ctx, id, 9.5); err != nil || status != "filled" {
		t.Fatalf("成交失败: %v %s", err, status)
	}
	a = acct(t, e)
	if a.Cash != 9045 || a.FrozenCash != 0 {
		t.Fatalf("成交后 cash=%.2f frozen=%.2f, want 9045/0（差额 50 退回现金）", a.Cash, a.FrozenCash)
	}
	p, _ := e.store.GetPosition("0.000001")
	if p == nil || p.Quantity != 100 || p.AvgCost != 9.5 {
		t.Fatalf("持仓=%+v, want 100股@9.5", p)
	}
	// 总资产=现金+冻结+持仓市值，市值取实时行情（网络），这里只校验现金口径不再重复扣款
}

// TestBuyOrderCancelRelease 撤单把冻结资金放回现金。
func TestBuyOrderCancelRelease(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	id, _, err := e.PlaceOrder(ctx, model.Order{
		SecID: "1.600519", Code: "600519", Name: "测试股",
		Side: model.SideBuy, OrderType: model.OrderTypeLimit, Price: 50, Quantity: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.CancelOrder(id); err != nil {
		t.Fatal(err)
	}
	a := acct(t, e)
	if a.Cash != 10000 || a.FrozenCash != 0 {
		t.Fatalf("撤单后 cash=%.2f frozen=%.2f, want 10000/0", a.Cash, a.FrozenCash)
	}
}

// TestBuyOrderInsufficient 资金不足应拒绝且不动现金。
func TestBuyOrderInsufficient(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	_, status, err := e.PlaceOrder(ctx, model.Order{
		SecID: "1.600519", Code: "600519", Name: "测试股",
		Side: model.SideBuy, OrderType: model.OrderTypeLimit, Price: 10, Quantity: 1000,
	})
	if err == nil || status != "rejected" {
		t.Fatalf("应拒绝: %v %s", err, status)
	}
	a := acct(t, e)
	if a.Cash != 10000 || a.FrozenCash != 0 {
		t.Fatalf("拒绝后 cash=%.2f frozen=%.2f, want 10000/0", a.Cash, a.FrozenCash)
	}
}

// TestSellNoFreeze 卖出挂单不冻结现金（成交时才校验持仓）。
func TestSellNoFreeze(t *testing.T) {
	e := newTestEngine(t)
	ctx := context.Background()
	if _, status, err := e.PlaceOrder(ctx, model.Order{
		SecID: "0.000001", Code: "000001", Name: "测试股",
		Side: model.SideBuy, OrderType: model.OrderTypeLimit, Price: 10, Quantity: 100,
	}); err != nil || status != "pending" {
		t.Fatalf("买入挂单失败: %v %s", err, status)
	}
	if _, status, err := e.fillOrder(ctx, 1, 10); err != nil || status != "filled" {
		t.Fatalf("买入成交失败: %v %s", err, status)
	}
	before := acct(t, e).Cash
	id, status, err := e.PlaceOrder(ctx, model.Order{
		SecID: "0.000001", Code: "000001", Name: "测试股",
		Side: model.SideSell, OrderType: model.OrderTypeLimit, Price: 11, Quantity: 100,
	})
	if err != nil || status != "pending" {
		t.Fatalf("卖出挂单失败: %v %s", err, status)
	}
	a := acct(t, e)
	if a.Cash != before || a.FrozenCash != 0 {
		t.Fatalf("卖出挂单不应冻结: cash=%.2f frozen=%.2f", a.Cash, a.FrozenCash)
	}
	// 更高价成交：佣金 1100×万2.5 不足 5 元按 5 元收，另加印花税 0.55
	if _, status, err := e.fillOrder(ctx, id, 11); err != nil || status != "filled" {
		t.Fatalf("卖出成交失败: %v %s", err, status)
	}
	a = acct(t, e)
	want := before + 1100 - (5 + 1100*0.0005)
	if a.Cash < want-0.01 || a.Cash > want+0.01 {
		t.Fatalf("卖出后 cash=%.2f, want %.2f", a.Cash, want)
	}
}
