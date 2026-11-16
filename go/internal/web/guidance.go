package web

// 交易指导 API：
//   GET  /api/trade/guidance?group=&n=   持仓处置建议 + 候选买入（分析研判 × 账户仓位）
//   POST /api/trade/positions/import     期初持仓导入（账户外已有持仓登记，不动现金）

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"time"

	"killer/internal/analysis"
	"killer/internal/model"
	"killer/internal/monitor"
	"killer/internal/source"
)

// 持仓处置动作
const (
	actHold  = "持有"
	actAdd   = "加仓"
	actCut   = "减仓"
	actClear = "清仓"
	actBuy   = "买入"
)

// buyQty 按预算算可买整手股数：占用资金含买入佣金（印花税只收卖出），不超预算。
func buyQty(budget, price float64, commissionRate, minCommission float64, lot int64) int64 {
	if budget <= 0 || price <= 0 || lot <= 0 {
		return 0
	}
	perLot := price * float64(lot)
	fee := perLot * commissionRate
	if fee < minCommission {
		fee = minCommission
	}
	lots := int64(math.Floor((budget + 1e-9) / (perLot + fee)))
	if lots < 0 {
		return 0
	}
	return lots * lot
}

// cutQty 把单票市值压到目标市值所需卖出的股数（按整手向上取整——卖少了压不回上限，宁可略减过头）。
func cutQty(qty int64, price, mv, targetMV float64, lot int64) int64 {
	if price <= 0 || mv <= targetMV || lot <= 0 {
		return 0
	}
	need := int64(math.Ceil((mv - targetMV) / price))
	n := (need + lot - 1) / lot * lot
	if n >= qty {
		return qty
	}
	return n
}

// guideHolding 给单只持仓定动作。
// 优先级：已破止损 → 清仓；研判回避 → 清仓；市值超单票上限 → 减到上限（纪律优先，偏多也减）；
// 研判偏多且还有买入预算 → 加仓，否则持有。
func guideHolding(a *analysis.Assessment, qty int64, price, mv, total, singleCap, deployable float64, lot int64, commissionRate, minCommission float64) (action string, suggestQty int64, suggestPrice float64, reason string) {
	if price <= 0 || qty <= 0 {
		return actHold, 0, 0, "缺少行情，暂不给建议"
	}
	if a.StopLoss > 0 && price < a.StopLoss {
		return actClear, qty, price, "现价已跌破止损参考位，按纪律离场"
	}
	weight := 0.0
	if total > 0 {
		weight = mv / total * 100
	}
	if a.Advice == "回避" {
		return actClear, qty, price, "研判「回避」，建议全部离场"
	}
	if mv > singleCap {
		if n := cutQty(qty, price, mv, singleCap, lot); n > 0 {
			capPct := 0.0
			if total > 0 {
				capPct = singleCap / total * 100
			}
			return actCut, n, price, "仓位 " + f1(weight) + "% 超单票上限 " + f1(capPct) + "%，减到上限以内"
		}
		return actHold, 0, 0, "仓位略超上限，但减一手反而跌破成本区，先持有"
	}
	if a.Advice == "偏弱，暂缓买入" {
		return actHold, 0, 0, "研判偏弱，不加仓，破止损位再离场"
	}
	budget := math.Min(deployable, singleCap-mv)
	if n := buyQty(budget, price, commissionRate, minCommission, lot); n > 0 {
		return actAdd, n, buyPrice(price, a.Support), "研判偏多且仓位未满，可回踩支撑加仓"
	}
	return actHold, 0, 0, "研判偏多，但买入预算不足一手，先持有"
}

// buyPrice 建议买入价：不追高，挂到支撑位附近（支撑位上浮 2%），现价更低则按现价。
func buyPrice(price, support float64) float64 {
	px := price
	if support > 0 {
		limit := support * 1.02
		if limit < px {
			px = limit
		}
	}
	return math.Round(px*100) / 100
}

// f1 保留 1 位小数拼字符串（拼 reason 用）。
func f1(v float64) string {
	return strconv.FormatFloat(math.Round(v*10)/10, 'f', 1, 64)
}

type guidanceHolding struct {
	SecID       string  `json:"secid"`
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Qty         int64   `json:"qty"`
	AvgCost     float64 `json:"avg_cost"`
	Price       float64 `json:"price"`
	Pct         float64 `json:"pct"`
	MarketValue float64 `json:"market_value"`
	WeightPct   float64 `json:"weight_pct"`
	Profit      float64 `json:"profit"`
	Score       float64 `json:"score"`
	Advice      string  `json:"advice"`
	Action      string  `json:"action"`
	SuggestQty  int64   `json:"suggest_qty"`
	SuggestPrice float64 `json:"suggest_price"`
	Support     float64 `json:"support"`
	Resistance  float64 `json:"resistance"`
	StopLoss    float64 `json:"stop_loss"`
	Reason      string  `json:"reason"`
}

type guidanceCandidate struct {
	SecID        string  `json:"secid"`
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Price        float64 `json:"price"`
	Pct          float64 `json:"pct"`
	Score        float64 `json:"score"`
	Advice       string  `json:"advice"`
	SuggestPrice float64 `json:"suggest_price"`
	SuggestQty   int64   `json:"suggest_qty"`
	Amount       float64 `json:"amount"`
	Support      float64 `json:"support"`
	Resistance   float64 `json:"resistance"`
	StopLoss     float64 `json:"stop_loss"`
	Reason       string  `json:"reason"`
}

type guidanceRisk struct {
	TotalAsset     float64  `json:"total_asset"`
	Cash           float64  `json:"cash"`
	FrozenCash     float64  `json:"frozen_cash"`
	MarketValue    float64  `json:"market_value"`
	PositionPct    float64  `json:"position_pct"`
	MaxPositionPct float64  `json:"max_position_pct"`
	SingleLimitPct float64  `json:"single_limit_pct"`
	Deployable     float64  `json:"deployable"`
	Notes          []string `json:"notes"`
}

// handleTradeGuidance GET /api/trade/guidance?group=&n=5
// 持仓逐只给处置建议；候选来自分析日报评分（只留"可以买入/观望"），按整手与仓位上限给买入数量。
func (s *Server) handleTradeGuidance(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	acc, err := s.trade.Account(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	positions, err := s.trade.Positions(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cfg := s.cfg.Trading
	lot := cfg.LotSize
	if lot <= 0 {
		lot = 100
	}
	total := acc.TotalAsset
	if total <= 0 {
		total = acc.Cash + acc.MarketValue
	}
	singleCap := total * cfg.SinglePositionPct / 100
	maxMV := total * cfg.MaxPositionPct / 100
	deployable := math.Min(acc.Cash, math.Max(0, maxMV-acc.MarketValue))

	risk := guidanceRisk{
		TotalAsset: total, Cash: acc.Cash, FrozenCash: acc.FrozenCash, MarketValue: acc.MarketValue,
		MaxPositionPct: cfg.MaxPositionPct, SingleLimitPct: cfg.SinglePositionPct,
		Deployable: deployable, Notes: []string{},
	}
	if acc.FrozenCash > 0 {
		risk.Notes = append(risk.Notes, "挂单冻结 "+f1(acc.FrozenCash)+" 元未计入可投入预算")
	}
	if total > 0 {
		risk.PositionPct = acc.MarketValue / total * 100
	}

	holdings := make([]guidanceHolding, 0, len(positions))
	for _, p := range positions {
		h := guidanceHolding{
			SecID: p.SecID, Code: p.Code, Name: p.Name, Qty: p.Quantity, AvgCost: p.AvgCost,
			Price: p.LastPrice, MarketValue: p.MarketValue, Profit: p.Profit,
		}
		if total > 0 {
			h.WeightPct = p.MarketValue / total * 100
		}
		a, aErr := s.assess(ctx, p.SecID, p.Name)
		if aErr != nil {
			h.Action, h.Reason = actHold, "研判失败："+aErr.Error()
			holdings = append(holdings, h)
			continue
		}
		h.Score, h.Advice = a.Score, a.Advice
		h.Pct, h.Support, h.Resistance, h.StopLoss = a.Pct, a.Support, a.Resistance, a.StopLoss
		h.Action, h.SuggestQty, h.SuggestPrice, h.Reason = guideHolding(
			a, p.Quantity, p.LastPrice, p.MarketValue, total, singleCap, deployable, lot,
			cfg.CommissionRate, cfg.MinCommission)
		if h.Action == actAdd {
			deployable -= h.SuggestPrice * float64(h.SuggestQty) // 加仓占用的预算不再给候选
		}
		if h.WeightPct > cfg.SinglePositionPct {
			risk.Notes = append(risk.Notes, h.Name+" 单票仓位 "+f1(h.WeightPct)+"%，已超上限 "+f1(cfg.SinglePositionPct)+"%")
		}
		holdings = append(holdings, h)
	}
	if risk.PositionPct > cfg.MaxPositionPct {
		risk.Notes = append(risk.Notes, "总仓位 "+f1(risk.PositionPct)+"% 已超上限 "+f1(cfg.MaxPositionPct)+"%，不再建议新开仓")
	}
	if deployable < singleCap*0.2 {
		risk.Notes = append(risk.Notes, "可投入预算仅剩 "+f1(deployable)+" 元，注意留够补仓与手续费")
	}

	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	if n <= 0 || n > 10 {
		n = 5
	}
	cands := s.guideCandidates(ctx, r.URL.Query().Get("group"), n, singleCap, deployable, lot, cfg.CommissionRate, cfg.MinCommission)

	writeJSON(w, map[string]any{
		"generated_at": time.Now(),
		"source_date":  monitor.LastSessionDate(time.Now()),
		"groups":       s.groupNames(),
		"risk":         risk,
		"holdings":     holdings,
		"candidates":   cands,
	})
}

// guideCandidates 挑可买候选：
// 1) 取最近交易日的分析日报评分（只留「可以买入/观望」，剔除已持仓）；
// 2) 批量行情算价格，按单票上限与可投入预算粗筛买得起 100 股的；
// 3) 前 3n 只现算研判，按支撑位定建议限价与整手数量。
func (s *Server) guideCandidates(ctx context.Context, group string, n int, singleCap, deployable float64, lot int64, commissionRate, minCommission float64) []guidanceCandidate {
	date := monitor.LastSessionDate(time.Now())
	reports, err := s.st.ListAnalysisReportsByDate(date)
	if err != nil || len(reports) == 0 {
		return []guidanceCandidate{}
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
	type cand struct {
		secid, code, name, advice string
		score, price              float64
	}
	pool := make([]cand, 0, 40)
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
		if len(pool) >= n*3 {
			break
		}
	}
	if len(pool) == 0 {
		return []guidanceCandidate{}
	}
	secids := make([]string, len(pool))
	for i, c := range pool {
		secids[i] = c.secid
	}
	quotes, qErr := s.tradeQuotes(ctx, secids)
	if qErr != nil {
		return []guidanceCandidate{}
	}
	out := make([]guidanceCandidate, 0, n)
	for _, c := range pool {
		q, ok := quotes[c.secid]
		if !ok || q.Price <= 0 {
			continue
		}
		c.price = q.Price
		budget := math.Min(deployable, singleCap)
		if buyQty(budget, c.price, commissionRate, minCommission, lot) < lot {
			continue // 一手都买不起
		}
		a, aErr := s.assess(ctx, c.secid, c.name)
		if aErr != nil {
			continue
		}
		px := buyPrice(q.Price, a.Support)
		qty := buyQty(math.Min(deployable, singleCap), px, commissionRate, minCommission, lot)
		if qty < lot {
			continue
		}
		amount := px * float64(qty)
		g := guidanceCandidate{
			SecID: c.secid, Code: q.Code, Name: c.name, Price: q.Price, Pct: q.Pct,
			Score: a.Score, Advice: a.Advice, SuggestPrice: px, SuggestQty: qty, Amount: amount,
			Support: a.Support, Resistance: a.Resistance, StopLoss: a.StopLoss,
		}
		g.Reason = "评分 " + f1(a.Score) + " · " + a.Advice
		if a.Support > 0 && q.Price > a.Support*1.05 {
			g.Reason += "；现价高于支撑 5%+，等回踩到 " + f1(px) + " 再接"
		}
		if a.StopLoss > 0 {
			g.Reason += "；止损 " + f1(a.StopLoss)
		}
		out = append(out, g)
		deployable -= amount
		if len(out) >= n {
			break
		}
	}
	return out
}

// heldSecids 已持仓标的（候选要去重）。
func (s *Server) heldSecids(ctx context.Context) map[string]bool {
	positions, err := s.trade.Positions(ctx)
	if err != nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(positions))
	for _, p := range positions {
		out[p.SecID] = true
	}
	return out
}

// tradeQuotes 批量行情（股票/ETF 通吃），按 secid 索引。
func (s *Server) tradeQuotes(ctx context.Context, secids []string) (map[string]model.Stock, error) {
	quotes, err := source.FetchUSQuotes(ctx, secids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]model.Stock, len(quotes))
	for _, q := range quotes {
		out[q.SecID] = q
	}
	return out, nil
}

// handleTradeImport POST /api/trade/positions/import — 期初持仓导入。
// body: {secid, code, name, quantity, avg_cost}；覆盖式写入，不动现金、不生成成交记录。
func (s *Server) handleTradeImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SecID    string  `json:"secid"`
		Code     string  `json:"code"`
		Name     string  `json:"name"`
		Quantity int64   `json:"quantity"`
		AvgCost  float64 `json:"avg_cost"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "非法请求体: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.SecID == "" || req.Quantity <= 0 || req.AvgCost <= 0 {
		http.Error(w, "需要 secid、数量、成本价", http.StatusBadRequest)
		return
	}
	if err := s.trade.ImportPosition(req.SecID, req.Code, req.Name, req.Quantity, req.AvgCost); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"secid": req.SecID, "quantity": req.Quantity})
}
