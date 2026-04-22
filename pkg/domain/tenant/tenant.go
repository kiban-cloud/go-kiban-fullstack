package tenant

import "time"

// Base contains the tenant fields shared across all kiban backend projects.
// Embed it in a project-specific Tenant struct and add the extra fields your
// project needs (tokens, contract, usage, referral, etc.).
//
// CurrentPlan and PlanStatus are stored as raw strings so the shared lib
// doesn't couple to project-specific plan catalogs or the Stripe SDK. Cast
// to your project's typed enums (e.g. plan.Plan, stripe.SubscriptionStatus)
// at the boundary.
type Base struct {
	ID        string     `bson:"_id" json:"id"`
	OwnerID   string     `bson:"ownerId" json:"ownerId"`
	CreatedAt *time.Time `bson:"createdAt,omitempty" json:"createdAt,omitempty"`
	UpdatedAt *time.Time `bson:"updatedAt,omitempty" json:"updatedAt,omitempty"`

	StripeCustomerID     string `bson:"stripeCustomerID,omitempty" json:"stripeCustomerID,omitempty"`
	StripeSubscriptionID string `bson:"stripeSubscriptionID,omitempty" json:"stripeSubscriptionID,omitempty"`

	CurrentPlan string `bson:"currentPlan,omitempty" json:"currentPlan,omitempty"`
	PlanStatus  string `bson:"planStatus,omitempty" json:"planStatus,omitempty"`

	SubscriptionStartDate *time.Time `bson:"subscriptionStartDate,omitempty" json:"subscriptionStartDate,omitempty"`
	CurrentPeriodStart    *time.Time `bson:"currentPeriodStart,omitempty" json:"currentPeriodStart,omitempty"`
	CurrentPeriodEnd      *time.Time `bson:"currentPeriodEnd,omitempty" json:"currentPeriodEnd,omitempty"`
	LastPaymentAt         *time.Time `bson:"lastPaymentAt,omitempty" json:"lastPaymentAt,omitempty"`

	ScheduledPlanChange *ScheduledPlanChange `bson:"scheduledPlanChange,omitempty" json:"scheduledPlanChange,omitempty"`
}

type ScheduledPlanChange struct {
	NewPlan     string     `bson:"newPlan,omitempty" json:"newPlan,omitempty"`
	ScheduledAt *time.Time `bson:"scheduledAt,omitempty" json:"scheduledAt,omitempty"`
}

// NewBase returns a Base populated with ID/OwnerID and CreatedAt/UpdatedAt
// set to now. Projects should call this then fill in their own fields.
func NewBase(tenantID, ownerID string) Base {
	now := time.Now()
	return Base{
		ID:        tenantID,
		OwnerID:   ownerID,
		CreatedAt: &now,
		UpdatedAt: &now,
	}
}
