package source

// ETF 持仓明细（成分股）：东财天天基金 F10 jjcc 接口。
// 2026-11-16 实测：FundArchivesDatas.aspx?type=jjcc 返回 GBK 编码的 JS
// （var apidata={ content:"<div class='boxitem'>…前十大重仓表格…"}），
// 行结构 <tr><td>序号</td><td><a href='…unify/r/0.300308'>300308</a></td>
// <td class='tol'><a>名称</a></td>…<td class='tor'>3.56%</td>（占净值比）
// <td class='tor'>470.63</td>（万股）<td class='tor'>380,479.67</td>（万元）</tr>。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"killer/internal/model"
)

// ETF 持仓披露期标签（如 "2026年3季度股票投资明细"）。
type ETFHoldings struct {
	Period string         `json:"period"`
	Items  []model.Holding `json:"items"`
}

var (
	// 持仓行：序号 + 统一行情链接代码 + 名称 + 尾部三列（占比/万股/万元）
	etfHoldRowRe = regexp.MustCompile(
		`<tr><td>(\d+)</td><td><a href='//quote\.eastmoney\.com/unify/r/\d\.(\d{6})'>\d{6}</a></td>` +
			`<td class='tol'><a[^>]*>([^<]+)</a></td>(?s:.*?)` +
			`<td class='tor'>([\d.]+)%</td><td class='tor'>([\d.,]+)</td><td class='tor'>([\d.,]+)</td></tr>`)
	etfHoldPeriodRe = regexp.MustCompile(`<label class='left'>(?s:.*?)</a>&nbsp;&nbsp;([^<]+)</label>`)
)

// FetchETFHoldings 拉取基金最新披露期的前十大重仓股（东财天天基金 F10）。
// code 为 6 位基金代码（ETF 主代码）。返回披露期标签与持仓行。
func FetchETFHoldings(ctx context.Context, code string) (*ETFHoldings, error) {
	if len(code) != 6 {
		return nil, fmt.Errorf("非法基金代码: %s", code)
	}
	url := fmt.Sprintf("http://fundf10.eastmoney.com/FundArchivesDatas.aspx?type=jjcc&code=%s&topline=10&year=&month=", code)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Referer", "http://fundf10.eastmoney.com/")
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jjcc 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jjcc HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("jjcc 读取失败: %w", err)
	}
	// 2026-11-16 实测 jjcc 为 UTF-8（历史认知 GBK 有误）；仅在非法 UTF-8 时按 GBK 兜底
	text := string(raw)
	if !utf8.Valid(raw) {
		text = string(gbkToUTF8(raw))
	}
	return parseETFHoldings(text)
}

// parseETFHoldings 从 jjcc 响应文本解析持仓（content:"…" JS 字符串 → 首个报告期）。
func parseETFHoldings(text string) (*ETFHoldings, error) {
	content := extractJSString(text, `content:"`)
	if content == "" {
		return nil, fmt.Errorf("jjcc 响应缺少 content 字段")
	}
	// 多个报告期时只取第一个 boxitem（最新披露期）
	if i := strings.Index(content, "boxitem"); i >= 0 {
		if j := strings.Index(content[i+1:], "boxitem"); j >= 0 {
			content = content[:i+1+j]
		}
	}
	out := &ETFHoldings{Period: "", Items: []model.Holding{}}
	if m := etfHoldPeriodRe.FindStringSubmatch(content); m != nil {
		out.Period = strings.TrimSpace(m[1])
	}
	for _, m := range etfHoldRowRe.FindAllStringSubmatch(content, -1) {
		rank, _ := strconv.Atoi(m[1])
		out.Items = append(out.Items, model.Holding{
			Rank:   rank,
			Code:   m[2],
			Name:   m[3],
			Pct:    parseFloat(m[4]),
			Shares: parseFloat(strings.ReplaceAll(m[5], ",", "")),
			Value:  parseFloat(strings.ReplaceAll(m[6], ",", "")),
		})
	}
	if len(out.Items) == 0 {
		return nil, fmt.Errorf("jjcc 未解析到持仓行（可能非股票型基金或接口变更）")
	}
	return out, nil
}

// extractJSString 提取 JS 赋值字符串（处理 \" 转义），key 形如 content:"。
func extractJSString(text, key string) string {
	i := strings.Index(text, key)
	if i < 0 {
		return ""
	}
	s := i + len(key)
	var b strings.Builder
	for j := s; j < len(text); j++ {
		switch text[j] {
		case '\\':
			if j+1 < len(text) {
				b.WriteByte(text[j+1])
				j++
			}
		case '"':
			return b.String()
		default:
			b.WriteByte(text[j])
		}
	}
	return ""
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
