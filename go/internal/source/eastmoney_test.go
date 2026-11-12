package source

import (
	"context"
	"strings"
	"testing"
)

// TestSecidOf 验证 secid 拼装覆盖全部市场号：
// 0/1=沪深，90=板块（BK 代码），105/106/107=美股。
func TestSecidOf(t *testing.T) {
	cases := []struct {
		market int
		code   string
		want   string
	}{
		{1, "000001", "1.000001"},
		{0, "399001", "0.399001"},
		{90, "BK1128", "90.BK1128"},
		{90, "BK1326", "90.BK1326"},
		{105, "AAPL", "105.AAPL"},
	}
	for _, c := range cases {
		if got := secidOf(c.market, c.code); got != c.want {
			t.Errorf("secidOf(%d, %q)=%q want %q", c.market, c.code, got, c.want)
		}
	}
}

// TestFetchBoardMembersInvalidSecid 非板块 secid 在发起网络请求前即被拒绝。
func TestFetchBoardMembersInvalidSecid(t *testing.T) {
	for _, secid := range []string{"1.000001", "0.399001", "105.AAPL", "BK1128", ""} {
		_, err := FetchBoardMembers(context.Background(), secid)
		if err == nil {
			t.Errorf("secid=%q 应返回错误", secid)
		} else if !strings.Contains(err.Error(), "90.BK") {
			t.Errorf("secid=%q 错误信息应提示 90.BKxxxx: %v", secid, err)
		}
	}
}
