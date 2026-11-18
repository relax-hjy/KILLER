package web

import (
	"testing"

	"killer/internal/analysis"
)

// TestFp 价位文案口径：四舍五入到 3 位、第 3 位为 0 只显示 2 位——
// 低价 ETF 的 0.55 止损不能被压成"0.6"（与 0.60 的支撑撞车），整数价位保 2 位不掉精度。
func TestFp(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0.5499, "0.55"},
		{0.566, "0.566"},
		{0.641, "0.641"},
		{95, "95.00"},
		{110, "110.00"},
		{80.4359, "80.436"},
	}
	for _, c := range cases {
		if got := fp(c.v); got != c.want {
			t.Errorf("fp(%v) = %q, want %q", c.v, got, c.want)
		}
	}
}

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

// TestGuideHolding 持仓处置的优先级：破止损/回避 → 清仓；贴近压力且有浮盈 → 止盈一半；
// 市值超单票上限 → 减到上限（偏多也减）；偏多未满仓且有预算 → 加仓；其余持有。
func TestGuideHolding(t *testing.T) {
	const lot = 100
	mk := func(advice string, stop float64) *analysis.Assessment {
		return &analysis.Assessment{Score: 50, Advice: advice, Support: 90, Resistance: 110, StopLoss: stop}
	}
	cases := []struct {
		name              string
		a                 *analysis.Assessment
		qty               int64
		price, avgCost    float64
		mv, total         float64
		singleCap, budget float64
		wantAction        string
		wantQty           int64
	}{
		{"破止损清仓", mk("可以买入", 95), 200, 94, 94, 18800, 36000, 10800, 12000, actClear, 200},
		{"回避清仓", mk("回避", 80), 200, 100, 100, 20000, 36000, 10800, 12000, actClear, 200},
		{"贴近压力止盈一半", mk("可以买入", 80), 200, 108, 90, 21600, 36000, 10800, 12000, actTake, 100},
		{"止盈不足一手不减", mk("可以买入", 80), 100, 108, 90, 10800, 36000, 10800, 12000, actHold, 0},
		{"贴近压力但没浮盈不喊止盈", &analysis.Assessment{Score: 50, Advice: "观望，回踩支撑再介入", Support: 90, Resistance: 100, StopLoss: 80}, 100, 99, 105, 9900, 36000, 10800, 12000, actHold, 0},
		{"超仓减一手", mk("偏弱，暂缓买入", 80), 200, 80, 80, 16000, 36000, 8000, 12000, actCut, 100},
		{"超仓不足一手也减", mk("偏弱，暂缓买入", 80), 200, 80, 80, 16000, 36000, 10800, 12000, actCut, 100},
		{"偏多超仓同样减", mk("可以买入", 80), 200, 80, 80, 16000, 36000, 10800, 12000, actCut, 100},
		{"偏弱轻仓持有", mk("偏弱，暂缓买入", 80), 100, 80, 80, 8000, 36000, 10800, 12000, actHold, 0},
		{"偏多加仓四手", mk("可以买入", 18), 100, 20, 20, 2000, 36000, 10800, 12000, actAdd, 400},
		{"偏多预算不足一手", mk("可以买入", 80), 100, 80, 80, 8000, 36000, 10800, 2000, actHold, 0},
		{"正好满仓持有", mk("可以买入", 80), 135, 80, 80, 10800, 36000, 10800, 12000, actHold, 0},
	}
	for _, c := range cases {
		action, qty, _, _ := guideHolding(c.a, c.qty, c.price, c.avgCost, c.mv, c.total, c.singleCap, c.budget, lot, 0.00025, 5)
		if action != c.wantAction || qty != c.wantQty {
			t.Errorf("%s: action=%s qty=%d, want %s %d", c.name, action, qty, c.wantAction, c.wantQty)
		}
	}
}

// TestHalfLot 止盈减半按整手向下取整，不足一手不减。
func TestHalfLot(t *testing.T) {
	cases := []struct {
		qty  int64
		want int64
	}{
		{200, 100}, {300, 100}, {400, 200}, {100, 0}, {199, 0},
	}
	for _, c := range cases {
		if got := halfLot(c.qty, 100); got != c.want {
			t.Errorf("halfLot(%d) = %d, want %d", c.qty, got, c.want)
		}
	}
}

// TestPlanHolding 应对预案：止损线恒在且锚定买入价；已破位时第一条就是清仓；
// 压力位止盈线只在持仓够一手半时给；加仓/回踩分支互斥；环境弱势永远是最后一条。
func TestPlanHolding(t *testing.T) {
	const lot = 100
	base := func() *analysis.Assessment {
		return &analysis.Assessment{Score: 66, Advice: "可以买入", Support: 95, Resistance: 110, Atr: 2}
	}
	// 止损 = max(支撑-ATR=93, 买入价-2.5ATR=95) → 95（Atr=2、买入价 100）
	steps := planHolding(base(), 200, 100, 100, actHold, 0, lot)
	if len(steps) < 3 {
		t.Fatalf("正常持仓应有止损/止盈/支撑/环境至少 4 条，实际 %d: %+v", len(steps), steps)
	}
	if steps[0].If != "跌破止损位 95.00（较买入价约 -5.0%）" {
		t.Errorf("第一条应是止损线，实际 %+v", steps[0])
	}
	if steps[0].Then != "清仓止损，不补仓不摊薄" {
		t.Errorf("止损动作应是清仓，实际 %+v", steps[0])
	}
	if got := steps[1].If; got != "涨到压力位 110.00" {
		t.Errorf("第二条应是压力位止盈，实际 %q", got)
	}
	if steps[len(steps)-1].Then == "" {
		t.Error("最后一条应是环境弱势预案")
	}
	last := steps[len(steps)-1]
	if last.If != "大盘环境转弱势（指数跌破 MA20、板块普跌）" {
		t.Errorf("环境非弱势时应给未来条件句，实际 %q", last.If)
	}

	// 已破止损：第一条变成"已跌破"，动作是立即清仓
	broken := planHolding(base(), 200, 100, 90, actClear, 0, lot)
	if broken[0].If == "" || broken[0].Then != "立即清仓离场，按纪律执行，不等回本" {
		t.Errorf("破位应立即清仓，实际 %+v", broken[0])
	}

	// 加仓动作 → 支撑线变成加仓条件
	add := planHolding(base(), 200, 100, 100, actAdd, 100, lot)
	found := false
	for _, s := range add {
		if s.If == "回踩支撑位 95.00 附近企稳（不破）" && s.Then == "可加仓 100 股，加仓部分同样守止损" {
			found = true
		}
	}
	if !found {
		t.Errorf("加仓预案缺失: %+v", add)
	}

	// 持仓不足一手半 → 不给止盈线（halfLot=0）
	small := planHolding(base(), 100, 100, 100, actHold, 0, lot)
	for _, s := range small {
		if s.If == "涨到压力位 110.00" {
			t.Errorf("一手持仓不应给止盈一半预案: %+v", small)
		}
	}

	// 研判回避 → 有离场预案；已在弱势环境 → 条件句变成"维持弱势"
	weak := base()
	weak.Advice = "回避"
	weak.Regime = "弱势"
	ws := planHolding(weak, 200, 100, 100, actClear, 0, lot)
	hasQuit, hasWeak := false, false
	for _, s := range ws {
		if s.If == "研判维持「回避」" {
			hasQuit = true
		}
		if s.If == "大盘环境维持弱势" {
			hasWeak = true
		}
	}
	if !hasQuit || !hasWeak {
		t.Errorf("回避/弱势预案缺失 quit=%v weak=%v: %+v", hasQuit, hasWeak, ws)
	}
}
