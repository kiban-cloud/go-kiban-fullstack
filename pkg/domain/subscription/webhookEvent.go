package subscription

import "time"

type PlanStatus string

const (
	PLAN_STATUS_ACTIVE   PlanStatus = "active"
	PLAN_STATUS_PAST_DUE PlanStatus = "past_due"
	PLAN_STATUS_CANCELED PlanStatus = "canceled"
	PLAN_STATUS_TRIALING PlanStatus = "trialing"
	PLAN_STATUS_UNPAID   PlanStatus = "unpaid"
)

type Action string

const (
	SUBSCRIPTION_CREATED                  Action = "SUBSCRIPTION_CREATED"
	SUBSCRIPTION_UPDATED                  Action = "SUBSCRIPTION_UPDATED"
	SUBSCRIPTION_DELETED                  Action = "SUBSCRIPTION_DELETED"
	SUBSCRIPTION_UNDO_CANCELLATION_AT_END Action = "SUBSCRIPTION_UNDO_CANCELLATION_AT_END"
	SUBSCRIPTION_CANCELLED_AT_END         Action = "SUBSCRIPTION_CANCELLED_AT_END"
	PAYMENT_SUCCEEDED                     Action = "PAYMENT_SUCCEEDED"
	PAYMENT_FAILED                        Action = "PAYMENT_FAILED"
)

type SubscriptionChangedEvent struct {
	TenantIDVal    string
	SubscriptionID string
	Action         Action
	Status         string
	RawDataVal     any
	PriceId        string
	PeriodStart    *time.Time
	PeriodEnd      *time.Time
}

func (e SubscriptionChangedEvent) TenantID() string         { return e.TenantIDVal }
func (e SubscriptionChangedEvent) RawData() any             { return e.RawDataVal }
func (e SubscriptionChangedEvent) StripeCustomerID() string { return "" }

type InvoiceChangedEvent struct {
	StripeCustomerIDVal string
	InvoiceID           string
	Action              Action
	AmountDue           int64
	SubscriptionID      string
	HasSubscription     bool
	RawDataVal          any
}

func (e InvoiceChangedEvent) TenantID() string         { return "" }
func (e InvoiceChangedEvent) StripeCustomerID() string { return e.StripeCustomerIDVal }
func (e InvoiceChangedEvent) RawData() any             { return e.RawDataVal }
