package analysis

// 评分有效性回测：按日滚动重算评分（评估日只用截至当日的数据），
// 统计各评分分层的未来 5/10/20 根收益与胜率——回答"这套评分到底有没有预测力"。

import "killer/internal/model"

// 滚动回测的观察窗口（交易日）。
const (
	RollWarmup = 250 // 与年线 MA250 同口径：不足 250 根时年线项缺席，分层统计会混两种语义
)

// RollHorizons 未来收益观察期（根），顺序即 RollEval.Ret 与 BucketStat 各数组的顺序。
var RollHorizons = [...]int{5, 10, 20}

// RollEval 单个评估日的滚动评分与未来收益。
type RollEval struct {
	Date   string     `json:"date"`
	Score  float64    `json:"score"`
	Advice string     `json:"advice"`
	Ret    [3]float64 `json:"ret"`   // 未来 5/10/20 根收益（%），入场=信号次根收盘
	Valid  [3]bool    `json:"valid"` // 对应观察期是否有足够K线
}

// BucketStat 评分分层统计（毛收益，未计手续费）。
type BucketStat struct {
	Bucket string     `json:"bucket"` // 评分区间
	Advice string     `json:"advice"` // 该档的操作建议
	Count  int        `json:"count"`  // 评估次数
	N      [3]int     `json:"n"`      // 各观察期的有效样本数（5/10/20），观察期不足的不计入均值
	Win    [3]float64 `json:"win"`    // 胜率（%）：未来 5/10/20 根收益>0 的占比
	Avg    [3]float64 `json:"avg"`    // 平均收益（%）
	Excess [3]float64 `json:"excess"` // 超额（%）：分层平均 − 全样本平均
}

// RollScore 逐根滚动重算评分：评估日 t 只用 klines[:t+1]，价格取当日收盘。
// 信号在收盘后产生，故入场价取次根收盘（可执行价），出场取入场后第 k 根收盘；
// 最后一根没有"次根"，不评估。
// warmup 不足 30 按 30；总根数不足 warmup+2 返回空。
func RollScore(klines []model.Kline, warmup int) []RollEval {
	if warmup < 30 {
		warmup = 30
	}
	n := len(klines)
	if n <= warmup+1 {
		return nil
	}
	out := make([]RollEval, 0, n-warmup-1)
	for i := warmup; i < n-1; i++ {
		seg := klines[:i+1]
		price := seg[i].Close
		pct := 0.0
		if i > 0 && seg[i-1].Close > 0 {
			pct = (price - seg[i-1].Close) / seg[i-1].Close * 100
		}
		a, err := Assess("", "", seg, price, pct)
		if err != nil {
			continue
		}
		ev := RollEval{Date: seg[i].Date, Score: a.Score, Advice: a.Advice}
		entry := klines[i+1].Close
		for hi, h := range RollHorizons {
			if entry <= 0 || i+1+h >= n {
				continue
			}
			ev.Ret[hi] = (klines[i+1+h].Close - entry) / entry * 100
			ev.Valid[hi] = true
		}
		out = append(out, ev)
	}
	return out
}

// bucketOf 评分区间（与 adviceOf 的分档同边界）。
func bucketOf(score float64) string {
	switch {
	case score >= 80:
		return "≥80"
	case score >= 65:
		return "65-80"
	case score >= 50:
		return "50-65"
	case score >= 35:
		return "35-50"
	default:
		return "<35"
	}
}

// Aggregate 按评分分层统计未来收益；超额 = 分层平均 − 全样本平均。
// 无样本的分层不出现在结果里。
func Aggregate(evals []RollEval) []BucketStat {
	type acc struct {
		n      int
		win    [3]float64
		sum    [3]float64
		cnt    [3]float64 // 该观察期的有效样本数
		advice string
	}
	order := []string{"≥80", "65-80", "50-65", "35-50", "<35"}
	byBucket := map[string]*acc{}
	var all acc
	for _, ev := range evals {
		b := bucketOf(ev.Score)
		if byBucket[b] == nil {
			byBucket[b] = &acc{}
		}
		for _, cur := range []*acc{byBucket[b], &all} {
			cur.n++
			if cur.advice == "" {
				cur.advice = ev.Advice
			}
			for hi := range RollHorizons {
				if !ev.Valid[hi] {
					continue
				}
				cur.sum[hi] += ev.Ret[hi]
				cur.cnt[hi]++
				if ev.Ret[hi] > 0 {
					cur.win[hi]++
				}
			}
		}
	}
	avg := func(cur *acc, hi int) float64 {
		if cur.cnt[hi] == 0 {
			return 0
		}
		return cur.sum[hi] / cur.cnt[hi]
	}
	out := make([]BucketStat, 0, len(order))
	for _, b := range order {
		cur := byBucket[b]
		if cur == nil {
			continue
		}
		st := BucketStat{Bucket: b, Advice: cur.advice, Count: cur.n}
		for hi := range RollHorizons {
			st.N[hi] = int(cur.cnt[hi])
			if cur.cnt[hi] > 0 {
				st.Win[hi] = cur.win[hi] / cur.cnt[hi] * 100
			}
			st.Avg[hi] = avg(cur, hi)
			st.Excess[hi] = avg(cur, hi) - avg(&all, hi)
		}
		out = append(out, st)
	}
	return out
}
