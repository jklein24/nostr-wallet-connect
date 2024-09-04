package uma

import (
	"github.com/getAlby/nostr-wallet-connect/nip47"
	"github.com/uma-universal-money-address/uma-auth-api/codegen/go/umaauth"
)

func InvoiceToNip47Transaction(i umaauth.Transaction) *nip47.Nip47Transaction {
	preimage := ""
	if i.Preimage != nil {
		preimage = *i.Preimage
	}

	memo := ""
	if i.Description != nil {
		memo = *i.Description
	}

	descriptionHash := ""
	if i.DescriptionHash != nil {
		descriptionHash = *i.DescriptionHash
	}

	return &nip47.Nip47Transaction{
		Type:            string(i.Type),
		Invoice:         *i.Invoice,
		Description:     memo,
		DescriptionHash: descriptionHash,
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

type TokenRequest struct {
	Permissions []string `json:"permissions"`
	Expiration  *int64   `json:"expiration"`
}

type TokenResponse struct {
	Token string `json:"token"`
}

func ToNip47LookupUserResponse(r umaauth.LookupUserResponse) *nip47.Nip47LookupUserResponse {
	currencies := make([]nip47.Nip47Currency, len(r.Currencies))
	for i, c := range r.Currencies {
		currencies[i] = nip47.Nip47Currency{
			Code:                c.Code,
			Name:                c.Name,
			Symbol:              c.Symbol,
			MillisatoshiPerUnit: float64(c.Multiplier),
			MinSendable:         c.Min,
			MaxSendable:         c.Max,
			Decimals:            int(c.Decimals),
		}
	}

	return &nip47.Nip47LookupUserResponse{
		Currencies: currencies,
	}
}
