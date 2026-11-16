package web

import (
	"testing"

	"killer/internal/analysis"
)

// TestBuyQty 买入数量按整手折算，占用资金含佣金、不超预算；高低价股都要能买得起。
func TestBuyQty(t *testing.T) {
	const lot = 100
	cases := []struct {
		name               string
		budget, price      float64
		commission, minFee float64
		want               int64
	}{
		{"2万买20元股", 20000, 20, 0.00025, 5, 900},  // 每手 2000+5=2005 → 9 手 18045，第 10 手超预算
		{"预算刚好一手", 2005, 20, 0.00025, 5, 100},
		{"预算差1元", 2004, 20, 0.00025, 5, 0},
		{"买不起一手", 15000, 1453.9, 0.00025, 5, 0}, // 茅台一手 14.5 万
		{"零预算", 0, 20, 0.00025, 5, 0},
	}
	for _, c := range cases {
		if got := buyQty(c.budget, c.price, c.commission, c.minFee, lot); got != c.want {
			t.Errorf("%s: buyQty = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestCutQty 减仓到目标市值，按整手向上取整（卖少了压不回上限）；未超仓不减、不超过实际持仓。
func TestCutQty(t *testing.T) {
	const lot = 100
	cases := []struct {
		name      string
		qty       int64
		price, mv float64
		target    float64
		want      int64
	}{
		{"超75股也减一手", 200, 80, 16000, 10000, 100},
		{"正好一手", 200, 80, 16000, 8000, 100},
		{"未超仓", 200, 80, 16000, 16000, 0},
		{"清仓级", 100, 80, 8000, 0, 100},
		{"超1股也减一手", 100, 80, 8100, 8000, 100},
	}
	for _, c := range cases {
		if got := cutQty(c.qty, c.price, c.mv, c.target, lot); got != c.want {
			t.Errorf("%s: cutQty = %d, want %d", c.name, got, c.want)
		}
	}
}

// TestBuyPrice 建议限价挂支撑位附近，不追高；现价已低于支撑则按现价。
func TestBuyPrice(t *testing.T) {
	cases := []struct {
		price, support float64
		want           float64
	}{
		{100, 90, 91.8},   // 支撑上浮 2%
		{91, 90, 91.0},    // 现价已在支撑附近，不挂更低
		{88, 90, 88},      // 现价低于支撑，按现价
		{100, 0, 100},     // 无支撑位则按现价
	}
	for _, c := range cases {
		if got := buyPrice(c.price, c.support); got != c.want {
			t.Errorf("buyPrice(%v, %v) = %v, want %v", c.price, c.support, got, c.want)
		}
	}
}

// TestGuideHolding 持仓处置的优先级：破止损/回避 → 清仓；市值超单票上限 → 减到上限（偏多也减）；
// 偏多未满仓且有预算 → 加仓；偏弱或预算不足 → 持有。
func TestGuideHolding(t *testing.T) {
	const lot = 100
	mk := func(advice string, stop float64) *analysis.Assessment {
		return &analysis.Assessment{Score: 50, Advice: advice, Support: 90, Resistance: 110, StopLoss: stop}
	}
	cases := []struct {
		name              string
		a                 *analysis.Assessment
		qty               int64
		price, mv, total  float64
		singleCap, budget float64
		wantAction        string
		wantQty           int64
	}{
		{"破止损清仓", mk("可以买入", 95), 200, 94, 18800, 36000, 10800, 12000, actClear, 200},
		{"回避清仓", mk("回避", 80), 200, 100, 20000, 36000, 10800, 12000, actClear, 200},
		{"超仓减一手", mk("偏弱，暂缓买入", 80), 200, 80, 16000, 36000, 8000, 12000, actCut, 100},
		{"超仓不足一手也减", mk("偏弱，暂缓买入", 80), 200, 80, 16000, 36000, 10800, 12000, actCut, 100},
		{"偏多超仓同样减", mk("可以买入", 80), 200, 80, 16000, 36000, 10800, 12000, actCut, 100},
		{"偏弱轻仓持有", mk("偏弱，暂缓买入", 80), 100, 80, 8000, 36000, 10800, 12000, actHold, 0},
		{"偏多加仓四手", mk("可以买入", 18), 100, 20, 2000, 36000, 10800, 12000, actAdd, 400},
		{"偏多预算不足一手", mk("可以买入", 80), 100, 80, 8000, 36000, 10800, 2000, actHold, 0},
		{"正好满仓持有", mk("可以买入", 80), 135, 80, 10800, 36000, 10800, 12000, actHold, 0},
	}
	for _, c := range cases {
		action, qty, _, _ := guideHolding(c.a, c.qty, c.price, c.mv, c.total, c.singleCap, c.budget, lot, 0.00025, 5)
		if action != c.wantAction || qty != c.wantQty {
			t.Errorf("%s: action=%s qty=%d, want %s %d", c.name, action, qty, c.wantAction, c.wantQty)
		}
	}
}
