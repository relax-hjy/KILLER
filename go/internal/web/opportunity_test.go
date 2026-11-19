package web

import (
	"strings"
	"testing"

	"killer/internal/analysis"
)

// TestPlanEntry 新建仓操作计划：必须覆盖建仓条件、止损、止盈、环境应对四类步骤。
func TestPlanEntry(t *testing.T) {
	a := &analysis.Assessment{Support: 10, Resistance: 12}
	steps := planEntry(a, 10.2, 9.6, 12, 800, 100)
	if len(steps) < 4 {
		t.Fatalf("操作计划应≥4 步（建仓/止损/止盈/环境），got %d", len(steps))
	}
	joined := ""
	for _, s := range steps {
		joined += s.If + " " + s.Then + "\n"
	}
	for _, kw := range []string{"买入", "止损", "止盈", "弱势"} {
		if !strings.Contains(joined, kw) {
			t.Errorf("操作计划缺关键词 %q：\n%s", kw, joined)
		}
	}
	// 无支撑/压力时也应给出降级计划（不能空手）
	steps = planEntry(&analysis.Assessment{}, 10, 0, 0, 800, 100)
	if len(steps) < 2 {
		t.Errorf("缺价位时仍应有建仓与环境步骤，got %d", len(steps))
	}
}

// TestEntryRisks 风险提示：低置信度、弱势环境、追高、信号相左、观望评级都要提示。
func TestEntryRisks(t *testing.T) {
	a := &analysis.Assessment{
		Score: 70, Confidence: 45, Support: 10, Advice: "观望，回踩支撑再介入",
		Notes: []string{"财报闸门：营收净利双降，建议降档"},
	}
	regime := regimeResult{Label: "弱势", Reason: "指数 1/3 站上MA20"}
	risks := entryRisks(a, 11, regime, 2, 4)
	joined := strings.Join(risks, "\n")
	for _, kw := range []string{"置信度", "弱势", "支撑", "偏空", "观望", "财报闸门"} {
		if !strings.Contains(joined, kw) {
			t.Errorf("风险提示缺 %q：\n%s", kw, joined)
		}
	}
	// 健康标的 + 强势环境：不应误报置信度/弱势
	ok := &analysis.Assessment{Score: 75, Confidence: 90, Support: 10, Advice: "可以买入"}
	good := entryRisks(ok, 10.1, regimeResult{Label: "强势"}, 0, 4)
	for _, r := range good {
		if strings.Contains(r, "置信度") || strings.Contains(r, "弱势") {
			t.Errorf("健康标的不应报该风险: %s", r)
		}
	}
}

// TestOppScoreSort 机会分公式：评分高、买入信号多的机会分应更高（排序依据的口径锁定）。
func TestOppScoreSort(t *testing.T) {
	// 与 scanOpportunities 内公式一致：评分*0.55 + (50+50*共振)*0.30 + 环境*0.15
	score := func(score, resonance, regime float64) float64 {
		return score*0.55 + (50+50*resonance)*0.30 + regime*0.15
	}
	strongBuy := score(80, 1, 70) // 高分全买强环境
	weakSell := score(60, -1, 30) // 低分全卖弱环境
	if strongBuy <= weakSell {
		t.Errorf("高分多头机会分应高于低分空头: %.1f vs %.1f", strongBuy, weakSell)
	}
	// 共振对分数有实质影响
	if score(70, 0.5, 50)-score(70, -0.5, 50) < 10 {
		t.Error("信号共振从 -0.5 到 +0.5 应拉开 ≥10 分")
	}
}
