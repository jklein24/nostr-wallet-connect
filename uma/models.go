package uma

import (
	"github.com/getAlby/nostr-wallet-connect/nip47"
)

type UmaBolt11Invoice struct {
	Amount          int64       `json:"amount"`
	CreatedAt       int64       `json:"created_at"`
	DescriptionHash string      `json:"description_hash"`
	ExpiresAt       *int64      `json:"expires_at"`
	Memo            string      `json:"memo"`
	Metadata        interface{} `json:"metadata"`
	PaymentHash     string      `json:"payment_hash"`
	PaymentRequest  string      `json:"payment_request"`
	Preimage        string      `json:"preimage"`
	SettledAt       *int64      `json:"settled_at:omitempty"`
	Type            string      `json:"type"`
}

func (i UmaBolt11Invoice) ToNip47Transaction() *nip47.Nip47Transaction {
	description := i.Memo
	preimage := i.Preimage

	return &nip47.Nip47Transaction{
		Type:            i.Type,
		Invoice:         i.PaymentRequest,
		Description:     description,
		DescriptionHash: i.DescriptionHash,
		Preimage:        preimage,
		PaymentHash:     i.PaymentHash,
		Amount:          i.Amount,
		FeesPaid:        0, // TODO: support fees
		CreatedAt:       i.CreatedAt,
		ExpiresAt:       i.ExpiresAt,
		SettledAt:       i.SettledAt,
		Metadata:        i.Metadata,
	}
}

type PayRequest struct {
	Invoice string `json:"invoice"`
	Amount  *int64 `json:"amount:omitempty"`
}

type CurrencyBalance struct {
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
	Decimals int    `json:"decimals"`
	Symbol   string `json:"symbol"`
}

type BalanceResponse struct {
	Balances []CurrencyBalance `json:"balances"`
}

type PayResponse struct {
	Preimage    string `json:"payment_preimage"`
	PaymentHash string `json:"payment_hash"`
}

type MakeInvoiceRequest struct {
	Amount          int64  `json:"amount"`
	Description     string `json:"description"`
	DescriptionHash string `json:"description_hash"`
	Expiry          int64  `json:"expiry"`
}
