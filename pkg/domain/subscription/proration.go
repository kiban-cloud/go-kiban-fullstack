package subscription

import "time"

type ProrationPreview struct {
	ProrationTime time.Time       `json:"prorationTime"`
	AmountDue     int64           `json:"amountDue"`
	Lines         []ProrationLine `json:"lines"`
	Currency      string          `json:"currency"`
}

type ProrationLine struct {
	Description string `json:"description"`
	Amount      int64  `json:"amount"`
}
