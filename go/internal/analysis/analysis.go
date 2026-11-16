// 分析系统：基于日K与实时价对标的做多维度技术研判，
// 输出综合评分（0-100）与当下操作建议（是否为合理买点、支撑/压力/止损参考位）。
//
// 内置五个分析系统（各自独立打分 -100~+100，加权合成）：
//   - trend      趋势分析：均线排列、价格与均线位置、均线斜率
//   - momentum   动量分析：MACD 金叉死叉与柱状方向、RSI 超买超卖
//   - volume     量价分析：量比、放量上攻/放量下跌/缩量回调等配合关系
//   - support    支撑压力：现价在 60 日区间的位置、支撑位/压力位距离
//   - volatility 波动风险：ATR% 与近期振幅，衡量追高风险
package analysis

import (
	"fmt"
	"strings"
	"time"

	"killer/internal/model"
)

// 分析系统名称。
const (
	NameTrend      = "trend"
	NameMomentum   = "momentum"
	NameVolume     = "volume"
	NameSupport    = "support"
	NameVolatility = "volatility"
)

// Finding 单个分析系统的结论。
type Finding struct {
	Analyzer string  `json:"analyzer"` // 英文名
	Title    string  `json:"title"`    // 中文名
	Score    float64 `json:"score"`    // -100（极度看空）~ +100（极度看多）
	Verdict  string  `json:"verdict"`  // 偏多 / 中性 / 偏空
	Detail   string  `json:"detail"`   // 关键数值
	Reason   string  `json:"reason"`   // 结论说明
}

// Assessment 综合研判结论。
type Assessment struct {
	SecID       string    `json:"secid"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Price       float64   `json:"price"`       // 参考价（实时价优先，缺省用最新收盘）
	Pct         float64   `json:"pct"`         // 涨跌幅（%），实时价缺失时用最新两根日K推算
	Score       float64   `json:"score"`       // 综合评分 0-100
	Advice      string    `json:"advice"`      // 当下操作建议
	Summary     string    `json:"summary"`     // 一句话综述（主要理由 + 风险提示）
	Support     float64   `json:"support"`     // 支撑位（近 20 日最低）
	Resistance  float64   `json:"resistance"`  // 压力位（近 60 日最高）
	StopLoss    float64   `json:"stop_loss"`   // 参考止损位（支撑下方 1×ATR，且不深于 -8%）
	Findings    []Finding `json:"findings"`    // 各分析系统结论（展示顺序）
	KlineCount  int       `json:"kline_count"` // 参与计算的日K根数
	KlineDate   string    `json:"kline_date"`  // 最新一根日K日期
	GeneratedAt time.Time `json:"generated_at"`
}

// analyzer 单个分析系统定义。
type analyzer struct {
	name   string
	title  string
	weight float64 // 合成权重（五项合计 1.0）
	run    func(klines []model.Kline, price float64, lv levels) Finding
}

// analyzers 全部分析系统，顺序即前端展示顺序。
var analyzers = []analyzer{
	{name: NameTrend, title: "趋势分析", weight: 0.30, run: runTrend},
	{name: NameMomentum, title: "动量分析", weight: 0.25, run: runMomentum},
	{name: NameVolume, title: "量价分析", weight: 0.20, run: runVolume},
	{name: NameSupport, title: "支撑压力", weight: 0.15, run: runSupport},
	{name: NameVolatility, title: "波动风险", weight: 0.10, run: runVolatility},
}

// levels 支撑/压力/波动幅度计算结果（支撑压力与止损共用）。
type levels struct {
	support    float64 // 近 20 日最低
	resistance float64 // 近 60 日最高
	atr        float64 // 近 14 日均真实波幅（绝对值）
	atrPct     float64 // ATR 占现价比例（%）
}

// Assess 对单个标的做全量分析。
// price/pct 传 0 表示无实时行情，回退用最新日K收盘推算。
func Assess(secid, name string, klines []model.Kline, price, pct float64) (*Assessment, error) {
	if len(klines) < 30 {
		return nil, fmt.Errorf("日K数据不足（%d 根，需至少 30 根）", len(klines))
	}
	last := klines[len(klines)-1]
	if price <= 0 {
		price = last.Close
		if pct == 0 && len(klines) >= 2 {
			prev := klines[len(klines)-2].Close
			if prev > 0 {
				pct = (last.Close - prev) / prev * 100
			}
		}
	}
	lv := calcLevels(klines, price)

	findings := make([]Finding, 0, len(analyzers))
	raw := 0.0
	for _, a := range analyzers {
		f := a.run(klines, price, lv)
		if f.Score > 100 {
			f.Score = 100
		}
		if f.Score < -100 {
			f.Score = -100
		}
		f.Verdict = verdictOf(f.Score)
		findings = append(findings, f)
		raw += f.Score * a.weight
	}

	score := (raw + 100) / 2
	return &Assessment{
		SecID:       secid,
		Code:        codeOf(secid),
		Name:        name,
		Price:       price,
		Pct:         pct,
		Score:       round1(score),
		Advice:      adviceOf(score),
		Summary:     summaryOf(score, findings),
		Support:     round2(lv.support),
		Resistance:  round2(lv.resistance),
		StopLoss:    round2(stopLossOf(price, lv)),
		Findings:    findings,
		KlineCount:  len(klines),
		KlineDate:   last.Date,
		GeneratedAt: time.Now(),
	}, nil
}

// calcLevels 计算支撑/压力/ATR。支撑=近 20 日最低，压力=近 60 日最高。
func calcLevels(klines []model.Kline, price float64) levels {
	var lv levels
	n := len(klines)

	lo20 := klines[n-1].Low
	for i := n - 2; i >= 0 && i >= n-20; i-- {
		if klines[i].Low < lo20 {
			lo20 = klines[i].Low
		}
	}
	hi60 := klines[n-1].High
	for i := n - 2; i >= 0 && i >= n-60; i-- {
		if klines[i].High > hi60 {
			hi60 = klines[i].High
		}
	}
	lv.support = lo20
	lv.resistance = hi60

	// ATR14（真实波幅 = max(高-低, |高-前收|, |低-前收|)）
	sum := 0.0
	cnt := 0
	for i := n - 1; i > 0 && cnt < 14; i-- {
		tr := klines[i].High - klines[i].Low
		if d := klines[i].High - klines[i-1].Close; d > tr {
			tr = d
		}
		if d := klines[i-1].Close - klines[i].Low; d > tr {
			tr = d
		}
		sum += tr
		cnt++
	}
	if cnt > 0 {
		lv.atr = sum / float64(cnt)
		if price > 0 {
			lv.atrPct = lv.atr / price * 100
		}
	}
	return lv
}

// stopLossOf 参考止损位：支撑位下方 1×ATR，且不深于现价 -8%（取两者中更高者）。
func stopLossOf(price float64, lv levels) float64 {
	sl := lv.support - lv.atr
	floor := price * 0.92
	if sl < floor {
		sl = floor
	}
	if sl <= 0 {
		sl = floor
	}
	return sl
}

// runTrend 趋势分析：均线排列（40）+ 价与 MA20 位置（25）+ MA20 斜率（25）+ 价与 MA60 位置（10）。
func runTrend(klines []model.Kline, price float64, _ levels) Finding {
	closes := closesOf(klines)
	ma5 := sma(closes, 5)
	ma10 := sma(closes, 10)
	ma20 := sma(closes, 20)
	ma60 := sma(closes, 60)

	score := 0.0
	var tags []string
	switch {
	case ma5 > ma10 && ma10 > ma20:
		score += 40
		tags = append(tags, "均线多头排列")
	case ma5 < ma10 && ma10 < ma20:
		score -= 40
		tags = append(tags, "均线空头排列")
	default:
		tags = append(tags, "均线缠绕")
	}
	if price > ma20 {
		score += 25
		tags = append(tags, "价站上MA20")
	} else {
		score -= 25
		tags = append(tags, "价跌破MA20")
	}
	// MA20 五日前后对比取斜率
	if len(closes) >= 25 {
		ma20Prev := sma(closes[:len(closes)-5], 20)
		if ma20 > ma20Prev*1.005 {
			score += 25
			tags = append(tags, "MA20上行")
		} else if ma20 < ma20Prev*0.995 {
			score -= 25
			tags = append(tags, "MA20下行")
		}
	}
	if price > ma60 {
		score += 10
	} else {
		score -= 10
		tags = append(tags, "价在MA60下方")
	}
	return Finding{
		Analyzer: NameTrend, Title: "趋势分析", Score: score,
		Detail: fmt.Sprintf("MA5=%.2f MA20=%.2f MA60=%.2f，现价 %.2f", ma5, ma20, ma60, price),
		Reason: strings.Join(tags, "，"),
	}
}

// runMomentum 动量分析：MACD 状态（35）+ 柱状动能方向（20）+ RSI 区间（35）。
func runMomentum(klines []model.Kline, price float64, _ levels) Finding {
	closes := closesOf(klines)
	dif, dea := macd(closes)
	score := 0.0
	var tags []string

	if dif > dea {
		score += 35
		tags = append(tags, "MACD金叉状态")
	} else {
		score -= 35
		tags = append(tags, "MACD死叉状态")
	}
	// 柱状图（2*(DIF-DEA)）较上一根的变化 = 动能方向
	if len(closes) >= 2 {
		hist := 2 * (dif - dea)
		difP, deaP := macd(closes[:len(closes)-1])
		histPrev := 2 * (difP - deaP)
		if hist > histPrev {
			score += 20
			tags = append(tags, "红柱走强")
		} else {
			score -= 20
			tags = append(tags, "动能转弱")
		}
	}
	r := rsi(closes, 14)
	switch {
	case r >= 50 && r <= 70:
		score += 35
		tags = append(tags, fmt.Sprintf("RSI=%.1f 强势区", r))
	case r > 70:
		score -= 25
		tags = append(tags, fmt.Sprintf("RSI=%.1f 超买，追高风险", r))
	case r < 30:
		score += 20
		tags = append(tags, fmt.Sprintf("RSI=%.1f 超卖，反弹机会", r))
	default:
		tags = append(tags, fmt.Sprintf("RSI=%.1f 中性", r))
	}
	return Finding{
		Analyzer: NameMomentum, Title: "动量分析", Score: score,
		Detail: fmt.Sprintf("DIF=%.3f DEA=%.3f RSI14=%.1f，现价 %.2f", dif, dea, r, price),
		Reason: strings.Join(tags, "，"),
	}
}

// runVolume 量价分析：近 5 日与 20 日量比 + 近 5 日涨跌配合关系。
func runVolume(klines []model.Kline, _ float64, _ levels) Finding {
	n := len(klines)
	vol5 := meanVol(klines[n-min(5, n):])
	vol20 := meanVol(klines[n-min(20, n):])
	ratio := 0.0
	if vol20 > 0 {
		ratio = vol5 / vol20
	}
	// 近 5 日涨跌幅
	chg := 0.0
	if n >= 6 && klines[n-6].Close > 0 {
		chg = (klines[n-1].Close - klines[n-6].Close) / klines[n-6].Close * 100
	}

	score := 0.0
	var tags []string
	switch {
	case ratio >= 1.5 && chg > 0:
		score = 45
		tags = append(tags, "放量上攻，资金介入明显")
	case ratio >= 1.5 && chg <= 0:
		score = -50
		tags = append(tags, "放量下跌，抛压较重")
	case ratio < 0.7 && chg <= 0:
		score = 25
		tags = append(tags, "缩量回调，抛压枯竭（健康调整）")
	case ratio < 0.7 && chg > 0:
		score = -15
		tags = append(tags, "缩量上涨，量价背离")
	case chg > 0:
		score = 15
		tags = append(tags, "量能平稳，温和放量")
	default:
		score = -10
		tags = append(tags, "量能平稳但价格走弱")
	}
	return Finding{
		Analyzer: NameVolume, Title: "量价分析", Score: score,
		Detail: fmt.Sprintf("量比(5日/20日)=%.2f，近5日涨跌 %.2f%%", ratio, chg),
		Reason: strings.Join(tags, "，"),
	}
}

// runSupport 支撑压力：现价在 20日低~60日高 区间的位置打分，突破与逼近上沿单独处理。
func runSupport(klines []model.Kline, price float64, lv levels) Finding {
	if lv.resistance <= lv.support {
		return Finding{
			Analyzer: NameSupport, Title: "支撑压力", Score: 0,
			Detail: "区间数据不足",
			Reason: "高低点区间异常，暂无法判断",
		}
	}
	// 突破 60 日高点：强势但需防回踩
	if price > lv.resistance {
		return Finding{
			Analyzer: NameSupport, Title: "支撑压力", Score: 25,
			Detail: fmt.Sprintf("支撑=%.2f（20日低）压力=%.2f（60日高），现价 %.2f 已突破上沿", lv.support, lv.resistance, price),
			Reason: "突破60日高点，强势；注意回踩确认",
		}
	}
	// 跌破 20 日支撑（现价低于近 20 日全部低点）：破位风险
	if price < lv.support {
		return Finding{
			Analyzer: NameSupport, Title: "支撑压力", Score: -50,
			Detail: fmt.Sprintf("支撑=%.2f（20日低）压力=%.2f（60日高），现价 %.2f 已跌破支撑", lv.support, lv.resistance, price),
			Reason: "跌破20日支撑位，破位下行风险",
		}
	}
	pos := (price - lv.support) / (lv.resistance - lv.support) * 100 // 0=区间底 100=区间顶
	score := 0.0
	var tags []string
	switch {
	case pos < 15:
		score = 50
		tags = append(tags, "贴近支撑位，买点位置较优")
	case pos < 35:
		score = 30
		tags = append(tags, "位于区间下沿，距离支撑近")
	case pos < 60:
		score = 0
		tags = append(tags, "位于区间中部")
	case pos < 80:
		score = -25
		tags = append(tags, "位于区间上沿，追高风险上升")
	default:
		score = -45
		tags = append(tags, "紧贴压力位，不宜追高")
	}
	return Finding{
		Analyzer: NameSupport, Title: "支撑压力", Score: score,
		Detail: fmt.Sprintf("支撑=%.2f（20日低）压力=%.2f（60日高），现价 %.2f 处于区间 %.0f%%", lv.support, lv.resistance, price, pos),
		Reason: strings.Join(tags, "，"),
	}
}

// runVolatility 波动风险：ATR% 分档（-40~+30）+ 近期波动是否放大（±15）。
func runVolatility(klines []model.Kline, _ float64, lv levels) Finding {
	score := 0.0
	var tags []string
	switch {
	case lv.atrPct < 1.5:
		score += 30
		tags = append(tags, "波动温和，持仓体验好")
	case lv.atrPct < 3:
		score += 15
		tags = append(tags, "波动适中")
	case lv.atrPct < 5:
		score -= 10
		tags = append(tags, "波动偏大，注意仓位")
	default:
		score -= 30
		tags = append(tags, "波动剧烈，重仓风险高")
	}
	// 近 5 日平均振幅 vs 近 20 日，判断波动是否在放大
	amp5 := meanAmp(klines[len(klines)-min(5, len(klines)):])
	amp20 := meanAmp(klines[len(klines)-min(20, len(klines)):])
	if amp20 > 0 && amp5 > amp20*1.3 {
		score -= 15
		tags = append(tags, "近5日波动放大")
	}
	return Finding{
		Analyzer: NameVolatility, Title: "波动风险", Score: score,
		Detail: fmt.Sprintf("ATR14=%.2f（%.2f%%），近5日均振幅 %.2f%%", lv.atr, lv.atrPct, amp5),
		Reason: strings.Join(tags, "，"),
	}
}

// adviceOf 综合评分 → 操作建议。
func adviceOf(score float64) string {
	switch {
	case score >= 80:
		return "可积极买入"
	case score >= 65:
		return "可以买入"
	case score >= 50:
		return "观望，回踩支撑再介入"
	case score >= 35:
		return "偏弱，暂缓买入"
	default:
		return "回避"
	}
}

// summaryOf 一句话综述：主要理由 + 明显拖累项的风险提示。
func summaryOf(score float64, findings []Finding) string {
	best, worst := findings[0], findings[0]
	for _, f := range findings {
		if f.Score > best.Score {
			best = f
		}
		if f.Score < worst.Score {
			worst = f
		}
	}
	advice := adviceOf(score)
	if best.Score >= 20 {
		s := fmt.Sprintf("%s：%s", advice, best.Reason)
		if worst.Score <= -20 && worst.Analyzer != best.Analyzer {
			s += fmt.Sprintf("；风险点——%s（%s）", worst.Reason, worst.Title)
		}
		return s
	}
	if worst.Score <= -20 {
		return fmt.Sprintf("%s：%s（%s）", advice, worst.Reason, worst.Title)
	}
	return advice + "：各分析系统信号均不突出"
}

// verdictOf 单项分数 → 多空结论。
func verdictOf(score float64) string {
	switch {
	case score >= 20:
		return "偏多"
	case score <= -20:
		return "偏空"
	default:
		return "中性"
	}
}

// codeOf 从 secid 提取纯代码（1.688981 → 688981）。
func codeOf(secid string) string {
	for i := 0; i < len(secid); i++ {
		if secid[i] == '.' {
			return secid[i+1:]
		}
	}
	return secid
}

// ---------- 指标计算辅助 ----------

func closesOf(klines []model.Kline) []float64 {
	out := make([]float64, len(klines))
	for i, k := range klines {
		out[i] = k.Close
	}
	return out
}

// sma 简单移动平均（样本不足 period 时用可得全部）。
func sma(closes []float64, period int) float64 {
	n := len(closes)
	if n == 0 {
		return 0
	}
	if period > n {
		period = n
	}
	sum := 0.0
	for _, v := range closes[n-period:] {
		sum += v
	}
	return sum / float64(period)
}

// macd 计算最新一根的 DIF 与 DEA（EMA12-EMA26，DEA=DIF 的 EMA9）。
func macd(closes []float64) (dif, dea float64) {
	if len(closes) < 2 {
		return 0, 0
	}
	ema12 := emaSeries(closes, 12)
	ema26 := emaSeries(closes, 26)
	difs := make([]float64, len(closes))
	for i := range closes {
		difs[i] = ema12[i] - ema26[i]
	}
	deas := emaSeries(difs, 9)
	return difs[len(difs)-1], deas[len(deas)-1]
}

// emaSeries 指数移动平均序列（初值取首个收盘）。
func emaSeries(vals []float64, period int) []float64 {
	out := make([]float64, len(vals))
	if len(vals) == 0 {
		return out
	}
	k := 2.0 / float64(period+1)
	out[0] = vals[0]
	for i := 1; i < len(vals); i++ {
		out[i] = vals[i]*k + out[i-1]*(1-k)
	}
	return out
}

// rsi 相对强弱指标（最近 period 根，Wilder 平滑简化版）。
func rsi(closes []float64, period int) float64 {
	n := len(closes)
	if n < period+1 {
		return 50
	}
	gain, loss := 0.0, 0.0
	for i := n - period; i < n; i++ {
		diff := closes[i] - closes[i-1]
		if diff > 0 {
			gain += diff
		} else {
			loss -= diff
		}
	}
	avgGain := gain / float64(period)
	avgLoss := loss / float64(period)
	if avgLoss == 0 {
		return 100
	}
	return 100 - 100/(1+avgGain/avgLoss)
}

func meanVol(klines []model.Kline) float64 {
	if len(klines) == 0 {
		return 0
	}
	sum := 0.0
	for _, k := range klines {
		sum += k.Volume
	}
	return sum / float64(len(klines))
}

// meanAmp 平均振幅（%，相对前收盘）。
func meanAmp(klines []model.Kline) float64 {
	if len(klines) < 2 {
		return 0
	}
	sum := 0.0
	cnt := 0
	for i := 1; i < len(klines); i++ {
		if klines[i-1].Close > 0 {
			sum += (klines[i].High - klines[i].Low) / klines[i-1].Close * 100
			cnt++
		}
	}
	if cnt == 0 {
		return 0
	}
	return sum / float64(cnt)
}

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
