package source

// 纳斯达克100成分股：来源 yfiua.github.io（GitHub Pages，月度更新，实测可达）。
// 成分表变化频率低（每年两次再平衡），缓存 6 小时。

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"killer/internal/model"
)

const ndxListURL = "https://yfiua.github.io/index-constituents/constituents-nasdaq100.json"

type ndxMember struct {
	Symbol string `json:"Symbol"`
	Name   string `json:"Name"`
}

// FetchNDXMembers 拉取纳指100成分（Symbol 为 Yahoo 风格代码）。
// 转成东财 secid：绝大多数在纳斯达克(105)，BRK.B/BF.B 这类 B 股后缀转点号后仍 105。
func FetchNDXMembers(ctx context.Context) ([]model.Stock, error) {
	body, err := getJSONRaw(ctx, ndxListURL)
	if err != nil {
		return nil, fmt.Errorf("NDX 成分拉取失败: %w", err)
	}
	var members []ndxMember
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, fmt.Errorf("NDX 成分解析失败: %w", err)
	}
	out := make([]model.Stock, 0, len(members))
	for _, m := range members {
		if m.Symbol == "" {
			continue
		}
		symbol := normalizeUSSymbol(m.Symbol)
		out = append(out, model.Stock{
			SecID: "105." + symbol,
			Code:  symbol,
			Name:  m.Name,
		})
	}
	return out, nil
}

// normalizeUSSymbol Yahoo "BRK.B" → 东财 "BRK.B"（东财同用点号，实测 AAPL 直通）。
func normalizeUSSymbol(s string) string { return s }

// USSectorSPDR 美股11个 SPDR 行业 ETF（东财市场 107=NYSE Arca，实测全通）。
// 静态配置：SPDR 行业构成极稳定，无需动态源。
func USSectorSPDR() []model.Stock {
	sectors := []struct{ code, name string }{
		{"XLK", "科技"},
		{"XLF", "金融"},
		{"XLE", "能源"},
		{"XLV", "医疗"},
		{"XLY", "可选消费"},
		{"XLP", "必需消费"},
		{"XLI", "工业"},
		{"XLU", "公用事业"},
		{"XLB", "原材料"},
		{"XLC", "通信服务"},
		{"XLRE", "房地产"},
	}
	out := make([]model.Stock, 0, len(sectors))
	for _, s := range sectors {
		out = append(out, model.Stock{SecID: "107." + s.code, Code: s.code, Name: s.name})
	}
	return out
}

// USIndexSecids 美股三大指数（东财 100 市场，实测通）。
func USIndexSecids() []string {
	return []string{"100.DJIA", "100.NDX", "100.SPX"}
}

// 供 web 层做缓存 TTL 的默认值。
const (
	TTLBoards = 60 * time.Second // 板块：盘中 1 分钟
	TTLStocks = 15 * time.Second // 个股列表：15 秒
	TTLQuotes = 10 * time.Second // 批量行情：10 秒
	TTLNDX    = 6 * time.Hour    // 成分表：6 小时
)
