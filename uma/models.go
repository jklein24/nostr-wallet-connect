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

type Currency struct {
	// Code is the ISO 4217 (if applicable) currency code (eg. "USD"). For cryptocurrencies, this will  be a ticker
	// symbol, such as BTC for Bitcoin.
	Code string `json:"code"`

	// Name is the full display name of the currency (eg. US Dollars).
	Name string `json:"name"`

	// Symbol is the symbol of the currency (eg. $ for USD).
	Symbol string `json:"symbol"`

	// MillisatoshiPerUnit is the estimated millisats per smallest "unit" of this currency (eg. 1 cent in USD).
	MillisatoshiPerUnit float64 `json:"multiplier"`

	// MinSendable is the minimum amount of the currency that can be sent in a single transaction. This is in the
	// smallest unit of the currency (eg. cents for USD).
	MinSendable int64 `json:"min"`

	// MaxSendable is the maximum amount of the currency that can be sent in a single transaction. This is in the
	// smallest unit of the currency (eg. cents for USD).
	MaxSendable int64 `json:"max"`

	// Decimals is the number of digits after the decimal point for display on the sender side, and to add clarity
	// around what the "smallest unit" of the currency is. For example, in USD, by convention, there are 2 digits for
	// cents - $5.95. In this case, `decimals` would be 2. Note that the multiplier is still always in the smallest
	// unit (cents). In addition to display purposes, this field can be used to resolve ambiguity in what the multiplier
	// means. For example, if the currency is "BTC" and the multiplier is 1000, really we're exchanging in SATs, so
	// `decimals` would be 8.
	// For details on edge cases and examples, see https://github.com/uma-universal-money-address/protocol/blob/main/umad-04-lnurlp-response.md.
	Decimals int `json:"decimals"`
}

type LookupUserResponse struct {
	ReceiverCurrencies []Currency `json:"receiverCurrencies"`
}

func (r *LookupUserResponse) ToNip47LookupUserResponse() *nip47.Nip47LookupUserResponse {
	currencies := make([]nip47.Nip47Currency, len(r.ReceiverCurrencies))
	for i, c := range r.ReceiverCurrencies {
		currencies[i] = nip47.Nip47Currency{
			Code:                c.Code,
			Name:                c.Name,
			Symbol:              c.Symbol,
			MillisatoshiPerUnit: c.MillisatoshiPerUnit,
			MinSendable:         c.MinSendable,
			MaxSendable:         c.MaxSendable,
			Decimals:            c.Decimals,
		}
	}

	return &nip47.Nip47LookupUserResponse{
		Currencies: currencies,
	}
}
