package source

// F10 公司基本面数据（东财，2026-11-17 三接口实测全通）：
//   - 公司概况: emweb.securities.eastmoney.com/PC_HSF10/CompanySurvey/PageAjax?code=SH600519
//   - 主营构成: datacenter RPT_F10_FN_MAINOP（最新报告期，产品/地区收入结构）
//   - 财报指标: datacenter RPT_F10_FINANCE_MAINFINADATA（最近 4 期主要指标）
//
// 基本面按季度变化，web 层做 30 分钟缓存；三接口全成功才返回，
// 任一失败整体报错（不缓存残缺结果，东财限流期前端走降级占位）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"killer/internal/model"
)

const (
	emCompanyURL    = "https://emweb.securities.eastmoney.com/PC_HSF10/CompanySurvey/PageAjax?code=%s"
	emF10DataURL    = "https://datacenter.eastmoney.com/securities/api/data/v1/get?reportName=%s&columns=ALL&filter=(SECUCODE%%3D%%22%s%%22)&pageSize=%d&sortColumns=REPORT_DATE&sortTypes=-1&source=HSF10&client=PC"
	f10MainOpRows   = 60 // 最新期产品+地区条目总量上限（实测单期 < 20 条）
	f10FinanceRows  = 4  // 财报指标取最近 4 期
	f10FinancePages = "RPT_F10_FINANCE_MAINFINADATA"
	f10MainOpName   = "RPT_F10_FN_MAINOP"
)

// f10Codes secid → 东财 F10 两种编码：
// emCode（SH600519，公司概况接口用）、secuCode（600519.SH，datacenter 接口用）。
// 口径与 isAShareSecid 一致：仅沪深北 A 股（指数/ETF/B股无 F10 数据）。
func f10Codes(secid string) (emCode, secuCode string, ok bool) {
	if !isAShareSecid(secid) {
		return "", "", false
	}
	market, code, _ := strings.Cut(secid, ".")
	if market == "1" {
		return "SH" + code, code + ".SH", true
	}
	if strings.HasPrefix(code, "92") { // 北交所
		return "BJ" + code, code + ".BJ", true
	}
	return "SZ" + code, code + ".SZ", true
}

// FetchF10 拉取个股公司基本面（概况 + 主营构成 + 财报指标，三接口并发）。
// 非 A 股 secid 直接报错。
func FetchF10(ctx context.Context, secid string) (*model.F10, error) {
	emCode, secuCode, ok := f10Codes(secid)
	if !ok {
		return nil, fmt.Errorf("F10 仅支持沪深北 A 股 secid: %s", secid)
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		company  f10CompanyParsed
		ops      []model.F10MainOp
		opPeriod string
		finance  []model.F10Finance
		errs     []error
	)
	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}()
	}
	run(func() error {
		c, err := fetchF10Company(ctx, emCode)
		if err != nil {
			return fmt.Errorf("公司概况: %w", err)
		}
		company = c
		return nil
	})
	run(func() error {
		p, o, err := fetchF10MainOps(ctx, secuCode)
		if err != nil {
			return fmt.Errorf("主营构成: %w", err)
		}
		opPeriod, ops = p, o
		return nil
	})
	run(func() error {
		f, err := fetchF10Finance(ctx, secuCode)
		if err != nil {
			return fmt.Errorf("财报指标: %w", err)
		}
		finance = f
		return nil
	})
	wg.Wait()
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return nil, fmt.Errorf("F10 抓取失败: %s", strings.Join(msgs, "; "))
	}
	return &model.F10{
		OrgName:       company.orgName,
		Industry:      company.industry,
		Profile:       strings.TrimSpace(company.profile),
		BusinessScope: strings.TrimSpace(company.scope),
		MainOpPeriod:  opPeriod,
		MainOps:       ops,
		Finance:       finance,
	}, nil
}

// ---- 公司概况 ----

type f10CompanyParsed struct {
	orgName, industry, profile, scope string
}

func fetchF10Company(ctx context.Context, emCode string) (f10CompanyParsed, error) {
	body, err := getJSONRaw(ctx, fmt.Sprintf(emCompanyURL, emCode))
	if err != nil {
		return f10CompanyParsed{}, fmt.Errorf("请求失败: %w", err)
	}
	return parseF10Company(body)
}

func parseF10Company(body []byte) (f10CompanyParsed, error) {
	var root struct {
		Jbzl []struct {
			OrgName       string `json:"ORG_NAME"`
			Industry      string `json:"EM2016"`
			OrgProfile    string `json:"ORG_PROFILE"`
			BusinessScope string `json:"BUSINESS_SCOPE"`
		} `json:"jbzl"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return f10CompanyParsed{}, fmt.Errorf("解析失败: %w", err)
	}
	if len(root.Jbzl) == 0 {
		return f10CompanyParsed{}, fmt.Errorf("无公司概况数据")
	}
	j := root.Jbzl[0]
	return f10CompanyParsed{orgName: j.OrgName, industry: j.Industry, profile: j.OrgProfile, scope: j.BusinessScope}, nil
}

// ---- 主营构成 ----

// fetchF10MainOps 拉取主营构成，只保留最新报告期的条目。
// 返回 (报告期名, 条目列表)。
func fetchF10MainOps(ctx context.Context, secuCode string) (string, []model.F10MainOp, error) {
	body, err := getJSONRaw(ctx, fmt.Sprintf(emF10DataURL, f10MainOpName, secuCode, f10MainOpRows))
	if err != nil {
		return "", nil, fmt.Errorf("请求失败: %w", err)
	}
	return parseF10MainOps(body)
}

func parseF10MainOps(body []byte) (string, []model.F10MainOp, error) {
	var root struct {
		Result struct {
			Data []struct {
				MainOpType         string   `json:"MAINOP_TYPE"`          // 1行业 2产品 3地区
				ItemName           string   `json:"ITEM_NAME"`            // 项目名
				MainBusinessIncome float64  `json:"MAIN_BUSINESS_INCOME"` // 主营收入（元）
				MbiRatio           float64  `json:"MBI_RATIO"`            // 收入占比（小数）
				GrossProfitRatio   *float64 `json:"GROSS_RPOFIT_RATIO"`   // 毛利率（小数，可能 null）
				ReportName         string   `json:"REPORT_NAME"`          // 报告期名（2026三季报）
				ReportDate         string   `json:"REPORT_DATE"`
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return "", nil, fmt.Errorf("解析失败: %w", err)
	}
	rows := root.Result.Data
	if len(rows) == 0 {
		return "", nil, nil // 无披露（新股等），不算错误
	}
	latest := rows[0].ReportDate // 已按 REPORT_DATE 倒序
	kindOf := map[string]string{"1": "行业", "2": "产品", "3": "地区"}
	out := make([]model.F10MainOp, 0, len(rows))
	for _, r := range rows {
		if r.ReportDate != latest {
			break
		}
		op := model.F10MainOp{
			Kind:   kindOf[r.MainOpType],
			Name:   r.ItemName,
			Income: r.MainBusinessIncome,
			Ratio:  r.MbiRatio * 100,
		}
		if r.GrossProfitRatio != nil {
			op.Margin = *r.GrossProfitRatio * 100
		}
		out = append(out, op)
	}
	return rows[0].ReportName, out, nil
}

// ---- 财报指标 ----

func fetchF10Finance(ctx context.Context, secuCode string) ([]model.F10Finance, error) {
	body, err := getJSONRaw(ctx, fmt.Sprintf(emF10DataURL, f10FinancePages, secuCode, f10FinanceRows))
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	return parseF10Finance(body)
}

func parseF10Finance(body []byte) ([]model.F10Finance, error) {
	var root struct {
		Result struct {
			Data []struct {
				Period      string  `json:"REPORT_DATE_NAME"`   // 2026三季报
				Revenue     float64 `json:"TOTALOPERATEREVE"`   // 营业总收入（元）
				RevenueYoy  float64 `json:"TOTALOPERATEREVETZ"` // 营收同比（%）
				Profit      float64 `json:"PARENTNETPROFIT"`    // 归母净利（元）
				ProfitYoy   float64 `json:"PARENTNETPROFITTZ"`  // 归母净利同比（%）
				Deduct      float64 `json:"KCFJCXSYJLR"`        // 扣非净利（元）
				DeductYoy   float64 `json:"KCFJCXSYJLRTZ"`      // 扣非净利同比（%）
				Margin      float64 `json:"XSMLL"`              // 销售毛利率（%）
				NetMargin   float64 `json:"XSJLL"`              // 销售净利率（%）
				RoE         float64 `json:"ROEJQ"`              // 加权 ROE（%）
				EPS         float64 `json:"EPSJB"`              // 基本每股收益（元）
				Bps         float64 `json:"BPS"`                // 每股净资产（元）
				DebtRatio   float64 `json:"ZCFZL"`              // 资产负债率（%）
				OperateCash float64 `json:"NETCASH_OPERATE_PK"` // 经营现金流净额（元）
				TotalAssets float64 `json:"TOTAL_ASSETS_PK"`    // 总资产（元）
			} `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("解析失败: %w", err)
	}
	rows := root.Result.Data
	if len(rows) == 0 {
		return nil, fmt.Errorf("无财报数据")
	}
	out := make([]model.F10Finance, 0, len(rows))
	for _, r := range rows {
		out = append(out, model.F10Finance{
			Period: r.Period, Revenue: r.Revenue, RevenueYoy: r.RevenueYoy,
			Profit: r.Profit, ProfitYoy: r.ProfitYoy,
			Deduct: r.Deduct, DeductYoy: r.DeductYoy,
			Margin: r.Margin, NetMargin: r.NetMargin, RoE: r.RoE,
			EPS: r.EPS, Bps: r.Bps, DebtRatio: r.DebtRatio,
			OperateCash: r.OperateCash, TotalAssets: r.TotalAssets,
		})
	}
	return out, nil
}
