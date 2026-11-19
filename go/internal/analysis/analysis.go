// 分析系统：基于日K与实时价对标的做多维度技术研判，
// 输出综合评分（0-100）与当下操作建议（是否为合理买点、支撑/压力/止损参考位）。
//
// 内置五个分析系统（各自独立打分 -100~+100，加权合成）：
//   - trend      趋势分析：均线排列、价格与均线位置（含年线 MA250）、均线斜率
//   - momentum   动量分析：MACD 金叉死叉与柱状方向、RSI 超买超卖
//   - volume     量价分析：量比、放量上攻/放量下跌/缩量回调等配合关系
//   - support    支撑压力：现价在 20 日低~120 日高区间的位置、支撑位/压力位距离
//   - volatility 波动风险：ATR% 与近期振幅，衡量追高风险
package analysis

import (
	"fmt"
	"math"
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

// SubScore 单个分析系统内的子项得分（供"分析详情"展开看分数从哪来）。
type SubScore struct {
	Name  string  `json:"name"`  // 子项名（如 均线排列）
	Score float64 `json:"score"` // 该子项贡献的分值（可正可负）
	Note  string  `json:"note"`  // 当前读数说明（如 多头排列 / 年线缺失）
}

// Finding 单个分析系统的结论。
type Finding struct {
	Analyzer string     `json:"analyzer"` // 英文名
	Title    string     `json:"title"`    // 中文名
	Score    float64    `json:"score"`    // -100（极度看空）~ +100（极度看多）
	Verdict  string     `json:"verdict"`  // 偏多 / 中性 / 偏空
	Detail   string     `json:"detail"`   // 关键数值
	Reason   string     `json:"reason"`   // 结论说明
	Subs     []SubScore `json:"subs"`     // 子项得分（合计 = Score）
}

// Assessment 综合研判结论。
type Assessment struct {
	SecID       string    `json:"secid"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Price       float64   `json:"price"`       // 参考价（实时价优先，缺省用最新收盘）
	Pct         float64   `json:"pct"`         // 涨跌幅（%），实时价缺失时用最新两根日K推算
	Score       float64   `json:"score"`       // 综合评分 0-100
	Advice      string    `json:"advice"`      // 当下操作建议（含市场环境/质量闸门降档后的结果）
	Summary     string    `json:"summary"`     // 一句话综述（主要理由 + 风险提示）
	Support     float64   `json:"support"`     // 支撑位（近 20 日最低）
	Resistance  float64   `json:"resistance"`  // 压力位（近 120 日最高）
	StopLoss    float64   `json:"stop_loss"`   // 参考止损位（支撑−1×ATR 与 现价−2.5×ATR 取高，不深于 -15%）
	Atr         float64   `json:"atr"`         // 近 14 日均真实波幅（绝对值，供按买入价重算止损）
	Confidence  float64   `json:"confidence"`  // 评分置信度 0-100（按日K样本量，长周期项缺失会拉低）
	Regime      string    `json:"regime"`      // 评估时的市场环境（强势/中性/弱势），web 层择时模块填入
	Notes       []string  `json:"notes"`       // 口径与降档说明（年线缺失、环境过滤、质量闸门等）
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
	resistance float64 // 近 120 日最高
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
		Support:     round3(lv.support),
		Resistance:  round3(lv.resistance),
		StopLoss:    round3(stopLossOf(price, lv)),
		Atr:         round3(lv.atr),
		Confidence:  round1(confidenceOf(len(klines))),
		Findings:    findings,
		KlineCount:  len(klines),
		KlineDate:   last.Date,
		GeneratedAt: time.Now(),
	}, nil
}

// confidenceOf 评分置信度（0-100）：日K样本 <250 根时长周期项（年线/周线）不参与评分，
// 结论只是中短期口径，样本越少越不可信。
func confidenceOf(klineCount int) float64 {
	switch {
	case klineCount >= 250:
		return 100
	case klineCount >= 60:
		return 60 + float64(klineCount-60)/190*40 // 60~250 根线性爬升
	default:
		return float64(klineCount) / 60 * 60
	}
}

// calcLevels 计算支撑/压力/ATR。支撑=近 20 日最低，压力=近 120 日最高。
func calcLevels(klines []model.Kline, price float64) levels {
	var lv levels
	n := len(klines)

	lo20 := klines[n-1].Low
	for i := n - 2; i >= 0 && i >= n-20; i-- {
		if klines[i].Low < lo20 {
			lo20 = klines[i].Low
		}
	}
	hi120 := klines[n-1].High
	for i := n - 2; i >= 0 && i >= n-120; i-- {
		if klines[i].High > hi120 {
			hi120 = klines[i].High
		}
	}
	lv.support = lo20
	lv.resistance = hi120

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

// stopLossOf 参考止损位（波动率口径）：支撑位下方 1×ATR 与基准价下方 2.5×ATR 取较高者，
// 且不深于基准价 -15%（止损太远等于没有）。固定 -8% 对高波动股在噪音里、对低波动股又太宽。
// 基准价传现价即当下口径，传买入价即限价计划口径（见 StopLossAt）。
func stopLossOf(price float64, lv levels) float64 {
	if price <= 0 {
		return 0
	}
	if lv.atr <= 0 {
		return price * 0.92 // 无 ATR 时退回固定 -8%
	}
	sl := lv.support - lv.atr
	if floor := price - 2.5*lv.atr; sl < floor {
		sl = floor
	}
	if cap := price * 0.85; sl < cap {
		sl = cap
	}
	return sl
}

// StopLossAt 以指定买入价为基准的参考止损位。
// "等回踩再接"的限价计划必须锚定买入价——用现价算的止损高于回踩买入价，一成交就贴着止损。
func (a *Assessment) StopLossAt(entry float64) float64 {
	return round3(stopLossOf(entry, levels{support: a.Support, atr: a.Atr}))
}

// runTrend 趋势分析：均线排列（35）+ 价与 MA20 位置（20）+ MA20 斜率（20）+ 价与 MA60 位置（10）+ 价与年线 MA250 位置（15）。
// 年线是长周期多空分界：站上偏多、跌破偏空；历史不足 250 根时 sma 退化为全历史均线（上市不久的标的仍按可得数据评）。
// runTrend 趋势分析：均线排列（30）+ 价与 MA20 位置（18）+ MA20 斜率（17）+ 价与 MA60（10）
// + 年线 MA250（15）+ 周线共振（10）。
// 长周期项（MA60/年线/周线）样本不足时记 0 分：退化成"可用全量均值"会把新股的均价当长均线扣分。
func runTrend(klines []model.Kline, price float64, _ levels) Finding {
	closes := closesOf(klines)
	ma5 := sma(closes, 5)
	ma10 := sma(closes, 10)
	ma20 := sma(closes, 20)

	score := 0.0
	var tags []string
	var subs []SubScore
	add := func(pts float64, name, note string) {
		score += pts
		if note != "" {
			tags = append(tags, note)
		}
		subs = append(subs, SubScore{Name: name, Score: pts, Note: note})
	}
	switch {
	case ma5 > ma10 && ma10 > ma20:
		add(30, "均线排列", "均线多头排列")
	case ma5 < ma10 && ma10 < ma20:
		add(-30, "均线排列", "均线空头排列")
	default:
		add(0, "均线排列", "均线缠绕")
	}
	if price > ma20 {
		add(18, "价与MA20", "价站上MA20")
	} else {
		add(-18, "价与MA20", "价跌破MA20")
	}
	// MA20 五日前后对比取斜率
	if len(closes) >= 25 {
		ma20Prev := sma(closes[:len(closes)-5], 20)
		switch {
		case ma20 > ma20Prev*1.005:
			add(17, "MA20斜率", "MA20上行")
		case ma20 < ma20Prev*0.995:
			add(-17, "MA20斜率", "MA20下行")
		default:
			add(0, "MA20斜率", "")
		}
	}
	detail := fmt.Sprintf("MA5=%.2f MA20=%.2f", ma5, ma20)
	if len(closes) >= 60 {
		ma60 := sma(closes, 60)
		detail += fmt.Sprintf(" MA60=%.2f", ma60)
		if price > ma60 {
			add(10, "价与MA60", "")
		} else {
			add(-10, "价与MA60", "价在MA60下方")
		}
	} else {
		add(0, "价与MA60", fmt.Sprintf("MA60缺失（仅 %d 根）", len(closes)))
	}
	if len(closes) >= 250 {
		ma250 := sma(closes, 250)
		detail += fmt.Sprintf(" MA250=%.2f", ma250)
		if price > ma250 {
			add(15, "年线MA250", "价站上年线")
		} else {
			add(-15, "年线MA250", "价在年线下方")
		}
	} else {
		// 不足 250 根不打分：sma 会退化成全量均值，那不是年线
		add(0, "年线MA250", fmt.Sprintf("年线缺失（仅 %d 根）", len(closes)))
	}
	if w := weeklyScore(klines, price); w != 0 {
		if w > 0 {
			add(w, "周线共振", "周线共振向上")
		} else {
			add(w, "周线共振", "周线走弱")
		}
	} else {
		add(0, "周线共振", "周线样本不足")
	}
	return Finding{
		Analyzer: NameTrend, Title: "趋势分析", Score: score,
		Detail: detail + fmt.Sprintf("，现价 %.2f", price),
		Reason: strings.Join(tags, "，"),
		Subs:   subs,
	}
}

// weeklyScore 周线共振：周 MA5 > 周 MA10 且上行、价在周 MA10 上 → +10；全部反向 → -10；其余 0。
// 周线不足 12 根记 0（约一个季度）。
func weeklyScore(klines []model.Kline, price float64) float64 {
	wc := weeklyCloses(klines)
	if len(wc) < 12 {
		return 0
	}
	ma5 := sma(wc, 5)
	ma10 := sma(wc, 10)
	ma5Prev := sma(wc[:len(wc)-1], 5)
	switch {
	case ma5 > ma10 && ma5 > ma5Prev && price > ma10:
		return 10
	case ma5 < ma10 && ma5 < ma5Prev && price < ma10:
		return -10
	}
	return 0
}

// weeklyCloses 按 ISO 周聚合日K（每周取最后一根收盘），日期解析失败时每根算独立一周。
func weeklyCloses(klines []model.Kline) []float64 {
	out := make([]float64, 0, len(klines)/5+1)
	lastKey := ""
	for _, k := range klines {
		key := k.Date
		if t, err := time.Parse("2006-01-02", k.Date); err == nil {
			y, w := t.ISOWeek()
			key = fmt.Sprintf("%d-%02d", y, w)
		}
		if key == lastKey && len(out) > 0 {
			out[len(out)-1] = k.Close
			continue
		}
		out = append(out, k.Close)
		lastKey = key
	}
	return out
}

// runMomentum 动量分析：MACD 状态（35）+ 柱状动能方向（20）+ RSI 区间（35）。
func runMomentum(klines []model.Kline, price float64, _ levels) Finding {
	closes := closesOf(klines)
	dif, dea := macd(closes)
	score := 0.0
	var tags []string
	var subs []SubScore
	add := func(pts float64, name, note string) {
		score += pts
		if note != "" {
			tags = append(tags, note)
		}
		subs = append(subs, SubScore{Name: name, Score: pts, Note: note})
	}

	if dif > dea {
		add(35, "MACD状态", "MACD金叉状态")
	} else {
		add(-35, "MACD状态", "MACD死叉状态")
	}
	// 柱状图（2*(DIF-DEA)）较上一根的变化 = 动能方向
	if len(closes) >= 2 {
		hist := 2 * (dif - dea)
		difP, deaP := macd(closes[:len(closes)-1])
		histPrev := 2 * (difP - deaP)
		if hist > histPrev {
			add(20, "动能方向", "红柱走强")
		} else {
			add(-20, "动能方向", "动能转弱")
		}
	}
	r := rsi(closes, 14)
	switch {
	case r >= 50 && r <= 70:
		add(35, "RSI强弱", fmt.Sprintf("RSI=%.1f 强势区", r))
	case r > 70:
		add(-25, "RSI强弱", fmt.Sprintf("RSI=%.1f 超买，追高风险", r))
	case r < 30:
		add(20, "RSI强弱", fmt.Sprintf("RSI=%.1f 超卖，反弹机会", r))
	default:
		add(0, "RSI强弱", fmt.Sprintf("RSI=%.1f 中性", r))
	}
	return Finding{
		Analyzer: NameMomentum, Title: "动量分析", Score: score,
		Detail: fmt.Sprintf("DIF=%.3f DEA=%.3f RSI14=%.1f，现价 %.2f", dif, dea, r, price),
		Reason: strings.Join(tags, "，"),
		Subs:   subs,
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
	// 幅度收敛：量价是单项，别让它一锤定音（趋势五项合计才 ±100）
	switch {
	case ratio >= 1.5 && chg > 0:
		score = 35
		tags = append(tags, "放量上攻，资金介入明显")
	case ratio >= 1.5 && chg <= 0:
		score = -40
		tags = append(tags, "放量下跌，抛压较重")
	case ratio < 0.7 && chg <= 0:
		score = 20
		tags = append(tags, "缩量回调，抛压枯竭（健康调整）")
	case ratio < 0.7 && chg > 0:
		score = -12
		tags = append(tags, "缩量上涨，量价背离")
	case chg > 0:
		score = 12
		tags = append(tags, "量能平稳，温和放量")
	default:
		score = -8
		tags = append(tags, "量能平稳但价格走弱")
	}
	return Finding{
		Analyzer: NameVolume, Title: "量价分析", Score: score,
		Detail: fmt.Sprintf("量比(5日/20日)=%.2f，近5日涨跌 %.2f%%", ratio, chg),
		Reason: strings.Join(tags, "，"),
		Subs:   []SubScore{{Name: "量价配合", Score: score, Note: strings.Join(tags, "，")}},
	}
}

// runSupport 支撑压力：现价在 20日低~120日高 区间的位置打分，突破与逼近上沿单独处理。
// runSupport 支撑压力：近 20 日区间定位买点（上下沿同窗，位置才有意义），
// 120 日高只作长期压力/突破判定与目标位——否则区间上沿遥不可及，强势股全被"紧贴压力位"扣分。
func runSupport(klines []model.Kline, price float64, lv levels) Finding {
	mk := func(score float64, detail, reason string) Finding {
		return Finding{
			Analyzer: NameSupport, Title: "支撑压力", Score: score,
			Detail: detail, Reason: reason,
			Subs: []SubScore{{Name: "区间位置", Score: score, Note: reason}},
		}
	}
	if lv.resistance <= lv.support {
		return mk(0, "区间数据不足", "高低点区间异常，暂无法判断")
	}
	hi20 := hiHigh(klines, 20)
	// 突破 120 日高点（长期压力）：最强形态但需防回踩
	if price > lv.resistance {
		return mk(25,
			fmt.Sprintf("支撑=%.2f（20日低）压力=%.2f（120日高），现价 %.2f 已突破长期上沿", lv.support, lv.resistance, price),
			"突破120日高点，强势；注意回踩确认")
	}
	// 跌破 20 日支撑：破位风险
	if price < lv.support {
		return mk(-50,
			fmt.Sprintf("支撑=%.2f（20日低）压力=%.2f（120日高），现价 %.2f 已跌破支撑", lv.support, lv.resistance, price),
			"跌破20日支撑位，破位下行风险")
	}
	// 突破近 20 日高：阶段走强（还在 120 日高下方）
	if price > hi20 {
		return mk(10,
			fmt.Sprintf("支撑=%.2f（20日低）20日高=%.2f，现价 %.2f 突破阶段上沿", lv.support, hi20, price),
			"突破20日高点，阶段走强")
	}
	if hi20 <= lv.support {
		return mk(0, "近20日区间过窄", "波动过窄，暂不给位置判断")
	}
	pos := (price - lv.support) / (hi20 - lv.support) * 100 // 0=区间底 100=区间顶
	score := 0.0
	var tags []string
	// 支撑刚被刷新（20 日最低出现在最近 3 根）→ 地板还在塌，不给"贴近支撑"的低吸分
	freshLow := lowIndex(klines, 20) >= len(klines)-3
	switch {
	case pos < 15 && !freshLow:
		score = 50
		tags = append(tags, "贴近支撑位，买点位置较优")
	case pos < 15:
		score = 0
		tags = append(tags, "低点刚刷新，支撑还在下移")
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
		score = -40
		tags = append(tags, "紧贴20日高点，等突破或回踩")
	}
	return mk(score,
		fmt.Sprintf("支撑=%.2f（20日低）20日高=%.2f 压力=%.2f（120日高），现价 %.2f 处于近20日区间 %.0f%%", lv.support, hi20, lv.resistance, price, pos),
		strings.Join(tags, "，"))
}

// hiHigh 近 n 根日K的最高价。
func hiHigh(klines []model.Kline, n int) float64 {
	start := len(klines) - n
	if start < 0 {
		start = 0
	}
	hi := klines[start].High
	for _, k := range klines[start+1:] {
		if k.High > hi {
			hi = k.High
		}
	}
	return hi
}

// lowIndex 近 n 根日K里最低价出现的位置（0=最早一根，len-1=最新一根）。
func lowIndex(klines []model.Kline, n int) int {
	start := len(klines) - n
	if start < 0 {
		start = 0
	}
	idx := start
	for i := start + 1; i < len(klines); i++ {
		if klines[i].Low < klines[idx].Low {
			idx = i
		}
	}
	return idx
}

// runVolatility 波动风险：ATR% 分档（-40~+30）+ 近期波动是否放大（±15）。
// runVolatility 波动风险：ATR% 相对自身近一年的分位（跨资产可比——否则科技股被结构性扣分、
// 银行股白拿满分），外加近 5 日波动是否放大。样本不足退回绝对阈值。
func runVolatility(klines []model.Kline, _ float64, lv levels) Finding {
	score := 0.0
	var tags []string
	var subs []SubScore
	add := func(pts float64, name, note string) {
		score += pts
		if note != "" {
			tags = append(tags, note)
		}
		subs = append(subs, SubScore{Name: name, Score: pts, Note: note})
	}
	q := atrPercentile(klines)
	switch {
	case q < 0: // 历史不足，退回绝对阈值
		switch {
		case lv.atrPct < 1.5:
			add(30, "波动水平", "波动温和，持仓体验好")
		case lv.atrPct < 3:
			add(15, "波动水平", "波动适中")
		case lv.atrPct < 5:
			add(-10, "波动水平", "波动偏大，注意仓位")
		default:
			add(-30, "波动水平", "波动剧烈，重仓风险高")
		}
	default:
		switch {
		case q <= 25:
			add(30, "波动水平", fmt.Sprintf("波动处于自身低位（%.0f 分位）", q))
		case q <= 50:
			add(15, "波动水平", fmt.Sprintf("波动低于自身常态（%.0f 分位）", q))
		case q <= 75:
			add(-10, "波动水平", fmt.Sprintf("波动高于自身常态（%.0f 分位），注意仓位", q))
		default:
			add(-30, "波动水平", fmt.Sprintf("波动处于自身高位（%.0f 分位），重仓风险高", q))
		}
	}
	// 近 5 日平均振幅 vs 近 20 日，判断波动是否在放大
	amp5 := meanAmp(klines[len(klines)-min(5, len(klines)):])
	amp20 := meanAmp(klines[len(klines)-min(20, len(klines)):])
	if amp20 > 0 && amp5 > amp20*1.3 {
		add(-15, "波动趋势", "近5日波动放大")
	} else {
		add(0, "波动趋势", "")
	}
	return Finding{
		Analyzer: NameVolatility, Title: "波动风险", Score: score,
		Detail: fmt.Sprintf("ATR14=%.2f（%.2f%%，自身 %.0f 分位），近5日均振幅 %.2f%%", lv.atr, lv.atrPct, q, amp5),
		Reason: strings.Join(tags, "，"),
		Subs:   subs,
	}
}

// atrPercentile 当前 ATR% 在自身滚动 ATR% 序列（最多近 250 根）中的分位 0-100；样本不足返回 -1。
func atrPercentile(klines []model.Kline) float64 {
	n := len(klines)
	if n < 60 {
		return -1
	}
	var hist []float64
	for i := n - 1; i >= 0 && len(hist) < 250; i-- {
		sum, cnt := 0.0, 0
		for j := i; j > 0 && cnt < 14; j-- {
			tr := klines[j].High - klines[j].Low
			if d := klines[j].High - klines[j-1].Close; d > tr {
				tr = d
			}
			if d := klines[j-1].Close - klines[j].Low; d > tr {
				tr = d
			}
			sum += tr
			cnt++
		}
		if cnt > 0 && klines[i].Close > 0 {
			hist = append(hist, sum/float64(cnt)/klines[i].Close*100)
		}
	}
	if len(hist) < 30 {
		return -1
	}
	cur := hist[0]
	le := 0
	for _, v := range hist {
		if v <= cur {
			le++
		}
	}
	return float64(le) / float64(len(hist)) * 100
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

// round3 保留 3 位小数：ETF 价位最小变动 0.001，round2 会把 0.566 的止损截成 0.57。
func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}
