package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getAlby/nostr-wallet-connect/nip47"
	"github.com/getAlby/nostr-wallet-connect/uma"
	"github.com/golang-jwt/jwt"
	"github.com/uma-universal-money-address/uma-auth-api/codegen/go/umaauth"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

type UmaNwcAdapterService struct {
	cfg       *Config
	oauthConf *oauth2.Config
	db        *gorm.DB
	Logger    *logrus.Logger
}

func NewUmaNwcAdapterService(svc *Service, e *echo.Echo) (result *UmaNwcAdapterService, err error) {
	conf := &oauth2.Config{
		ClientID:     "", // not used
		ClientSecret: "", // not used
		//Todo: do we really need all these permissions?
		Scopes: []string{"account:read", "payments:send", "invoices:read", "transactions:read", "invoices:create", "balance:read"},
		Endpoint: oauth2.Endpoint{
			TokenURL:  "", // Not used right now
			AuthURL:   svc.cfg.UmaLoginUrl,
			AuthStyle: 2, // use HTTP Basic Authorization https://pkg.go.dev/golang.org/x/oauth2#AuthStyle
		},
		RedirectURL: svc.cfg.UmaRedirectUrl,
	}

	umaSvc := &UmaNwcAdapterService{
		cfg:       svc.cfg,
		oauthConf: conf,
		db:        svc.db,
		Logger:    svc.Logger,
	}

	e.GET("/uma/auth", umaSvc.AuthHandler)
	e.GET("/uma/callback", umaSvc.CallbackHandler)

	return umaSvc, err
}

func (svc *UmaNwcAdapterService) FetchUserToken(ctx context.Context, app App) (token *oauth2.Token, err error) {
	user := app.User
	if user.AccessToken == "" {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": app.NostrPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Error("User does not have access token")
		return nil, errors.New("user does not have access token")
	}
	// TODO: Use real oauth and refresh if needed here.
	tok := &oauth2.Token{
		AccessToken: user.AccessToken,
		TokenType:   "Bearer", // Use bearer or omit when it's a real token
	}
	return tok, nil
}

func (svc *UmaNwcAdapterService) MakeInvoice(ctx context.Context, senderPubkey string, amount int64, description string, descriptionHash string, expiry int64) (transaction *nip47.Nip47Transaction, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey":    senderPubkey,
			"amount":          amount,
			"description":     description,
			"descriptionHash": descriptionHash,
			"expiry":          expiry,
		}).Errorf("App not found: %v", err)
		return nil, err
	}

	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey":    senderPubkey,
		"amount":          amount,
		"description":     description,
		"descriptionHash": descriptionHash,
		"expiry":          expiry,
		"appId":           app.ID,
		"userId":          app.User.ID,
	}).Info("Processing make invoice request")
	tok, err := svc.FetchUserToken(ctx, *app)
	if err != nil {
		return nil, err
	}
	client := svc.oauthConf.Client(ctx, tok)

	body := bytes.NewBuffer([]byte{})
	payload := &umaauth.MakeInvoiceRequest{
		Amount:          amount,
		Description:     description,
		DescriptionHash: descriptionHash,
		Expiry:          int32(expiry),
	}
	err = json.NewEncoder(body).Encode(payload)

	// TODO: move to a shared function
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/invoice", svc.cfg.UmaAPIURL), body)
	if err != nil {
		svc.Logger.WithError(err).Error("Error creating request /invoices")
		return nil, err
	}

	// TODO: move to creation of HTTP client
	req.Header.Set("User-Agent", "NWC")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey":    senderPubkey,
			"amount":          amount,
			"description":     description,
			"descriptionHash": descriptionHash,
			"expiry":          expiry,
			"appId":           app.ID,
			"userId":          app.User.ID,
		}).Errorf("Failed to make invoice: %v", err)
		return nil, err
	}

	if resp.StatusCode < 300 {
		responsePayload := &umaauth.Invoice{}
		err = json.NewDecoder(resp.Body).Decode(responsePayload)
		if err != nil {
			return nil, err
		}
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey":    senderPubkey,
			"amount":          amount,
			"description":     description,
			"descriptionHash": descriptionHash,
			"expiry":          expiry,
			"appId":           app.ID,
			"userId":          app.User.ID,
			"paymentRequest":  responsePayload.PaymentRequest,
			"paymentHash":     responsePayload.PaymentHash,
		}).Info("Make invoice successful")

		transaction := uma.InvoiceToNip47Transaction(*responsePayload)
		return transaction, nil
	}

	errorPayload := &ErrorResponse{}
	err = json.NewDecoder(resp.Body).Decode(errorPayload)
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey":    senderPubkey,
		"amount":          amount,
		"description":     description,
		"descriptionHash": descriptionHash,
		"expiry":          expiry,
		"appId":           app.ID,
		"userId":          app.User.ID,
		"APIHttpStatus":   resp.StatusCode,
	}).Errorf("Make invoice failed %s", string(errorPayload.Message))
	return nil, errors.New(errorPayload.Message)
}

func (svc *UmaNwcAdapterService) LookupInvoice(ctx context.Context, senderPubkey string, paymentHash string) (transaction *nip47.Nip47Transaction, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"paymentHash":  paymentHash,
		}).Errorf("App not found: %v", err)
		return nil, err
	}

	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey": senderPubkey,
		"paymentHash":  paymentHash,
		"appId":        app.ID,
		"userId":       app.User.ID,
	}).Info("Processing lookup invoice request")
	tok, err := svc.FetchUserToken(ctx, *app)
	if err != nil {
		return nil, err
	}
	client := svc.oauthConf.Client(ctx, tok)

	body := bytes.NewBuffer([]byte{})

	// TODO: move to a shared function
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/invoices/%s", svc.cfg.UmaAPIURL, paymentHash), body)
	if err != nil {
		svc.Logger.WithError(err).Errorf("Error creating request /invoices/%s", paymentHash)
		return nil, err
	}

	req.Header.Set("User-Agent", "NWC")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"paymentHash":  paymentHash,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Errorf("Failed to lookup invoice: %v", err)
		return nil, err
	}

	if resp.StatusCode < 300 {
		responsePayload := &umaauth.Invoice{}
		err = json.NewDecoder(resp.Body).Decode(responsePayload)
		if err != nil {
			return nil, err
		}
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey":   senderPubkey,
			"paymentHash":    paymentHash,
			"appId":          app.ID,
			"userId":         app.User.ID,
			"paymentRequest": responsePayload.PaymentRequest,
			"settled":        responsePayload.SettledAt != nil,
		}).Info("Lookup invoice successful")

		transaction = uma.InvoiceToNip47Transaction(*responsePayload)
		return transaction, nil
	}

	errorPayload := &ErrorResponse{}
	err = json.NewDecoder(resp.Body).Decode(errorPayload)
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey":  senderPubkey,
		"paymentHash":   paymentHash,
		"appId":         app.ID,
		"userId":        app.User.ID,
		"APIHttpStatus": resp.StatusCode,
	}).Errorf("Lookup invoice failed %s", string(errorPayload.Message))
	return nil, errors.New(errorPayload.Message)
}

func (svc *UmaNwcAdapterService) GetInfo(ctx context.Context, senderPubkey string) (info *NodeInfo, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
		}).Errorf("App not found: %v", err)
		return nil, err
	}

	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey": senderPubkey,
		"appId":        app.ID,
		"userId":       app.User.ID,
	}).Info("Info fetch successful")
	// TODO: Implement real node info
	return &NodeInfo{
		Alias:       "UMA",
		Color:       "",
		Pubkey:      "",
		Network:     "mainnet",
		BlockHeight: 0,
		BlockHash:   "",
	}, err
}

func (svc *UmaNwcAdapterService) LookupUser(ctx context.Context, senderPubkey string, address string) (response *nip47.Nip47LookupUserResponse, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"address":      address,
		}).Errorf("App not found: %v", err)
		return nil, err
	}

	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey": senderPubkey,
		"address":      address,
		"appId":        app.ID,
		"userId":       app.User.ID,
	}).Info("Processing lookup user request")
	tok, err := svc.FetchUserToken(ctx, *app)
	if err != nil {
		return nil, err
	}
	client := svc.oauthConf.Client(ctx, tok)

	body := bytes.NewBuffer([]byte{})

	// TODO: move to a shared function
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/receiver_info/%s", svc.cfg.UmaAPIURL, address), body)
	if err != nil {
		svc.Logger.WithError(err).Errorf("Error creating request /receiver_info/%s", address)
		return nil, err
	}

	req.Header.Set("User-Agent", "NWC")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"address":      address,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Errorf("Failed to lookup user: %v", err)
		return nil, err
	}

	if resp.StatusCode < 300 {
		responsePayload := &umaauth.LookupUserResponse{}
		err = json.NewDecoder(resp.Body).Decode(responsePayload)
		if err != nil {
			return nil, err
		}
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"address":      address,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Info("Lookup user successful")

		return uma.ToNip47LookupUserResponse(*responsePayload), nil
	}

	errorPayload := &ErrorResponse{}
	err = json.NewDecoder(resp.Body).Decode(errorPayload)
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey": senderPubkey,
		"address":      address,
		"appId":        app.ID,
		"userId":       app.User.ID,
	}).Errorf("Lookup user failed %s", string(errorPayload.Message))
	return nil, errors.New(errorPayload.Message)
}

func (svc *UmaNwcAdapterService) FetchQuote(ctx context.Context, senderPubkey string, params nip47.Nip47FetchQuoteParams) (quote *nip47.Nip47Quote, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"params":       params,
		}).Errorf("App not found: %v", err)
		return nil, err
	}
	tok, err := svc.FetchUserToken(ctx, *app)
	if err != nil {
		return nil, err
	}
	client := svc.oauthConf.Client(ctx, tok)

	body := bytes.NewBuffer([]byte{})
	urlParams := url.Values{}
	urlParams.Add("locked_currency_amount", strconv.FormatInt(params.LockedCurrencyAmount, 10))
	urlParams.Add("locked_currency_side", params.LockedCurrencySide)
	urlParams.Add("receiving_currency_code", params.ReceivingCurrencyCode)
	urlParams.Add("sending_currency_code", params.SendingCurrencyCode)
	urlParams.Add("receiving_address", *params.Receiver.Lud16)

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/quote?%s", svc.cfg.UmaAPIURL, urlParams.Encode()), body)
	if err != nil {
		svc.Logger.WithError(err).Errorf("Error creating quote request")
		return nil, err
	}

	req.Header.Set("User-Agent", "NWC")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Errorf("Failed to fetch quote: %v", err)
		return nil, err
	}

	if resp.StatusCode < 300 {
		responsePayload := &nip47.Nip47Quote{}
		err = json.NewDecoder(resp.Body).Decode(responsePayload)
		if err != nil {
			return nil, err
		}
		responsePayload.LockedCurrencySide = params.LockedCurrencySide
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
			"quote":        responsePayload,
		}).Info("Fetching quote successful")

		return responsePayload, nil
	}

	errorPayload := &ErrorResponse{}
	err = json.NewDecoder(resp.Body).Decode(errorPayload)
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey": senderPubkey,
		"appId":        app.ID,
		"userId":       app.User.ID,
	}).Errorf("Fetching quote failed %s", errorPayload.Message)
	return nil, errors.New(errorPayload.Message)
}

func (svc *UmaNwcAdapterService) ExecuteQuote(ctx context.Context, senderPubkey string, paymentHash string) (response *nip47.Nip47ExecuteQuoteResponse, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"paymentHash":  paymentHash,
		}).Errorf("App not found: %v", err)
		return nil, err
	}
	tok, err := svc.FetchUserToken(ctx, *app)
	if err != nil {
		return nil, err
	}
	client := svc.oauthConf.Client(ctx, tok)

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/quote/%s", svc.cfg.UmaAPIURL, paymentHash), nil)
	if err != nil {
		svc.Logger.WithError(err).Errorf("Error executing quote request")
		return nil, err
	}

	req.Header.Set("User-Agent", "NWC")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Errorf("Failed to execute quote: %v", err)
		return nil, err
	}

	if resp.StatusCode < 300 {
		responsePayload := &nip47.Nip47ExecuteQuoteResponse{}
		err = json.NewDecoder(resp.Body).Decode(responsePayload)
		if err != nil {
			return nil, err
		}
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Info("Executing quote successful")

		return responsePayload, nil
	}

	errorPayload := &ErrorResponse{}
	err = json.NewDecoder(resp.Body).Decode(errorPayload)
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey": senderPubkey,
		"appId":        app.ID,
		"userId":       app.User.ID,
	}).Errorf("Executing quote failed %s", errorPayload.Message)
	return nil, errors.New(errorPayload.Message)
}

func (svc *UmaNwcAdapterService) PayToAddress(ctx context.Context, senderPubkey string, params nip47.Nip47PayToAddressParams) (response *nip47.Nip47PayToAddressResponse, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"params":       params,
		}).Errorf("App not found: %v", err)
		return nil, err
	}
	tok, err := svc.FetchUserToken(ctx, *app)
	if err != nil {
		return nil, err
	}
	client := svc.oauthConf.Client(ctx, tok)
	receivingCurrency := ""
	if params.ReceivingCurrencyCode != nil {
		receivingCurrency = *params.ReceivingCurrencyCode
	}

	body := bytes.NewBuffer([]byte{})
	payload := &umaauth.PayToAddressRequest{
		SendingCurrencyAmount: params.SendingCurrencyAmount,
		ReceiverAddress:       *params.Receiver.Lud16,
		ReceivingCurrencyCode: receivingCurrency,
		SendingCurrencyCode:   params.SendingCurrencyCode,
	}
	err = json.NewEncoder(body).Encode(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/payments/lnurl", svc.cfg.UmaAPIURL), body)
	if err != nil {
		svc.Logger.WithError(err).Errorf("Error creating quote request")
		return nil, err
	}

	req.Header.Set("User-Agent", "NWC")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Errorf("Failed to fetch quote: %v", err)
		return nil, err
	}

	if resp.StatusCode < 300 {
		responsePayload := &nip47.Nip47PayToAddressResponse{}
		err = json.NewDecoder(resp.Body).Decode(responsePayload)
		if err != nil {
			return nil, err
		}
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Info("Pay to address successful")

		return responsePayload, nil
	}

	errorPayload := &ErrorResponse{}
	err = json.NewDecoder(resp.Body).Decode(errorPayload)
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey": senderPubkey,
		"appId":        app.ID,
		"userId":       app.User.ID,
	}).Errorf("Fetching quote failed %s", errorPayload.Message)
	return nil, errors.New(errorPayload.Message)
}

func (svc *UmaNwcAdapterService) GetBalance(ctx context.Context, senderPubkey string) (balance int64, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
		}).Errorf("App not found: %v", err)
		return 0, err
	}
	tok, err := svc.FetchUserToken(ctx, *app)
	if err != nil {
		return 0, err
	}
	client := svc.oauthConf.Client(ctx, tok)

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/balance", svc.cfg.UmaAPIURL), nil)
	if err != nil {
		svc.Logger.WithError(err).Error("Error creating request /balance")
		return 0, err
	}

	req.Header.Set("User-Agent", "NWC")

	resp, err := client.Do(req)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Errorf("Failed to fetch balance: %v", err)
		return 0, err
	}

	if resp.StatusCode < 300 {
		responsePayload := &umaauth.GetBalanceResponse{}
		err = json.NewDecoder(resp.Body).Decode(responsePayload)
		if err != nil {
			return 0, err
		}
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Info("Balance fetch successful")

		return int64(math.Floor(float64(responsePayload.Balance))), nil
	}

	errorPayload := &ErrorResponse{}
	err = json.NewDecoder(resp.Body).Decode(errorPayload)
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey":  senderPubkey,
		"appId":         app.ID,
		"userId":        app.User.ID,
		"APIHttpStatus": resp.StatusCode,
	}).Errorf("Balance fetch failed %s", errorPayload.Message)
	return 0, errors.New(errorPayload.Message)
}

func (svc *UmaNwcAdapterService) ListTransactions(ctx context.Context, senderPubkey string, from, until, limit, offset uint64, unpaid bool, invoiceType string) (transactions []nip47.Nip47Transaction, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
		}).Errorf("App not found: %v", err)
		return nil, err
	}
	tok, err := svc.FetchUserToken(ctx, *app)
	if err != nil {
		return nil, err
	}
	client := svc.oauthConf.Client(ctx, tok)

	urlParams := url.Values{}
	//urlParams.Add("page", "1")

	// TODO: clarify gt/lt vs from to in NWC spec
	if from != 0 {
		urlParams.Add("q[created_at_gt]", strconv.FormatUint(from, 10))
	}
	if until != 0 {
		urlParams.Add("q[created_at_lt]", strconv.FormatUint(until, 10))
	}
	if limit != 0 {
		urlParams.Add("items", strconv.FormatUint(limit, 10))
	}
	// TODO: Add Offset and Unpaid

	endpoint := "/invoices"

	switch invoiceType {
	case "incoming":
		endpoint += "/incoming"
	case "outgoing":
		endpoint += "/outgoing"
	}

	requestUrl := fmt.Sprintf("%s%s?%s", svc.cfg.UmaAPIURL, endpoint, urlParams.Encode())

	req, err := http.NewRequest("GET", requestUrl, nil)
	if err != nil {
		svc.Logger.WithError(err).Error("Error creating request /invoices")
		return nil, err
	}

	req.Header.Set("User-Agent", "NWC")

	resp, err := client.Do(req)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
			"requestUrl":   requestUrl,
		}).Errorf("Failed to fetch invoices: %v", err)
		return nil, err
	}

	var invoices []umaauth.Invoice

	if resp.StatusCode < 300 {
		err = json.NewDecoder(resp.Body).Decode(&invoices)
		if err != nil {
			svc.Logger.WithFields(logrus.Fields{
				"senderPubkey": senderPubkey,
				"appId":        app.ID,
				"userId":       app.User.ID,
				"requestUrl":   requestUrl,
			}).Errorf("Failed to decode invoices: %v", err)
			return nil, err
		}

		transactions = []nip47.Nip47Transaction{}
		for _, invoice := range invoices {
			transaction := uma.InvoiceToNip47Transaction(invoice)

			transactions = append(transactions, *transaction)
		}

		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"appId":        app.ID,
			"userId":       app.User.ID,
			"requestUrl":   requestUrl,
		}).Info("List transactions successful")
		return transactions, nil
	}

	errorPayload := &ErrorResponse{}
	err = json.NewDecoder(resp.Body).Decode(errorPayload)
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey":  senderPubkey,
		"appId":         app.ID,
		"userId":        app.User.ID,
		"APIHttpStatus": resp.StatusCode,
		"requestUrl":    requestUrl,
	}).Errorf("List transactions failed %s", string(errorPayload.Message))
	return nil, errors.New(errorPayload.Message)
}

func (svc *UmaNwcAdapterService) SendPaymentSync(ctx context.Context, senderPubkey, payReq string, amount int64) (preimage string, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"bolt11":       payReq,
		}).Errorf("App not found: %v", err)
		return "", err
	}
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey": senderPubkey,
		"bolt11":       payReq,
		"appId":        app.ID,
		"userId":       app.User.ID,
	}).Info("Processing payment request")
	tok, err := svc.FetchUserToken(ctx, *app)
	if err != nil {
		return "", err
	}
	client := svc.oauthConf.Client(ctx, tok)

	body := bytes.NewBuffer([]byte{})
	payload := &umaauth.PayInvoiceRequest{
		Invoice: payReq,
		Amount:  &amount,
	}
	err = json.NewEncoder(body).Encode(payload)

	req, err := http.NewRequest("POST", fmt.Sprintf("%s/payments/bolt11", svc.cfg.UmaAPIURL), body)
	if err != nil {
		svc.Logger.WithError(err).Error("Error creating request /payments/bolt11")
		return "", err
	}

	req.Header.Set("User-Agent", "NWC")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"bolt11":       payReq,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Errorf("Failed to pay invoice: %v", err)
		return "", err
	}

	if resp.StatusCode < 300 {
		responsePayload := &umaauth.PayInvoiceResponse{}
		err = json.NewDecoder(resp.Body).Decode(responsePayload)
		if err != nil {
			return "", err
		}
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"bolt11":       payReq,
			"appId":        app.ID,
			"userId":       app.User.ID,
			"invoice":      payload.Invoice,
		}).Info("Payment successful")
		return responsePayload.Preimage, nil
	}

	errorPayload := &ErrorResponse{}
	err = json.NewDecoder(resp.Body).Decode(errorPayload)
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey":  senderPubkey,
		"bolt11":        payReq,
		"appId":         app.ID,
		"userId":        app.User.ID,
		"APIHttpStatus": resp.StatusCode,
	}).Errorf("Payment failed %s", string(errorPayload.Message))
	return "", errors.New(errorPayload.Message)
}

func (svc *UmaNwcAdapterService) SendKeysend(ctx context.Context, senderPubkey string, amount int64, destination, preimage string, custom_records []nip47.TLVRecord) (preImage string, err error) {
	app, err := svc.appFromPubkey(senderPubkey)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"payeePubkey":  destination,
		}).Errorf("App not found: %v", err)
		return "", err
	}
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey": senderPubkey,
		"payeePubkey":  destination,
		"appId":        app.ID,
		"userId":       app.User.ID,
	}).Info("Processing keysend request")
	tok, err := svc.FetchUserToken(ctx, *app)
	if err != nil {
		return "", err
	}
	client := svc.oauthConf.Client(ctx, tok)

	customRecordsMap := make(map[string]string)
	for _, record := range custom_records {
		customRecordsMap[strconv.FormatUint(record.Type, 10)] = record.Value
	}

	body := bytes.NewBuffer([]byte{})
	payload := &KeysendRequest{
		Amount:        amount,
		Destination:   destination,
		CustomRecords: customRecordsMap,
	}
	err = json.NewEncoder(body).Encode(payload)

	// here we don't use the preimage from params
	req, err := http.NewRequest("POST", fmt.Sprintf("%s/payments/keysend", svc.cfg.UmaAPIURL), body)
	if err != nil {
		svc.Logger.WithError(err).Error("Error creating request /payments/keysend")
		return "", err
	}

	req.Header.Set("User-Agent", "NWC")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"payeePubkey":  destination,
			"appId":        app.ID,
			"userId":       app.User.ID,
		}).Errorf("Failed to pay keysend: %v", err)
		return "", err
	}

	if resp.StatusCode < 300 {
		responsePayload := &PayResponse{}
		err = json.NewDecoder(resp.Body).Decode(responsePayload)
		if err != nil {
			return "", err
		}
		svc.Logger.WithFields(logrus.Fields{
			"senderPubkey": senderPubkey,
			"payeePubkey":  destination,
			"appId":        app.ID,
			"userId":       app.User.ID,
			"preimage":     responsePayload.Preimage,
			"paymentHash":  responsePayload.PaymentHash,
		}).Info("Keysend payment successful")
		return responsePayload.Preimage, nil
	}

	errorPayload := &ErrorResponse{}
	err = json.NewDecoder(resp.Body).Decode(errorPayload)
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey":  senderPubkey,
		"payeePubkey":   destination,
		"appId":         app.ID,
		"userId":        app.User.ID,
		"APIHttpStatus": resp.StatusCode,
	}).Errorf("Payment failed %s", string(errorPayload.Message))
	return "", errors.New(errorPayload.Message)
}

func (svc *UmaNwcAdapterService) AuthHandler(c echo.Context) error {
	appName := c.QueryParam("c") // c - for client
	// clear current session
	sess, _ := session.Get(CookieName, c)
	if sess.Values["user_id"] != nil {
		delete(sess.Values, "user_id")
		sess.Options.MaxAge = 0
		sess.Options.SameSite = http.SameSiteLaxMode
		if svc.cfg.CookieDomain != "" {
			sess.Options.Domain = svc.cfg.CookieDomain
		}
		sess.Save(c.Request(), c.Response())
	}

	url := svc.oauthConf.AuthCodeURL(appName)
	return c.Redirect(302, url)
}

func (svc *UmaNwcAdapterService) CallbackHandler(c echo.Context) error {
	userJwt := c.QueryParam("token")
	token, err := jwt.ParseWithClaims(userJwt, jwt.MapClaims{}, func(token *jwt.Token) (interface{}, error) {
		return jwt.ParseECPublicKeyFromPEM([]byte(svc.cfg.UmaVaspJwtPubKey))
	})
	if err != nil {
		return err
	}
	claims := token.Claims.(jwt.MapClaims)

	user := User{}
	umaAddress := claims["address"].(string)
	// TODO: Maybe use a different User ID from the UMA side?
	svc.db.FirstOrInit(&user, User{AlbyIdentifier: claims["sub"].(string)})

	// TODO: Should update the expiry of the claims when we create the secret
	tok := &oauth2.Token{
		AccessToken: userJwt,
		TokenType:   "Bearer", // Use bearer or omit when it's a real token
	}
	client := svc.oauthConf.Client(c.Request().Context(), tok)

	body := bytes.NewBuffer([]byte{})
	payload := &uma.TokenRequest{
		// TODO: Switch to real permissions when this moves to the right place.
		Permissions: []string{"all"},
	}
	err = json.NewEncoder(body).Encode(payload)
	if err != nil {
		return err
	}
	newTokenResp, err := client.Post(svc.cfg.UmaTokenUrl, "application/json", body)
	if err != nil {
		return err
	}
	if newTokenResp.StatusCode != 200 {
		return errors.New("failed to get new token")
	}
	newToken := &uma.TokenResponse{}
	err = json.NewDecoder(newTokenResp.Body).Decode(newToken)
	if err != nil {
		svc.Logger.Errorf("Failed to decode token response: %v", err)
		return err
	}

	// NOTE: This should really be set on the app, not the user, but I don't want to deal
	// with refactoring this application if we're building our own NWC server.
	user.AccessToken = newToken.Token
	user.LightningAddress = umaAddress
	svc.db.Save(&user)

	sess, _ := session.Get(CookieName, c)
	sess.Options.MaxAge = 0
	sess.Options.SameSite = http.SameSiteLaxMode
	if svc.cfg.CookieDomain != "" {
		sess.Options.Domain = svc.cfg.CookieDomain
	}
	sess.Values["user_id"] = user.ID
	sess.Save(c.Request(), c.Response())
	return c.Redirect(302, "/")
}

func (svc *UmaNwcAdapterService) appFromPubkey(senderPubkey string) (app *App, err error) {
	err = svc.db.Preload("User").First(&app, &App{
		NostrPubkey: senderPubkey,
	}).Error
	if err != nil {
		return nil, err
	}
	return app, nil
}
