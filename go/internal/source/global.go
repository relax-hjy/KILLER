package source

// 全球指数 / 大宗商品行情。
//
// 三类标的，三种前缀：
//   - znb_  新浪全球指数（日经225 znb_NKY、纳斯达克100 znb_NDX 等）
//   - hf_   新浪商品期货（纽约原油 hf_CL、纽约黄金 hf_GC）
//   - hk    腾讯港股指数（恒生指数 hkHSI、恒生科技 hkHSTECH），字段与 A股一致
//
// znb_ 字段：名称,现价,涨跌额,涨跌幅,时间,时间戳[,日期,时间,昨收,今开,最高,最低,量]（精简版仅前6个）
// hf_  字段：现价,?,买价,卖价,最高,最低,时间,昨结,昨收,?,?,?,日期,名称,?
import (
	"context"
	"fmt"
	"strings"
	"time"

	"killer/internal/model"
)

// IsHKIndexSecid 判断是否港股指数（腾讯代码，hk 前缀 + 大写字母，如 hkHSI 恒生、hkHSTECH 恒生科技）。
func IsHKIndexSecid(secid string) bool {
	if !strings.HasPrefix(secid, "hk") || len(secid) < 4 {
		return false
	}
	// 第三个字符必须是大写字母（避免误判 hk_ 等其它前缀）
	return secid[2] >= 'A' && secid[2] <= 'Z'
}

// IsGlobalSecid 判断是否全球指数/商品标的（znb_ / hf_ / 港股指数）。
func IsGlobalSecid(secid string) bool {
	return strings.HasPrefix(secid, "znb_") || strings.HasPrefix(secid, "hf_") || IsHKIndexSecid(secid)
}

// FetchGlobalQuotes 批量获取全球指数/商品行情。
// 港股指数走腾讯 qt.gtimg.cn；znb_/hf_ 走新浪 hq.sinajs.cn。
func FetchGlobalQuotes(ctx context.Context, secids []string) ([]model.Quote, error) {
	var sinaSyms, hkSyms []string
	for _, id := range secids {
		if !IsGlobalSecid(id) {
			continue
		}
		if IsHKIndexSecid(id) {
			hkSyms = append(hkSyms, id)
		} else {
			sinaSyms = append(sinaSyms, id)
		}
	}

	var out []model.Quote
	// 港股指数：腾讯批量行情（复用 tencentSymbol 的 hk 透传 + parseTencentStock 字段）
	if len(hkSyms) > 0 {
		if st, err := tencentBatchQuotes(ctx, hkSyms); err == nil {
			for _, s := range st {
				out = append(out, model.Quote{
					SecID:      s.SecID,
					Code:       strings.TrimPrefix(s.SecID, "hk"), // hkHSI → HSI
					Name:       s.Name,
					Price:      s.Price,
					Change:     s.Change,
					Pct:        s.Pct,
					Open:       s.Open,
					High:       s.High,
					Low:        s.Low,
					PrevClose:  s.PrevClose,
					Volume:     s.Amount,
					UpdateTime: s.UpdateTime,
				})
			}
		}
	}

	// 全球指数/商品：新浪 znb_/hf_
	if len(sinaSyms) > 0 {
		body, err := sinaGet(ctx, sinaHQURL+strings.Join(sinaSyms, ","))
		if err == nil {
			sinaQuotes := parseSinaGlobal(string(gbkToUTF8(body)))
			out = append(out, sinaQuotes...)
		}
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("全球行情返回空")
	}
	return out, nil
}

// parseSinaGlobal 解析新浪 znb_/hf_ 全球行情文本。
// znb_ 行：hq_str_znb_NKY="名称,现价,涨跌额,涨跌幅,..."
// hf_  行：hq_str_hf_GC="现价,?,买价,卖价,最高,最低,时间,昨结,昨收,?,?,?,日期,名称,?"
func parseSinaGlobal(text string) []model.Quote {
	now := time.Now()
	var out []model.Quote
	for _, mt := range hqRe.FindAllStringSubmatch(text, -1) {
		sym, payload := mt[1], mt[2]
		if payload == "" {
			continue
		}
		p := strings.Split(payload, ",")
		if strings.HasPrefix(sym, "znb_") {
			// 两种形态：
			//   精简版(6字段)：名称,现价,涨跌额,涨跌幅,时间,时间戳  （纳斯达克100 znb_NDX 等）
			//   完整版(13字段)：名称,现价,涨跌额,涨跌幅,时间,时间戳,日期,时间,昨收,今开,最高,最低,量 （日经225 znb_NKY 等）
			if len(p) < 6 {
				continue
			}
			price, chg := f(p[1]), f(p[2])
			prev := 0.0
			if len(p) >= 13 {
				prev = f(p[8])
			} else if chg != 0 || price != 0 {
				prev = price - chg // 精简版无昨收，用 现价-涨跌额 反推
			}
			open, high, low := 0.0, 0.0, 0.0
			if len(p) >= 13 {
				open, high, low = f(p[9]), f(p[10]), f(p[11])
			}
			out = append(out, model.Quote{
				SecID:      sym,
				Code:       sym,
				Name:       p[0],
				Price:      price,
				Change:     chg,
				Pct:        f(p[3]),
				PrevClose:  prev,
				Open:       open,
				High:       high,
				Low:        low,
				UpdateTime: now,
			})
		} else if strings.HasPrefix(sym, "hf_") {
			// 现价,?,买价,卖价,最高,最低,时间,昨结,昨收,?,?,?,日期,名称,?
			if len(p) < 14 {
				continue
			}
			prev := f(p[8])
			price := f(p[0])
			pct := 0.0
			if prev > 0 {
				pct = (price - prev) / prev * 100
			}
			out = append(out, model.Quote{
				SecID:      sym,
				Code:       sym,
				Name:       p[13],
				Price:      price,
				Change:     price - prev,
				Pct:        pct,
				PrevClose:  prev,
				High:       f(p[4]),
				Low:        f(p[5]),
				UpdateTime: now,
			})
		}
	}
	return out
}
