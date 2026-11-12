package source

// 数据源治理：能力矩阵、功能路由、连通性探测。
//
// 四个数据源（均经实测）：
//   - eastmoney 东财：全功能主源，但 2026-11-12 实测 push2 系列连接失败（降为兜底）
//   - sina      新浪：行情/板块/列表/日K兜底（GBK 编码；无分钟分时、无美股列表）
//   - tencent   腾讯：批量行情首选（A股/美股/ETF/美股指数通吃）；行情/分时/K线已实测待接线
//   - yfiua     GitHub Pages：仅纳指100成分表（月度更新）
//
// 路由（routing）：每个功能可指定首选数据源，失败自动切另一支持源；
// 仅单一来源的功能（分时/美股列表/成分表）锁定不可切换。
// 路由状态存于内存（启动时由 config + DB 设置合并写入），运行期可被 API 热更新。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 功能键。
const (
	FeatQuotes    = "quotes"      // 大盘指数行情
	FeatIntraday  = "intraday"    // 当日分时（1分钟K）
	FeatKline     = "kline"       // 日K线
	FeatBoards    = "boards"      // A股板块
	FeatStocks    = "stocks"      // A股个股列表
	FeatBatch     = "batch"       // 批量行情（自选/纳指/美股板块）
	FeatUSStocks  = "us_stocks"   // 美股活跃榜
	FeatNDXMember = "ndx_members" // 纳指100成分表
)

// 数据源 ID。
const (
	SrcEastmoney = "eastmoney"
	SrcSina      = "sina"
	SrcTencent   = "tencent"
	SrcYfiua     = "yfiua"
)

// FeatureMeta 功能元数据。
type FeatureMeta struct {
	Key     string   `json:"key"`
	Label   string   `json:"label"`
	Sources []string `json:"sources"` // 支持该功能的数据源 ID（按推荐顺序）
}

// SourceMeta 数据源元数据。
type SourceMeta struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Desc     string   `json:"desc"`
	Features []string `json:"features"` // 支持的功能键
}

// Features 全部功能定义（顺序即看板展示顺序；Sources[0] 为默认首选）。
// 默认路由原则（2026-11-12 东财 push2 全挂后调整）：东财降兜底，
// 新浪/腾讯优先——但 quotes 保持东财首选，因 90.BK 板块行情仅东财可报价，
// 换源会导致看门狗板块行情永久缺失。
var Features = []FeatureMeta{
	{FeatQuotes, "大盘指数行情", []string{SrcEastmoney, SrcSina}},
	{FeatIntraday, "当日分时（1分钟K）", []string{SrcEastmoney}},
	{FeatKline, "日K线", []string{SrcSina, SrcEastmoney}},
	{FeatBoards, "A股板块", []string{SrcSina, SrcEastmoney}},
	{FeatStocks, "A股个股列表", []string{SrcSina, SrcEastmoney}},
	{FeatBatch, "批量行情（自选/纳指/美股板块）", []string{SrcTencent, SrcSina, SrcEastmoney}},
	{FeatUSStocks, "美股成交活跃榜", []string{SrcEastmoney}},
	{FeatNDXMember, "纳斯达克100成分表", []string{SrcYfiua}},
}

// Sources 全部数据源定义。
var Sources = []SourceMeta{
	{SrcEastmoney, "东方财富", "push2/push2his 行情接口（功能最全，2026-11-12 实测连接失败，降为兜底）",
		[]string{FeatQuotes, FeatIntraday, FeatKline, FeatBoards, FeatStocks, FeatBatch, FeatUSStocks}},
	{SrcSina, "新浪财经", "hq/Market_Center/板块接口（GBK 编码，行情/日K/板块/列表首选）",
		[]string{FeatQuotes, FeatKline, FeatBoards, FeatStocks, FeatBatch}},
	{SrcTencent, "腾讯财经", "qt.gtimg.cn/web.ifzq.gtimg.cn（批量行情首选，A股/美股/ETF/指数通吃；行情/分时/K线已实测待接线）",
		[]string{FeatBatch}},
	{SrcYfiua, "GitHub Pages 成分表", "yfiua/index-constituents（纳指100成分，月度更新）",
		[]string{FeatNDXMember}},
}

// FeatureLabel 按功能键取中文名。
func FeatureLabel(key string) string {
	for _, f := range Features {
		if f.Key == key {
			return f.Label
		}
	}
	return key
}

// ---- 路由状态（热更新）----

var (
	routingMu sync.RWMutex
	routing   = defaultRouting()
)

// defaultRouting 默认路由：全部走东财，成分表走 yfiua。
func defaultRouting() map[string]string {
	m := map[string]string{}
	for _, f := range Features {
		m[f.Key] = f.Sources[0] // 每个功能取推荐首选
	}
	return m
}

// SetRouting 热更新路由（仅接受该功能支持的源，非法键忽略）。
func SetRouting(m map[string]string) {
	routingMu.Lock()
	defer routingMu.Unlock()
	for k, v := range m {
		if _, ok := routing[k]; ok && sourceSupports(k, v) {
			routing[k] = v
		}
	}
}

// GetRouting 查询某功能当前路由的数据源（无配置返回默认）。
func GetRouting(feature string) string {
	routingMu.RLock()
	defer routingMu.RUnlock()
	if s, ok := routing[feature]; ok {
		return s
	}
	return SrcEastmoney
}

// RoutingSnapshot 返回完整路由快照。
func RoutingSnapshot() map[string]string {
	routingMu.RLock()
	defer routingMu.RUnlock()
	out := make(map[string]string, len(routing))
	for k, v := range routing {
		out[k] = v
	}
	return out
}

// sourceSupports 判断数据源是否支持功能。
func sourceSupports(feature, src string) bool {
	for _, f := range Features {
		if f.Key == feature {
			for _, s := range f.Sources {
				if s == src {
					return true
				}
			}
			return false
		}
	}
	return false
}

// Supports 对外暴露的能力查询。
func Supports(feature, src string) bool { return sourceSupports(feature, src) }

// pairFor 返回某功能的尝试顺序（首选 + 备选，单一来源功能只有一个）。
func pairFor(feature string) []string {
	primary := GetRouting(feature)
	for _, f := range Features {
		if f.Key == feature {
			for _, s := range f.Sources {
				if s == primary {
					// 首选在前，其余在后
					order := []string{s}
					for _, alt := range f.Sources {
						if alt != s {
							order = append(order, alt)
						}
					}
					return order
				}
			}
			// 路由值非法（理论上不会发生）→ 用默认顺序
			return f.Sources
		}
	}
	return []string{SrcEastmoney}
}

// ---- 连通性探测 ----

// ProbeResult 单个数据源的探测结果。
type ProbeResult struct {
	OK        bool      `json:"ok"`
	LatencyMs int64     `json:"latency_ms"`
	Error     string    `json:"error,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// 探测端点（轻量、免缓存，2026-11-11/12 实测）。
const (
	probeEastmoney = "https://push2.eastmoney.com/api/qt/ulist.np/get?fltt=2&secids=1.000001&fields=f2,f12,f14"
	probeSina      = "https://hq.sinajs.cn/list=s_sh000001"
	probeTencent   = "https://qt.gtimg.cn/q=s_sh000001"
	probeYfiua     = "https://yfiua.github.io/index-constituents/constituents-nasdaq100.json"
)

func probeGet(ctx context.Context, url, referer string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// ProbeOne 探测单个数据源，返回延迟与错误。
func ProbeOne(ctx context.Context, srcID string) ProbeResult {
	start := time.Now()
	var err error
	switch srcID {
	case SrcEastmoney:
		var body []byte
		body, err = probeGet(ctx, probeEastmoney, "https://quote.eastmoney.com/")
		if err == nil && !json.Valid(body) {
			err = fmt.Errorf("响应非 JSON")
		}
	case SrcSina:
		var body []byte
		body, err = probeGet(ctx, probeSina, "https://finance.sina.com.cn")
		if err == nil && !strings.Contains(string(body), `hq_str_s_sh000001="`) {
			err = fmt.Errorf("响应缺少行情字段")
		}
	case SrcTencent:
		var body []byte
		body, err = probeGet(ctx, probeTencent, "")
		if err == nil && !strings.Contains(string(body), `v_s_sh000001="`) {
			err = fmt.Errorf("响应缺少行情字段")
		}
	case SrcYfiua:
		var body []byte
		body, err = probeGet(ctx, probeYfiua, "")
		if err == nil {
			var arr []map[string]any
			if e := json.Unmarshal(body, &arr); e != nil || len(arr) == 0 {
				err = fmt.Errorf("成分表解析失败或为空")
			}
		}
	default:
		err = fmt.Errorf("未知数据源: %s", srcID)
	}
	res := ProbeResult{OK: err == nil, LatencyMs: time.Since(start).Milliseconds(), CheckedAt: time.Now()}
	if err != nil {
		res.Error = err.Error()
	}
	return res
}

// ProbeAll 并行探测全部数据源。
func ProbeAll(ctx context.Context) map[string]ProbeResult {
	out := make(map[string]ProbeResult, len(Sources))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, s := range Sources {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			r := ProbeOne(ctx, id)
			mu.Lock()
			out[id] = r
			mu.Unlock()
		}(s.ID)
	}
	wg.Wait()
	return out
}
