// 实盘交易通道适配层：下单/撤单统一走 Broker 接口，
// 模拟盘与未来实盘（QMT/Ptrade 等）在同一处切换，业务代码不感知后端差异。
//
// 模式由 config trading.mode 控制：
//   - sim（默认）：SimBroker，挂现有模拟撮合引擎
//   - live：LiveBroker，需已开通券商量化 API；未接入时下单直接拒绝并给出原因
package trading

import (
	"context"
	"fmt"

	"killer/internal/model"
)

// Broker 交易通道：下单、撤单。行情/账户等本地数据仍由 Engine 提供。
type Broker interface {
	// Name 通道名（sim / qmt …），界面徽标与日志用。
	Name() string
	// Live 是否实盘通道（界面警示与二次确认用）。
	Live() bool
	// PlaceOrder 委托下单，返回订单 ID 与状态。
	PlaceOrder(ctx context.Context, req model.Order) (int64, string, error)
	// CancelOrder 撤销挂单。
	CancelOrder(orderID int64) error
}

// SimBroker 模拟盘通道：全部委托现有模拟撮合引擎。
type SimBroker struct {
	eng *Engine
}

// NewSimBroker 构造模拟盘通道。
func NewSimBroker(eng *Engine) *SimBroker { return &SimBroker{eng: eng} }

// Name 通道名。
func (b *SimBroker) Name() string { return "sim" }

// Live 模拟盘非实盘。
func (b *SimBroker) Live() bool { return false }

// PlaceOrder 转发模拟引擎下单。
func (b *SimBroker) PlaceOrder(ctx context.Context, req model.Order) (int64, string, error) {
	return b.eng.PlaceOrder(ctx, req)
}

// CancelOrder 转发模拟引擎撤单。
func (b *SimBroker) CancelOrder(orderID int64) error { return b.eng.CancelOrder(orderID) }

// LiveBroker 实盘通道。当前为接口占位：券商 API（QMT/Ptrade 等）尚未开通，
// 所有下单/撤单一律拒绝，防止误以为已接入真实市场。
// 开通后在此实现真实委托（配置 account/server 等参数），业务层无需改动。
type LiveBroker struct {
	BrokerName string // 实盘通道名，如 "qmt"；未配置时用 "live"
}

// Name 通道名。
func (b *LiveBroker) Name() string {
	if b.BrokerName != "" {
		return b.BrokerName
	}
	return "live"
}

// Live 实盘通道。
func (b *LiveBroker) Live() bool { return true }

// PlaceOrder 未接入，拒绝下单。
func (b *LiveBroker) PlaceOrder(ctx context.Context, req model.Order) (int64, string, error) {
	return 0, "", fmt.Errorf("实盘通道未开通：券商 API（QMT/Ptrade）尚未接入，本单未提交。请将 trading.mode 配为 sim，或开通券商量化 API 后由开发接入 %s 通道", b.Name())
}

// CancelOrder 未接入，拒绝撤单。
func (b *LiveBroker) CancelOrder(orderID int64) error {
	return fmt.Errorf("实盘通道未开通（%s），无单可撤", b.Name())
}

// NewBroker 按模式构造交易通道。mode 非 live 一律回退模拟盘。
func NewBroker(mode string, eng *Engine) Broker {
	if mode == "live" {
		return &LiveBroker{}
	}
	return NewSimBroker(eng)
}
