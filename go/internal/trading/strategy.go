// 策略信号引擎：基于日K/实时价生成买卖信号。
//
// 内置策略（供信号面板与回测共用）：
//   - ma_cross   均线金叉/死叉（短均线上穿长均线→买，下穿→卖）
//   - breakout   突破策略（收盘价突破 N 日最高→买，跌破 N 日最低→卖）
//   - rsi        RSI 超买超卖（RSI<30 超卖→买，RSI>70 超买→卖）
package trading

import (
	"context"
	"fmt"
	"time"

	"killer/internal/model"
	"killer/internal/source"
)

// Params 策略参数。
type Params struct {
	Short int `json:"short"` // 短周期（默认 5）
	Long  int `json:"long"`  // 长周期（默认 20）
	Period int `json:"period"` // 突破/RSI 周期（默认 20）
}

// DefaultParams 返回策略默认参数。
func DefaultParams(name string) Params {
	switch name {
	case "ma_cross":
		return Params{Short: 5, Long: 20}
	case "breakout":
		return Params{Period: 20}
	case "rsi":
		return Params{Period: 14}
	}
	return Params{Short: 5, Long: 20, Period: 20}
}

// Strategy 策略名列表。
var Strategies = []string{"ma_cross", "breakout", "rsi"}

// StrategyName 中文名。
func StrategyName(name string) string {
	switch name {
	case "ma_cross":
		return "均线金叉"
	case "breakout":
		return "突破策略"
	case "rsi":
		return "RSI超买超卖"
	}
	return name
}

// LoadKlines 加载标的日K（供信号与回测共用）。
func LoadKlines(ctx context.Context, secid string, limit int) ([]model.Kline, error) {
	if limit <= 0 || limit > 500 {
		limit = 120
	}
	if source.IsBoardSecid(secid) {
		// 板块走腾讯
		kl, err := source.New("tencent")
		if err != nil {
			return nil, err
		}
		return kl.FetchKline(ctx, secid, limit)
	}
	// 个股/指数：东财优先，新浪兜底
	for _, name := range []string{"eastmoney", "sina"} {
		kl, err := source.New(name)
		if err != nil {
			continue
		}
		klines, err := kl.FetchKline(ctx, secid, limit)
		if err == nil && len(klines) > 0 {
			return klines, nil
		}
	}
	return nil, fmt.Errorf("无法获取 %s 的日K数据", secid)
}

// Generate 生成单个标的的策略信号。返回 action: buy/sell/hold。
func Generate(ctx context.Context, name, secid, displayName string) (*model.Signal, error) {
	params := DefaultParams(name)
	klines, err := LoadKlines(ctx, secid, 120)
	if err != nil {
		return nil, err
	}
	if len(klines) < 2 {
		return nil, fmt.Errorf("K线数据不足")
	}
	last := klines[len(klines)-1]
	sig := &model.Signal{
		SecID: secid, Name: displayName, Strategy: name,
		Action: "hold", Price: last.Close, GeneratedAt: time.Now(),
	}

	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i] = k.Close
	}

	switch name {
	case "ma_cross":
		sig.Action, sig.Reason, sig.Strength = maCross(closes, params.Short, params.Long)
	case "breakout":
		sig.Action, sig.Reason, sig.Strength = breakout(closes, params.Period)
	case "rsi":
		sig.Action, sig.Reason, sig.Strength = rsiSignal(closes, params.Period)
	default:
		return nil, fmt.Errorf("未知策略: %s", name)
	}
	return sig, nil
}

// maCross 均线金叉/死叉。返回 action/reason/strength。
func maCross(closes []float64, short, long int) (string, string, float64) {
	n := len(closes)
	if n < long+1 {
		return "hold", "数据不足", 0
	}
	smaShort := sma(closes, short)
	smaLong := sma(closes, long)
	// 上一根
	smaShortPrev := sma(closes[:n-1], short)
	smaLongPrev := sma(closes[:n-1], long)

	if smaShort > smaLong && smaShortPrev <= smaLongPrev {
		return "buy", fmt.Sprintf("MA%d 上穿 MA%d（金叉）", short, long), 0.7
	}
	if smaShort < smaLong && smaShortPrev >= smaLongPrev {
		return "sell", fmt.Sprintf("MA%d 下穿 MA%d（死叉）", short, long), 0.7
	}
	// 无交叉：给弱信号（多头排列持偏多，空头排列偏空）
	if smaShort > smaLong {
		return "hold", fmt.Sprintf("MA%d > MA%d 多头排列", short, long), 0.3
	}
	return "hold", fmt.Sprintf("MA%d < MA%d 空头排列", short, long), 0.3
}

// breakout 突破策略。
func breakout(closes []float64, period int) (string, string, float64) {
	n := len(closes)
	if n < period+1 {
		return "hold", "数据不足", 0
	}
	prevHigh := max(closes[n-period-1 : n-1])
	prevLow := min(closes[n-period-1 : n-1])
	cur := closes[n-1]
	if cur > prevHigh {
		return "buy", fmt.Sprintf("收盘价 %.2f 突破 %d 日新高 %.2f", cur, period, prevHigh), 0.8
	}
	if cur < prevLow {
		return "sell", fmt.Sprintf("收盘价 %.2f 跌破 %d 日新低 %.2f", cur, period, prevLow), 0.8
	}
	return "hold", fmt.Sprintf("区间震荡（%d 日高 %.2f / 低 %.2f）", period, prevHigh, prevLow), 0.2
}

// rsiSignal RSI 超买超卖。
func rsiSignal(closes []float64, period int) (string, string, float64) {
	r := rsi(closes, period)
	if r < 0 {
		return "hold", "数据不足", 0
	}
	if r < 30 {
		return "buy", fmt.Sprintf("RSI=%.1f 超卖", r), 0.6
	}
	if r > 70 {
		return "sell", fmt.Sprintf("RSI=%.1f 超买", r), 0.6
	}
	return "hold", fmt.Sprintf("RSI=%.1f 中性", r), 0.1
}

// sma 简单移动平均（最近 period 根）。
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

// rsi 相对强弱指标（最近 period 根，Wilder 平滑简化版）。
func rsi(closes []float64, period int) float64 {
	n := len(closes)
	if n < period+1 {
		return -1
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
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}

func max(v []float64) float64 {
	m := v[0]
	for _, x := range v[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func min(v []float64) float64 {
	m := v[0]
	for _, x := range v[1:] {
		if x < m {
			m = x
		}
	}
	return m
}
