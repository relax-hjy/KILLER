package web

// 机会扫描 API：
//   GET /api/trade/opportunities?group=&n=8  全市场机会排序
//
// 与交易指导的候选买入互补：指导只按分析评分挑「可以买入/观望」；
// 这里再叠加 ①全部已注册策略的信号共振 ②大盘环境分，给出综合机会分排序，
// 每条附带入手价、止损、目标位、入手后操作计划与风险提示。

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"killer/internal/analysis"
	"killer/internal/monitor"
	"killer/internal/trading"
)

// oppSignal 单个策略对某标的的信号（机会明细里逐条展示）。
type oppSignal struct {
	Strategy string  `json:"strategy"`
	Label    string  `json:"label"`
	Action   string  `json:"action"` // buy | sell | hold
	Reason   string  `json:"reason"`
	Strength float64 `json:"strength"`
}

// opportunity 一条可入手机会。
type opportunity struct {
	SecID       string      `json:"secid"`
	Code        string      `json:"code"`
	Name        string      `json:"name"`
	Price       float64     `json:"price"`
	Pct         float64     `json:"pct"`
	Score       float64     `json:"score"`
	Advice      string      `json:"advice"`
	Signals     []oppSignal `json:"signals"`
	BuySignals  int         `json:"buy_signals"`
	SellSignals int         `json:"sell_signals"`
	OppScore    float64     `json:"opp_score"` // 综合机会分（0-100，排序用）
	Entry       float64     `json:"entry"`     // 建议入手价（支撑位上浮 2%，不追高）
	Stop        float64     `json:"stop"`      // 止损位（锚定入手价）
	Target      float64     `json:"target"`    // 目标位（压力位）
	Qty         int64       `json:"qty"`       // 按单票上限与预算折算的整手数量
	Amount      float64     `json:"amount"`
	Plan        []planStep  `json:"plan"`   // 入手后操作计划（如果…就…）
	Risks       []string    `json:"risks"`  // 风险提示
	Reason      string      `json:"reason"` // 一句话推荐理由
}

// handleTradeOpportunities GET /api/trade/opportunities?group=&n=8
func (s *Server) handleTradeOpportunities(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 10 {
		n = 8
	}
	group := r.URL.Query().Get("group")

	s.oppMu.Lock()
	if s.oppVal != nil && time.Since(s.oppAt) < time.Minute && s.oppN == n && s.oppGroup == group {
		v := s.oppVal
		s.oppMu.Unlock()
		writeJSON(w, v)
		return
	}
	s.oppMu.Unlock()

	out := s.scanOpportunities(ctx, group, n)

	s.oppMu.Lock()
	s.oppVal, s.oppAt, s.oppN, s.oppGroup = out, time.Now(), n, group
	s.oppMu.Unlock()
	writeJSON(w, out)
}

// scanOpportunities 扫描主体。
// 1) 取最近交易日分析日报做初筛（可以买入/观望，剔除已持仓）；
// 2) 批量行情过滤买不起一手的；
// 3) 逐只过研判闸门 + 全策略信号共振；
// 4) 综合机会分 = 评分×0.55 + 信号共振×0.30 + 大盘环境×0.15，降序取前 n。
func (s *Server) scanOpportunities(ctx context.Context, group string, n int) map[string]any {
	empty := map[string]any{
		"generated_at": time.Now(),
		"source_date":  monitor.LastSessionDate(time.Now()),
		"regime":       s.marketRegime(ctx),
		"items":        []opportunity{},
		"note":         "暂无机会：分析日报未生成或候选均不符合条件",
	}

	cfg := s.cfg.Trading
	lot := cfg.LotSize
	if lot <= 0 {
		lot = 100
	}
	acc, err := s.trade.Account(ctx)
	if err != nil {
		empty["note"] = "账户读取失败：" + err.Error()
		return empty
	}
	total := acc.TotalAsset
	if total <= 0 {
		total = acc.Cash + acc.MarketValue
	}
	singleCap := total * cfg.SinglePositionPct / 100
	maxMV := total * cfg.MaxPositionPct / 100
	deployable := math.Min(acc.Cash, math.Max(0, maxMV-acc.MarketValue))

	date := monitor.LastSessionDate(time.Now())
	reports, err := s.st.ListAnalysisReportsByDate(date)
	if err != nil || len(reports) == 0 {
		return empty
	}

	var allow map[string]bool
	if group != "" {
		_, list := s.groupItems(group)
		allow = make(map[string]bool, len(list))
		for _, it := range list {
			allow[it.SecID] = true
		}
	}
	held := s.heldSecids(ctx)

	// 初筛：只留可买评级，按评分降序取 2n 进入精算
	type cand struct {
		secid, name, advice string
		score               float64
	}
	pool := make([]cand, 0, 2*n)
	for _, r := range reports {
		if allow != nil && !allow[r.SecID] {
			continue
		}
		if held[r.SecID] {
			continue
		}
		if r.Advice != "可以买入" && r.Advice != "观望，回踩支撑再介入" {
			continue
		}
		pool = append(pool, cand{secid: r.SecID, name: r.Name, advice: r.Advice, score: r.Score})
		if len(pool) >= 2*n {
			break
		}
	}
	if len(pool) == 0 {
		return empty
	}
	secids := make([]string, len(pool))
	for i, c := range pool {
		secids[i] = c.secid
	}
	quotes, qErr := s.tradeQuotes(ctx, secids)
	if qErr != nil {
		empty["note"] = "行情拉取失败：" + qErr.Error()
		return empty
	}

	regime := s.marketRegime(ctx)
	items := make([]opportunity, 0, n)
	for _, c := range pool {
		q, ok := quotes[c.secid]
		if !ok || q.Price <= 0 {
			continue
		}
		budget := math.Min(deployable, singleCap)
		if buyQty(budget, q.Price, cfg.CommissionRate, cfg.MinCommission, lot) < lot {
			continue // 一手都买不起
		}
		a, aErr := s.assess(ctx, c.secid, c.name)
		if aErr != nil {
			continue
		}
		a = gatedCopy(a)
		s.applyQualityGate(ctx, a)
		if a.Advice != "可以买入" && a.Advice != "观望，回踩支撑再介入" {
			continue // 闸门降档后不再可入手
		}
		sigs, sigErr := trading.GenerateAll(ctx, c.secid, c.name)
		if sigErr != nil || len(sigs) == 0 {
			continue // 信号算不出来就不给机会（宁缺毋滥）
		}
		buys, sells := 0, 0
		oppSigs := make([]oppSignal, 0, len(sigs))
		for _, sg := range sigs {
			switch sg.Action {
			case "buy":
				buys++
			case "sell":
				sells++
			}
			oppSigs = append(oppSigs, oppSignal{
				Strategy: sg.Strategy, Label: trading.StrategyName(sg.Strategy),
				Action: sg.Action, Reason: sg.Reason, Strength: sg.Strength,
			})
		}
		// 信号共振：(买-卖)/策略数 → -1~1，映射到 0~100
		resonance := float64(buys-sells) / float64(len(sigs))
		oppScore := math.Round((a.Score*0.55+(50+50*resonance)*0.30+regime.Score*0.15)*10) / 10

		entry := buyPrice(q.Price, a.Support)
		stop := a.StopLossAt(entry)
		target := a.Resistance
		// 数量先按满预算占位，排序后按机会分名次重新分配（见下）
		qty := buyQty(math.Min(deployable, singleCap), entry, cfg.CommissionRate, cfg.MinCommission, lot)
		if qty < lot {
			qty = lot
		}
		amount := entry * float64(qty)

		o := opportunity{
			SecID: c.secid, Code: q.Code, Name: c.name, Price: q.Price, Pct: q.Pct,
			Score: a.Score, Advice: a.Advice, Signals: oppSigs,
			BuySignals: buys, SellSignals: sells, OppScore: oppScore,
			Entry: entry, Stop: stop, Target: target, Qty: qty, Amount: amount,
			Plan:  planEntry(a, entry, stop, target, qty, lot),
			Risks: entryRisks(a, q.Price, regime, sells, len(sigs)),
		}
		o.Reason = "机会分 " + f1(o.OppScore) + " · 评分 " + f1(a.Score) + " · 信号 " +
			strconv.Itoa(buys) + "买/" + strconv.Itoa(sells) + "卖 · 环境" + regime.Label
		// 数量按当前可投入预算单独估算（各条互不扣减）：同时买多只会超总仓位上限，
		// 按机会分从高到低依次执行即可
		o.Risks = append(o.Risks, "数量按可投入预算 "+f1(deployable)+" 元单独估算；多只同时买入会超总仓位上限，按机会分从高到低依次执行")
		items = append(items, o)
	}
	if len(items) == 0 {
		return empty
	}
	sort.Slice(items, func(i, j int) bool { return items[i].OppScore > items[j].OppScore })
	if len(items) > n {
		items = items[:n]
	}
	return map[string]any{
		"generated_at": time.Now(),
		"source_date":  monitor.LastSessionDate(time.Now()),
		"regime":       regime,
		"items":        items,
	}
}

// planEntry 新建仓的操作计划（如果…就…）：先给建仓条件，再给持有期的止损/止盈/环境应对。
func planEntry(a *analysis.Assessment, entry, stop, target float64, qty int64, lot int64) []planStep {
	steps := make([]planStep, 0, 5)
	qtyStr := strconv.FormatInt(qty, 10)
	if a.Support > 0 {
		steps = append(steps, planStep{
			If:   "回踩支撑位 " + fp(a.Support) + " 附近企稳（不破）",
			Then: "限价 " + fp(entry) + " 买入 " + qtyStr + " 股（不追高，冲高不买）",
		})
	} else {
		steps = append(steps, planStep{
			If:   "回调企稳不破近期低点",
			Then: "限价 " + fp(entry) + " 买入 " + qtyStr + " 股",
		})
	}
	if stop > 0 {
		risk := (stop - entry) / entry * 100
		steps = append(steps, planStep{
			If:   "买入后跌破止损位 " + fp(stop) + "（较买价约 " + f1(risk) + "%）",
			Then: "无条件清仓，不补仓不摊薄",
		})
	}
	if target > 0 {
		gain := (target - entry) / entry * 100
		steps = append(steps, planStep{
			If:   "涨到压力位 " + fp(target) + "（较买价约 +" + f1(gain) + "%）",
			Then: "止盈一半先落袋，剩余仓位把止损上移到买价（保本止损）",
		})
	}
	steps = append(steps, planStep{
		If:   "大盘环境转弱势（" + weakRegimeCondition() + "）",
		Then: "暂停加仓；持仓跌破止损优先离场——弱势市高分也可能是陷阱",
	})
	return steps
}

// weakRegimeCondition 弱势环境的白话判断条件。
func weakRegimeCondition() string {
	return "指数跌破 MA20、板块普跌"
}

// entryRisks 新建仓的风险提示清单。
func entryRisks(a *analysis.Assessment, price float64, regime regimeResult, sells, totalStrategies int) []string {
	risks := make([]string, 0, 5)
	if a.Confidence > 0 && a.Confidence < 60 {
		risks = append(risks, "日K样本不足，评分置信度仅 "+f1(a.Confidence)+"，结论参考性有限")
	}
	if regime.Label == "弱势" {
		risks = append(risks, "大盘环境弱势（"+regime.Reason+"）：新仓务必减半或等企稳，高分也可能补跌")
	}
	if a.Support > 0 && price > a.Support*1.05 {
		risks = append(risks, "现价高于支撑位 "+fp(a.Support)+" 超 5%，直接买容错低，等回踩到支撑附近再接")
	}
	if sells >= 2 {
		risks = append(risks, "策略信号偏空："+strconv.Itoa(sells)+"/"+strconv.Itoa(totalStrategies)+" 个策略给卖出，与评分结论相左，谨慎")
	}
	if a.Advice == "观望，回踩支撑再介入" {
		risks = append(risks, "评级为「观望」：只有回踩支撑企稳才是买点，当前位置不是")
	}
	for _, note := range a.Notes {
		risks = append(risks, note) // 降档/口径说明（环境过滤、财报闸门、年线缺失等）
	}
	return risks
}
