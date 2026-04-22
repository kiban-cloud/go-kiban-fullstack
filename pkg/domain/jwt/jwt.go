package jwt

type TokenClaims struct {
	Subject  string `json:"subject"`
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}
