package adapter

import (
	"errors"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// NormalizeOrderError 将数据库订单错误转换为应用层稳定错误，阻止存储错误类型穿过适配边界。
func NormalizeOrderError(err error) error {
	if errors.Is(err, db.ErrNotFound) {
		return orderapp.ErrNotFound
	}
	if errors.Is(err, db.ErrForbidden) {
		return orderapp.ErrForbidden
	}
	return err
}

// OrderForAutomation 将应用订单转换为尚未迁移完成的自动化中心数据库模型。
func OrderForAutomation(order *orderapp.Order) *db.Order {
	if order == nil {
		return nil
	}
	return &db.Order{
		OrderID: order.OrderID, ItemID: order.ItemID, BuyerID: order.BuyerID,
		SpecName: order.SpecName, SpecValue: order.SpecValue, Quantity: order.Quantity,
		Amount: order.Amount, OrderStatus: order.OrderStatus, CookieID: order.CookieID,
		IsBargain: order.IsBargain, ReceiverName: order.ReceiverName,
		ReceiverPhone: order.ReceiverPhone, ReceiverAddr: order.ReceiverAddress,
		ReceiverCity: order.ReceiverCity, Version: order.Version, ChatID: order.ChatID,
		SystemShipped: order.SystemShipped, PaidAt: order.PaidAt, ShippedAt: order.ShippedAt,
		CompletedAt: order.CompletedAt, BuyerReviewedAt: order.BuyerReviewedAt,
		LastReviewRequestAt: order.LastReviewRequestAt, ReviewRequestCount: order.ReviewRequestCount,
		CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt,
	}
}
