// 回测引擎：基于历史日K，按策略信号模拟买卖，输出收益曲线与绩效指标。
package trading

import (
	"context"
	"fmt"
	"math"
	"time"

	"killer/internal/model"
	"killer/internal/source"
)

// loadKlinesForBacktest 加载回测所需日K（尽量多取历史，上限 500）。
func loadKlinesForBacktest(ctx context.Context, secid string) ([]model.Kline, error) {
	if source.IsBoardSecid(secid) {
		kl, err := source.New("tencent")
		if err != nil {
			return nil, err
		}
		return kl.FetchKline(ctx, secid, 500)
	}
	for _, name := range []string{"eastmoney", "sina"} {
		kl, err := source.New(name)
		if err != nil {
			continue
		}
		klines, err := kl.FetchKline(ctx, secid, 500)
		if err == nil && len(klines) > 0 {
			return klines, nil
		}
	}
	return nil, fmt.Errorf("无法获取 %s 的日K数据", secid)
}

// defaultParams 策略默认参数（小写别名）。
func defaultParams(name string) Params {
	return DefaultParams(name)
}

// Backtest 对单个标的运行回测。
// strategy: ma_cross | breakout | rsi
// 交易规则：满仓进出（全仓买/全仓卖），忽略手续费精度（按 commission_rate 计）。
func (e *Engine) Backtest(ctx context.Context, name, secid, displayName string, initialCash float64) (*model.BacktestResult, error) {
	klines, err := loadKlinesForBacktest(ctx, secid)
	if err != nil {
		return nil, err
	}
	if len(klines) < 30 {
		return nil, fmt.Errorf("K线数据不足（需至少 30 根，实际 %d 根）", len(klines))
	}
	if initialCash <= 0 {
		initialCash = 100000
	}

	closes := make([]float64, len(klines))
	dates := make([]string, len(klines))
	for i, k := range klines {
		closes[i] = k.Close
		dates[i] = k.Date
	}

	// 逐日生成信号并模拟交易
	params := defaultParams(name)
	cash := initialCash
	shares := 0.0 // 持股数量（允许小数，简化）
	equity := make([]model.EquityPoint, 0, len(klines))
	trades := make([]model.Trade, 0)
	var peak float64 = initialCash
	var maxDD float64 = 0

	winCount := 0
	tradeCount := 0
	var entryPrice float64 = 0

	for i := 1; i < len(klines); i++ {
		action := "hold"
		switch name {
		case "ma_cross":
			action, _, _ = maCross(closes[:i+1], params.Short, params.Long)
		case "breakout":
			action, _, _ = breakout(closes[:i+1], params.Period)
		case "rsi":
			action, _, _ = rsiSignal(closes[:i+1], params.Period)
		}

		px := closes[i]
		if action == "buy" && cash > px {
			// 全仓买入
			shares = cash / px
			cash = 0
			entryPrice = px
			trades = append(trades, model.Trade{
				SecID: secid, Code: codeOf(secid), Name: displayName, Side: model.SideBuy,
				Price: px, Quantity: int64(shares), Amount: shares * px, TradedAt: time.Now(),
			})
			tradeCount++
		} else if action == "sell" && shares > 0 {
			// 全仓卖出
			proceeds := shares * px
			if proceeds > entryPrice*shares {
				winCount++
			}
			cash = proceeds
			shares = 0
			trades = append(trades, model.Trade{
				SecID: secid, Code: codeOf(secid), Name: displayName, Side: model.SideSell,
				Price: px, Quantity: int64(shares), Amount: proceeds, TradedAt: time.Now(),
			})
		}

		// 记录权益
		asset := cash + shares*px
		equity = append(equity, model.EquityPoint{Date: dates[i], Asset: round2(asset)})
		if asset > peak {
			peak = asset
		}
		dd := (peak - asset) / peak * 100
		if dd > maxDD {
			maxDD = dd
		}
	}

	finalAsset := cash + shares*closes[len(closes)-1]
	totalReturn := (finalAsset - initialCash) / initialCash * 100

	// 年化收益率（按交易日约 250 天）
	days := len(klines)
	annualReturn := (math.Pow(finalAsset/initialCash, 250.0/float64(days)) - 1) * 100

	// 胜率
	winRate := 0.0
	if tradeCount > 0 {
		winRate = float64(winCount) / float64(tradeCount) * 100
	}

	// 夏普比率（按日收益率）
	sharpe := calcSharpe(equity)

	return &model.BacktestResult{
		Strategy: name, SecID: secid, Name: displayName,
		InitialCash: initialCash, FinalAsset: round2(finalAsset),
		TotalReturn: round2(totalReturn), AnnualReturn: round2(annualReturn),
		MaxDrawdown: round2(maxDD), WinRate: round2(winRate), Sharpe: round2(sharpe),
		TradeCount: tradeCount, EquityCurve: equity, Trades: trades,
		StartedAt: time.Now(), EndedAt: time.Now(),
	}, nil
}

// calcSharpe 计算夏普比率（日收益均值 / 日收益标准差 * sqrt(250)）。
func calcSharpe(equity []model.EquityPoint) float64 {
	if len(equity) < 2 {
		return 0
	}
	rets := make([]float64, 0, len(equity)-1)
	for i := 1; i < len(equity); i++ {
		if equity[i-1].Asset > 0 {
			rets = append(rets, equity[i].Asset/equity[i-1].Asset-1)
		}
	}
	if len(rets) < 2 {
		return 0
	}
	mean := 0.0
	for _, r := range rets {
		mean += r
	}
	mean /= float64(len(rets))
	variance := 0.0
	for _, r := range rets {
		variance += (r - mean) * (r - mean)
	}
	variance /= float64(len(rets))
	std := math.Sqrt(variance)
	if std == 0 {
		return 0
	}
	return mean / std * math.Sqrt(250)
}

// codeOf secid → 代码（去掉市场前缀，板块保留原样）。
func codeOf(secid string) string {
	if len(secid) > 2 && secid[1] == '.' {
		return secid[2:]
	}
	return secid
}
