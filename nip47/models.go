package nip47

import (
	"encoding/json"
)

// TODO: move to models/Nip47
type Nip47Transaction struct {
	Type            string      `json:"type"`
	Invoice         string      `json:"invoice"`
	Description     string      `json:"description"`
	DescriptionHash string      `json:"description_hash"`
	Preimage        string      `json:"preimage"`
	PaymentHash     string      `json:"payment_hash"`
	Amount          int64       `json:"amount"`
	FeesPaid        int64       `json:"fees_paid"`
	CreatedAt       int64       `json:"created_at"`
	ExpiresAt       *int64      `json:"expires_at"`
	SettledAt       *int64      `json:"settled_at"`
	Metadata        interface{} `json:"metadata,omitempty"`
}

type Nip47Currency struct {
	// Code is the ISO 4217 (if applicable) currency code (eg. "USD"). For cryptocurrencies, this will  be a ticker
	// symbol, such as BTC for Bitcoin.
	Code string `json:"code"`

	// Name is the full display name of the currency (eg. US Dollars).
	Name string `json:"name"`

	// Symbol is the symbol of the currency (eg. $ for USD).
	Symbol string `json:"symbol"`

	// MillisatoshiPerUnit is the estimated millisats per smallest "unit" of this currency (eg. 1 cent in USD).
	MillisatoshiPerUnit float64 `json:"multiplier"`

	MinSendable int64 `json:"min"`

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

// TODO: move to models/Nip47
type Nip47Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type Nip47Response struct {
	Error      *Nip47Error `json:"error,omitempty"`
	Result     interface{} `json:"result,omitempty"`
	ResultType string      `json:"result_type"`
}

type Nip47Error struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type Nip47PayParams struct {
	Invoice string `json:"invoice"`
	Amount  *int64 `json:"amount,omitempty"`
}
type Nip47PayResponse struct {
	Preimage string `json:"preimage"`
}

type Nip47KeysendParams struct {
	Amount     int64       `json:"amount"`
	Pubkey     string      `json:"pubkey"`
	Preimage   string      `json:"preimage"`
	TLVRecords []TLVRecord `json:"tlv_records"`
}

type Nip47BalanceResponse struct {
	Balance       int64  `json:"balance"`
	MaxAmount     int    `json:"max_amount"`
	BudgetRenewal string `json:"budget_renewal"`
}

// TODO: move to models/Nip47
type Nip47GetInfoResponse struct {
	Alias       string   `json:"alias"`
	Color       string   `json:"color"`
	Pubkey      string   `json:"pubkey"`
	Network     string   `json:"network"`
	BlockHeight uint32   `json:"block_height"`
	BlockHash   string   `json:"block_hash"`
	Methods     []string `json:"methods"`
}

type Nip47MakeInvoiceParams struct {
	Amount          int64  `json:"amount"`
	Description     string `json:"description"`
	DescriptionHash string `json:"description_hash"`
	Expiry          int64  `json:"expiry"`
}
type Nip47MakeInvoiceResponse struct {
	Nip47Transaction
}

type Nip47LookupInvoiceParams struct {
	Invoice     string `json:"invoice"`
	PaymentHash string `json:"payment_hash"`
}

type Nip47LookupInvoiceResponse struct {
	Nip47Transaction
}

type Nip47ListTransactionsParams struct {
	From   uint64 `json:"from,omitempty"`
	Until  uint64 `json:"until,omitempty"`
	Limit  uint64 `json:"limit,omitempty"`
	Offset uint64 `json:"offset,omitempty"`
	Unpaid bool   `json:"unpaid,omitempty"`
	Type   string `json:"type,omitempty"`
}

type Nip47ListTransactionsResponse struct {
	Transactions []Nip47Transaction `json:"transactions"`
}

type TLVRecord struct {
	Type  uint64 `json:"type"`
	Value string `json:"value"`
}

type Nip47LookupUserParams struct {
	Lud16 string `json:"lud16"`
}

type Nip47LookupUserResponse struct {
	Currencies []Nip47Currency `json:"currencies"`
}

type Nip47ReceiverAddress struct {
	Lud16  *string `json:"lud16"`
	Bolt12 *string `json:"bolt12"`
}

type Nip47FetchQuoteParams struct {
	Receiver              Nip47ReceiverAddress `json:"receiver"`
	SendingCurrencyCode   string               `json:"sending_currency_code"`
	ReceivingCurrencyCode string               `json:"receiving_currency_code"`
	LockedCurrencySide    string               `json:"locked_currency_side"`
	LockedCurrencyAmount  int64                `json:"locked_currency_amount"`
}

type Nip47Quote struct {
	PaymentHash           string  `json:"payment_hash"`
	ExpiresAt             int64   `json:"expires_at"`
	Multiplier            float64 `json:"multiplier"`
	SendingCurrencyCode   string  `json:"sending_currency_code"`
	ReceivingCurrencyCode string  `json:"receiving_currency_code"`
	Fee                   int64   `json:"fee"`
	TotalReceivingAmount  int64   `json:"total_receiving_amount"`
	TotalSendingAmount    int64   `json:"total_sending_amount"`
	LockedCurrencySide    string  `json:"locked_currency_side"`
}

type Nip47ExecuteQuoteParams struct {
	PaymentHash string `json:"payment_hash"`
}

type Nip47ExecuteQuoteResponse struct {
	Preimage string `json:"preimage"`
}

type Nip47PayToAddressParams struct {
	Receiver              Nip47ReceiverAddress `json:"receiver"`
	SendingCurrencyCode   string               `json:"sending_currency_code"`
	ReceivingCurrencyCode *string              `json:"receiving_currency_code"`
	SendingCurrencyAmount int64                `json:"sending_currency_amount"`
}

type Nip47PayToAddressResponse struct {
	Preimage string      `json:"preimage"`
	Quote    *Nip47Quote `json:"quote,omitempty"`
}
