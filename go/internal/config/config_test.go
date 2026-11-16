package config

import "testing"

// TestGroupMembers 分组 from 引用的展开口径：ETF 组直接复用顶部 etfs，
// 避免同一个 secid 在分析配置里再抄一遍；引用段为空时回退 watchlist，防止分组静默变空。
func TestGroupMembers(t *testing.T) {
	etfs := []Index{{SecID: "1.510300", Name: "沪深300ETF"}}
	cases := []struct {
		name string
		cfg  Config
		grp  AnalysisGroup
		want int
	}{
		{"引用 etfs", Config{ETFs: etfs}, AnalysisGroup{Name: "ETF", From: "etfs"}, 1},
		{"引用段为空则回退", Config{}, AnalysisGroup{
			Name: "ETF", From: "etfs", Watchlist: []Index{{SecID: "1.510300"}}}, 1},
		{"无 from 用 watchlist", Config{ETFs: etfs}, AnalysisGroup{
			Name: "存储", Watchlist: []Index{{SecID: "1.603986"}, {SecID: "0.301308"}}}, 2},
		{"未知 from 用 watchlist", Config{ETFs: etfs}, AnalysisGroup{
			Name: "X", From: "nope", Watchlist: []Index{{SecID: "1.600519"}}}, 1},
	}
	for _, c := range cases {
		if got := c.cfg.GroupMembers(c.grp); len(got) != c.want {
			t.Errorf("%s: 成员数 = %d, want %d", c.name, len(got), c.want)
		}
	}
	if got := (Config{ETFs: etfs}).GroupMembers(AnalysisGroup{From: "etfs"}); len(got) == 0 || got[0].SecID != "1.510300" {
		t.Errorf("引用 etfs 应返回自监控 ETF 列表, got %+v", got)
	}
}

// TestDefaultAnalysisGroups 默认分组必须能被 YAML 覆盖前的空配置区分开，且 ETF 组走引用而非抄代码。
func TestDefaultAnalysisGroups(t *testing.T) {
	c := Default()
	var etfGroup *AnalysisGroup
	for i := range c.Analysis.Groups {
		if c.Analysis.Groups[i].Name == "ETF" {
			etfGroup = &c.Analysis.Groups[i]
		}
	}
	if etfGroup == nil {
		t.Fatal("默认分组缺少 ETF 组")
	}
	if etfGroup.From != "etfs" || len(etfGroup.Watchlist) != 0 {
		t.Errorf("ETF 组应只声明 from: etfs, got from=%q watchlist=%d 条", etfGroup.From, len(etfGroup.Watchlist))
	}
	if n := len(c.GroupMembers(*etfGroup)); n != len(c.ETFs) || n == 0 {
		t.Errorf("ETF 组成员应等于 etfs 段 %d 只, got %d", len(c.ETFs), n)
	}
}
