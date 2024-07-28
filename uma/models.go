package uma

import (
	"github.com/getAlby/nostr-wallet-connect/nip47"
	"github.com/uma-universal-money-address/uma-auth-api/codegen/go/umaauth"
)

func InvoiceToNip47Transaction(i umaauth.Invoice) *nip47.Nip47Transaction {
	var expiresAt *int64 = nil
	if i.ExpiresAt != nil {
		expiresAt = new(int64)
		*expiresAt = i.ExpiresAt.Unix()
	}

	var settledAt *int64 = nil
	if i.SettledAt != nil {
		settledAt = new(int64)
		*settledAt = i.SettledAt.Unix()
	}

	preimage := ""
	if i.Preimage != nil {
		preimage = *i.Preimage
	}

	memo := ""
	if i.Memo != nil {
		memo = *i.Memo
	}

	return &nip47.Nip47Transaction{
		Type:            i.Type,
		Invoice:         i.PaymentRequest,
		Description:     memo,
		DescriptionHash: "",
		Preimage:        preimage,
		PaymentHash:     i.PaymentHash,
		Amount:          i.Amount,
		FeesPaid:        0, // TODO: support fees
		CreatedAt:       i.CreatedAt.Unix(),
		ExpiresAt:       expiresAt,
		SettledAt:       settledAt,
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
			MinSendable:         int64(c.Min),
			MaxSendable:         int64(c.Max),
			Decimals:            int(c.Decimals),
		}
	}

	return &nip47.Nip47LookupUserResponse{
		Currencies: currencies,
	}
}
