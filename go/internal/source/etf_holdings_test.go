package source

import "testing"

// jjcc 真实响应结构样本（2026-11-16 实测 510300 抓取后裁剪，单行 JS）。
// content 值内不含双引号（HTML 属性用单引号，与线上一致）。
const jjccSample = "var apidata={ content:\"<div class='box'>" +
	"<div class='boxitem w790'><h4 class='t'><label class='left'>" +
	"<a title='沪深300ETF华泰柏瑞' href='http://fund.eastmoney.com/510300.html'>沪深300ETF华泰柏瑞</a>" +
	"&nbsp;&nbsp;2026年3季度股票投资明细</label></h4><table><tbody>" +
	"<tr><td>1</td><td><a href='//quote.eastmoney.com/unify/r/0.300308'>300308</a></td>" +
	"<td class='tol'><a href='//quote.eastmoney.com/unify/r/0.300308'>中际旭创</a></td>" +
	"<td class='tor'><span data-id='dq300308'></span></td><td class='tor'><span data-id='zd300308'></span></td>" +
	"<td class='xglj'><a href='ccbdxq_510300_300308.html' class='red'>变动详情</a></td>" +
	"<td class='tor'>3.56%</td><td class='tor'>470.63</td><td class='tor'>380,479.67</td></tr>" +
	"<tr><td>2</td><td><a href='//quote.eastmoney.com/unify/r/1.600519'>600519</a></td>" +
	"<td class='tol'><a href='//quote.eastmoney.com/unify/r/1.600519'>贵州茅台</a></td>" +
	"<td class='tor'><span data-id='dq600519'></span></td><td class='tor'><span data-id='zd600519'></span></td>" +
	"<td class='xglj'></td><td class='tor'>3.09%</td><td class='tor'>200.10</td><td class='tor'>30,000.00</td></tr>" +
	"</tbody></table></div>" +
	"<div class='boxitem w790'><h4 class='t'><label class='left'><a>x</a>&nbsp;&nbsp;2026年2季度股票投资明细</label></h4>" +
	"<table><tbody><tr><td>1</td><td><a href='//quote.eastmoney.com/unify/r/0.000001'>000001</a></td>" +
	"<td class='tol'><a>平安银行</a></td><td class='tor'><span></span></td><td class='tor'><span></span></td>" +
	"<td class='xglj'></td><td class='tor'>9.99%</td><td class='tor'>1.00</td><td class='tor'>1.00</td></tr>" +
	"</tbody></table></div></div>\", arryear:[2026,2025], curyear:2026};"

func TestParseETFHoldings(t *testing.T) {
	h, err := parseETFHoldings(jjccSample)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if h.Period != "2026年3季度股票投资明细" {
		t.Errorf("period = %q", h.Period)
	}
	if len(h.Items) != 2 {
		t.Fatalf("items = %d, want 2（应只取首个报告期）", len(h.Items))
	}
	a := h.Items[0]
	if a.Rank != 1 || a.Code != "300308" || a.Name != "中际旭创" {
		t.Errorf("row1 = %+v", a)
	}
	if a.Pct != 3.56 || a.Shares != 470.63 || a.Value != 380479.67 {
		t.Errorf("row1 numbers = pct %v shares %v value %v", a.Pct, a.Shares, a.Value)
	}
	if b := h.Items[1]; b.Code != "600519" || b.Name != "贵州茅台" || b.Value != 30000 {
		t.Errorf("row2 = %+v", b)
	}
}

func TestParseETFHoldings_NoContent(t *testing.T) {
	if _, err := parseETFHoldings(`var apidata={ arryear:[]};`); err == nil {
		t.Fatal("expect error for missing content")
	}
}

func TestParseETFHoldings_NoRows(t *testing.T) {
	if _, err := parseETFHoldings(`var apidata={ content:"<div class='boxitem'>no table</div>", arryear:[]};`); err == nil {
		t.Fatal("expect error for zero rows")
	}
}
