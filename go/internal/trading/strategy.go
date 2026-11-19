// 策略信号引擎：基于日K/实时价生成买卖信号。策略以注册制接入，
// 新策略只需 Register 一处，信号面板 / 回测 / 机会扫描自动可用。
//
// 内置策略：
//   - ma_cross   均线金叉/死叉（短均线上穿长均线→买，下穿→卖）
//   - breakout   突破策略（收盘价突破 N 日最高→买，跌破 N 日最低→卖）
//   - rsi        RSI 超买超卖（RSI<30 超卖→买，RSI>70 超买→卖）
//   - macd       MACD 金叉/死叉（DIF 上穿 DEA→买，下穿→卖）
package trading

import (
	"context"
	"fmt"
	"time"

	"killer/internal/model"
	"killer/internal/source"
)

// Params 策略参数。各策略按约定取用：ma_cross 用 Short/Long，
// breakout/rsi 用 Period，macd 用 Short=快线(12)/Long=慢线(26)/Period=信号线(9)。
type Params struct {
	Short  int `json:"short"`  // 短周期（默认 5）
	Long   int `json:"long"`   // 长周期（默认 20）
	Period int `json:"period"` // 突破/RSI 周期（默认 20）
}

// DefaultParams 返回策略默认参数。
func DefaultParams(name string) Params {
	if s, ok := registry[name]; ok {
		return s.Params
	}
	return Params{Short: 5, Long: 20, Period: 20}
}

// SignalFunc 策略信号函数：输入截至当前的收盘价序列，返回 action(buy/sell/hold)/理由/强度。
type SignalFunc func(closes []float64, p Params) (action, reason string, strength float64)

// Strategy 策略注册项。
type Strategy struct {
	Name   string     // 机器名（配置/接口用）
	Label  string     // 中文名（界面展示）
	Params Params     // 默认参数
	Fn     SignalFunc // 信号函数
}

// registry 策略注册表；Strategies 为注册顺序的策略名列表（供 API 枚举）。
var (
	registry   = map[string]Strategy{}
	Strategies []string
)

// Register 注册策略（重复注册同名覆盖，保持首次出现的顺序）。
func Register(s Strategy) {
	if _, ok := registry[s.Name]; !ok {
		Strategies = append(Strategies, s.Name)
	}
	registry[s.Name] = s
}

func init() {
	Register(Strategy{"ma_cross", "均线金叉", Params{Short: 5, Long: 20}, maCrossWrap})
	Register(Strategy{"breakout", "突破策略", Params{Period: 20}, breakoutWrap})
	Register(Strategy{"rsi", "RSI超买超卖", Params{Period: 14}, rsiWrap})
	Register(Strategy{"macd", "MACD金叉", Params{Short: 12, Long: 26, Period: 9}, macdWrap})
}

// StrategyName 中文名。
func StrategyName(name string) string {
	if s, ok := registry[name]; ok {
		return s.Label
	}
	return name
}

// signalFunc 取已注册策略的信号函数；未注册返回 nil。
func signalFunc(name string) SignalFunc {
	if s, ok := registry[name]; ok {
		return s.Fn
	}
	return nil
}

// GenerateAll 对单个标的跑全部已注册策略（日K只拉一次），供机会扫描做信号共振。
func GenerateAll(ctx context.Context, secid, displayName string) ([]model.Signal, error) {
	klines, err := LoadKlines(ctx, secid, 250)
	if err != nil {
		return nil, err
	}
	if len(klines) < 2 {
		return nil, fmt.Errorf("K线数据不足")
	}
	closes := make([]float64, len(klines))
	for i, k := range klines {
		closes[i] = k.Close
	}
	last := klines[len(klines)-1]
	out := make([]model.Signal, 0, len(Strategies))
	for _, name := range Strategies {
		action, reason, strength := signalFunc(name)(closes, DefaultParams(name))
		out = append(out, model.Signal{
			SecID: secid, Code: codeOf(secid), Name: displayName, Strategy: name,
			Action: action, Price: last.Close, Strength: strength,
			Reason: reason, GeneratedAt: time.Now(),
		})
	}
	return out, nil
}

// LoadKlines 加载标的日K（供信号与回测共用）。
func LoadKlines(ctx context.Context, secid string, limit int) ([]model.Kline, error) {
	if limit <= 0 || limit > 500 {
		limit = 250
	}
	if source.IsBoardSecid(secid) {
		// 板块走腾讯
		kl, err := source.New("tencent")
		if err != nil {
			return nil, err
		}
		return kl.FetchKline(ctx, secid, limit)
	}
	// 个股/指数：前复权源优先（腾讯 qfq / 东财 fqt=1），新浪无复权参数只兜底——
	// 不复权K线的除权假跳空会让评分与回测失真
	for _, name := range []string{"tencent", "eastmoney", "sina"} {
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
	klines, err := LoadKlines(ctx, secid, 250)
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

	fn := signalFunc(name)
	if fn == nil {
		return nil, fmt.Errorf("未知策略: %s", name)
	}
	sig.Action, sig.Reason, sig.Strength = fn(closes, params)
	return sig, nil
}

// 信号函数包装：把策略签名对齐 SignalFunc（参数从 Params 解包）。
func maCrossWrap(closes []float64, p Params) (string, string, float64) {
	return maCross(closes, p.Short, p.Long)
}
func breakoutWrap(closes []float64, p Params) (string, string, float64) {
	return breakout(closes, p.Period)
}
func rsiWrap(closes []float64, p Params) (string, string, float64) {
	return rsiSignal(closes, p.Period)
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

// macdWrap MACD 金叉/死叉：DIF 上穿 DEA→买，下穿→卖。
func macdWrap(closes []float64, p Params) (string, string, float64) {
	return macdSignal(closes, p.Short, p.Long, p.Period)
}

// macdSignal MACD 信号。fast=快线 EMA 周期(12)，slow=慢线(26)，signal=信号线(9)。
func macdSignal(closes []float64, fast, slow, signal int) (string, string, float64) {
	n := len(closes)
	if n < slow+signal {
		return "hold", "数据不足", 0
	}
	dif := emaSeries(closes, fast)
	dea := emaSeries(dif, signal)
	// dif/dea 长度与 closes 相同（EMA 从头递推），取最后两根
	curDIF, curDEA := dif[n-1], dea[n-1]
	prevDIF, prevDEA := dif[n-2], dea[n-2]
	hist := 2 * (curDIF - curDEA)

	if curDIF > curDEA && prevDIF <= prevDEA {
		return "buy", fmt.Sprintf("DIF 上穿 DEA（金叉），MACD柱=%.4f", hist), 0.7
	}
	if curDIF < curDEA && prevDIF >= prevDEA {
		return "sell", fmt.Sprintf("DIF 下穿 DEA（死叉），MACD柱=%.4f", hist), 0.7
	}
	if curDIF > curDEA {
		return "hold", fmt.Sprintf("DIF>DEA 多头，MACD柱=%.4f", hist), 0.3
	}
	return "hold", fmt.Sprintf("DIF<DEA 空头，MACD柱=%.4f", hist), 0.3
}

// emaSeries 计算完整 EMA 序列（首值取 SMA 种子，与常见行情软件一致）。
func emaSeries(values []float64, period int) []float64 {
	n := len(values)
	if n == 0 {
		return nil
	}
	if period > n {
		period = n
	}
	out := make([]float64, n)
	k := 2.0 / float64(period+1)
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	out[period-1] = sum / float64(period)
	for i := period; i < n; i++ {
		out[i] = values[i]*k + out[i-1]*(1-k)
	}
	// 种子之前的部分向前回填种子值，保证序列长度对齐（前段不参与判定）
	for i := 0; i < period-1 && n > 0; i++ {
		out[i] = out[period-1]
	}
	return out
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
