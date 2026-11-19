package trading

import (
	"context"
	"strings"
	"testing"

	"killer/internal/model"
)

// TestNewBroker 模式路由：sim（含空/未知值）→ 模拟盘，live → 实盘占位。
func TestNewBroker(t *testing.T) {
	eng := &Engine{}
	for _, mode := range []string{"", "sim", "other"} {
		b := NewBroker(mode, eng)
		if b.Live() || b.Name() != "sim" {
			t.Errorf("mode=%q 应构造模拟盘通道，got live=%v name=%s", mode, b.Live(), b.Name())
		}
	}
	b := NewBroker("live", eng)
	if !b.Live() {
		t.Error("mode=live 应构造实盘通道")
	}
}

// TestLiveBrokerRejects 实盘通道未接入时，下单/撤单必须明确拒绝（防误以为已接真实市场）。
func TestLiveBrokerRejects(t *testing.T) {
	b := &LiveBroker{}
	_, _, err := b.PlaceOrder(context.Background(), model.Order{
		Side: model.SideBuy, Quantity: 100, Price: 10,
	})
	if err == nil {
		t.Fatal("未接入的实盘通道下单应报错")
	}
	for _, kw := range []string{"未开通", "QMT", "sim"} {
		if !strings.Contains(err.Error(), kw) {
			t.Errorf("拒绝原因应含 %q，got: %v", kw, err)
		}
	}
	if err := b.CancelOrder(1); err == nil {
		t.Error("未接入的实盘通道撤单应报错")
	}
}

// TestSimBrokerDelegates 模拟盘通道委托引擎（nil 引擎应 panic 在引擎内而非被通道吞掉——
// 这里用带 store 的最小引擎验证转发路径返回业务错误而非通道层拦截）。
func TestSimBrokerDelegates(t *testing.T) {
	b := NewSimBroker(&Engine{})
	// 引擎未初始化 store：转发应到达引擎并返回其错误，而不是通道直接拒绝
	_, _, err := b.PlaceOrder(context.Background(), model.Order{Side: "bad", Quantity: 100})
	if err == nil {
		t.Fatal("非法方向应由引擎拒绝")
	}
	if strings.Contains(err.Error(), "通道") {
		t.Errorf("错误应来自引擎业务校验，got: %v", err)
	}
}
