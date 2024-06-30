package main

import (
	"context"
	"encoding/hex"
	"errors"
	"github.com/getAlby/nostr-wallet-connect/nip47"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/lightsparkdev/go-sdk/objects"
	"github.com/lightsparkdev/go-sdk/services"
	"github.com/lightsparkdev/go-sdk/utils"

	decodepay "github.com/nbd-wtf/ln-decodepay"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"

	"github.com/labstack/echo-contrib/session"
	"github.com/labstack/echo/v4"
)

type LightsparkService struct {
	client *services.LightsparkClient
	nodeId string
	db     *gorm.DB
	Logger *logrus.Logger
}

func (svc *LightsparkService) AuthHandler(c echo.Context) error {
	user := &User{}
	err := svc.db.FirstOrInit(user, User{AlbyIdentifier: "lightspark"}).Error
	if err != nil {
		return err
	}

	sess, _ := session.Get(CookieName, c)
	sess.Values["user_id"] = user.ID
	sess.Save(c.Request(), c.Response())
	return c.Redirect(302, "/")
}

func (svc *LightsparkService) GetBalance(ctx context.Context, senderPubkey string) (balance int64, err error) {
	node, err := svc.getNode()
	if err != nil {
		return 0, err
	}
	msats, err := utils.ValueMilliSatoshi((*node).GetBalances().OwnedBalance)
	if err != nil {
		return 0, err
	}
	return msats / 1000, nil
}

func (svc *LightsparkService) ListTransactions(ctx context.Context, senderPubkey string, from, until, limit, offset uint64, unpaid bool, invoiceType string) (transactions []nip47.Nip47Transaction, err error) {
	account, err := svc.client.GetCurrentAccount()
	if err != nil {
		return nil, err
	}
	signedLimit := int64(limit)
	fromDate := time.Unix(int64(from), 0)
	untilDate := time.Unix(int64(until), 0)
	transactionTypes := []objects.TransactionType{objects.TransactionTypeIncomingPayment, objects.TransactionTypeOutgoingPayment}
	if invoiceType == "incoming" {
		transactionTypes = []objects.TransactionType{objects.TransactionTypeIncomingPayment}
	} else if invoiceType == "outgoing" {
		transactionTypes = []objects.TransactionType{objects.TransactionTypeOutgoingPayment}
	}

	transactionsConnection, err := account.GetTransactions(
		svc.client.Requester,
		&signedLimit,      // first
		nil,               //after
		&transactionTypes, // types
		&fromDate,         // after_date
		&untilDate,        // before_date
		nil,               // bitcoin_network
		&svc.nodeId,       // lightning_node_id
		nil,               // statuses
		nil,               //exclude_failures
	)
	if err != nil {
		return nil, err
	}
	for _, transaction := range transactionsConnection.Entities {
		nip47Transaction, err := svc.lightsparkTransactionToNip47(transaction)
		if err == nil && nip47Transaction != nil {
			transactions = append(transactions, *nip47Transaction)
		}
	}
	// TODO: Handle unpaid invoices.

	// sort by created date descending
	sort.SliceStable(transactions, func(i, j int) bool {
		return transactions[i].CreatedAt > transactions[j].CreatedAt
	})

	return transactions, nil
}

func derefOrEmptyString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (svc *LightsparkService) GetInfo(ctx context.Context, senderPubkey string) (info *NodeInfo, err error) {
	nodePtr, err := svc.getNode()
	if err != nil {
		return nil, err
	}
	node := *nodePtr
	return &NodeInfo{
		Alias:       derefOrEmptyString(node.GetAlias()),
		Color:       derefOrEmptyString(node.GetColor()),
		Pubkey:      derefOrEmptyString(node.GetPublicKey()),
		Network:     strings.ToLower(node.GetBitcoinNetwork().StringValue()),
		BlockHeight: 0,
		BlockHash:   "",
	}, nil
}

func (svc *LightsparkService) LookupUser(ctx context.Context, senderPubkey string, address string) (response *nip47.Nip47LookupUserResponse, err error) {
	return nil, errors.New("Not implemented")
}

func (svc *LightsparkService) FetchQuote(ctx context.Context, senderPubkey string, params nip47.Nip47FetchQuoteParams) (quote *nip47.Nip47Quote, err error) {
	return nil, errors.New("Not implemented")
}

func (svc *LightsparkService) ExecuteQuote(ctx context.Context, senderPubkey string, paymentHash string) (preimage string, err error) {
	return "", errors.New("Not implemented")
}

func (svc *LightsparkService) PayToAddress(ctx context.Context, senderPubkey string, params nip47.Nip47PayToAddressParams) (response *nip47.Nip47PayToAddressResponse, err error) {
	return nil, errors.New("Not implemented")
}

func (svc *LightsparkService) MakeInvoice(ctx context.Context, senderPubkey string, amount int64, description string, descriptionHash string, expiry int64) (transaction *nip47.Nip47Transaction, err error) {
	var descriptionHashBytes []byte

	if descriptionHash != "" {
		descriptionHashBytes, err = hex.DecodeString(descriptionHash)

		if err != nil || len(descriptionHashBytes) != 32 {
			svc.Logger.WithFields(logrus.Fields{
				"senderPubkey":    senderPubkey,
				"amount":          amount,
				"description":     description,
				"descriptionHash": descriptionHash,
				"expiry":          expiry,
			}).Errorf("Invalid description hash")
			return nil, errors.New("Description hash must be 32 bytes hex")
		}
	}

	expiry32 := int32(expiry)
	// TODO: Add description hash somehow.
	invoice, err := svc.client.CreateInvoice(svc.nodeId, amount, &description, nil, &expiry32)
	if err != nil {
		return nil, err
	}

	transaction, err = svc.lightsparkInvoiceToNip47(*invoice)
	if err != nil {
		return nil, err
	}
	return transaction, nil
}

func (svc *LightsparkService) LookupInvoice(ctx context.Context, senderPubkey string, paymentHash string) (transaction *nip47.Nip47Transaction, err error) {
	paymentHashBytes, err := hex.DecodeString(paymentHash)

	if err != nil || len(paymentHashBytes) != 32 {
		svc.Logger.WithFields(logrus.Fields{
			"paymentHash": paymentHash,
		}).Errorf("Invalid payment hash")
		return nil, errors.New("Payment hash must be 32 bytes hex")
	}

	invoice, err := svc.client.FetchInvoiceByPaymentHash(paymentHash)
	if err != nil {
		return nil, err
	}
	if invoice == nil {
		return nil, errors.New("invoice not found")
	}

	transaction, err = svc.lightsparkInvoiceToNip47(*invoice)
	if err != nil {
		return nil, err
	}
	return transaction, nil
}

func (svc *LightsparkService) SendPaymentSync(ctx context.Context, senderPubkey, payReq string, amount int64) (preimage string, err error) {
	bolt11, err := decodepay.Decodepay(payReq)
	if err != nil {
		return "", err
	}
	maxFees := math.Round(math.Max(float64(bolt11.MSatoshi)*.0016, 1000))
	resp, err := svc.client.PayInvoice(svc.nodeId, payReq, 60, int64(maxFees), nil)
	if err != nil {
		return "", err
	}
	transaction, err := svc.waitForPaymentCompletion(resp.Id)
	if err != nil {
		return "", err
	}
	outgoingPayment, ok := (*transaction).(objects.OutgoingPayment)
	if !ok {
		return "", errors.New("failed to cast payment to outgoing payment")
	}
	if outgoingPayment.PaymentPreimage == nil {
		return "", errors.New("payment preimage is nil")
	}
	return *outgoingPayment.PaymentPreimage, nil
}

func (svc *LightsparkService) SendKeysend(ctx context.Context, senderPubkey string, amount int64, destination, preimage string, custom_records []nip47.TLVRecord) (respPreimage string, err error) {
	// NOTE: We don't use the preimage or custom records for keysend in Lightspark.
	maxFees := math.Round(float64(amount) * .0016)
	resp, err := svc.client.SendPayment(svc.nodeId, destination, amount, 60, int64(maxFees))
	if err != nil {
		return "", err
	}
	transaction, err := svc.waitForPaymentCompletion(resp.GetId())
	if err != nil {
		return "", err
	}
	outgoingPayment, ok := (*transaction).(objects.OutgoingPayment)
	if !ok {
		return "", errors.New("failed to cast payment to outgoing payment")
	}
	if outgoingPayment.PaymentPreimage == nil {
		return "", errors.New("payment preimage is nil")
	}
	respPreimage = *outgoingPayment.PaymentPreimage
	svc.Logger.WithFields(logrus.Fields{
		"senderPubkey": senderPubkey,
		"amount":       amount,
		"payeePubkey":  destination,
		"respPreimage": respPreimage,
	}).Info("Keysend payment successful")

	return respPreimage, nil
}

func (svc *LightsparkService) getNode() (*objects.LightsparkNode, error) {
	nodeEntity, err := svc.client.GetEntity(svc.nodeId)
	if err != nil {
		return nil, err
	}
	node, ok := (*nodeEntity).(objects.LightsparkNode)
	if !ok || node == nil {
		return nil, errors.New("failed to cast node to LightsparkNode")
	}
	return &node, nil
}

func NewLightsparkService(ctx context.Context, svc *Service, e *echo.Echo) (result *LightsparkService, err error) {
	svc.Logger.Infof("Connecting to Lightspark account - %s", svc.cfg.LightsparkBaseUrl)
	lightsparkClient := services.NewLightsparkClient(
		svc.cfg.LightsparkClientId,
		svc.cfg.LightsparkClientSecret,
		&svc.cfg.LightsparkBaseUrl,
		services.WithContext(ctx),
	)
	account, err := lightsparkClient.GetCurrentAccount()
	if err != nil {
		return nil, err
	}
	//add default user to db
	user := &User{}
	err = svc.db.FirstOrInit(user, User{AlbyIdentifier: "lightspark"}).Error
	if err != nil {
		return nil, err
	}
	err = svc.db.Save(user).Error
	if err != nil {
		return nil, err
	}
	lightsparkClient.LoadNodeSigningKey(
		svc.cfg.LightsparkNodeId,
		*services.NewSigningKeyLoaderFromNodeIdAndPassword(svc.cfg.LightsparkNodeId, svc.cfg.LightsparkNodePassword),
	)

	lightsparkService := &LightsparkService{client: lightsparkClient, Logger: svc.Logger, db: svc.db, nodeId: svc.cfg.LightsparkNodeId}

	e.GET("/lightspark/auth", lightsparkService.AuthHandler)
	svc.Logger.Infof("Connected to Lightspark account - %s", *account.Name)

	return lightsparkService, nil
}

func (svc *LightsparkService) lightsparkTransactionToNip47(transaction objects.LightningTransaction) (*nip47.Nip47Transaction, error) {
	settledAt := transaction.GetResolvedAt().Unix()
	var preimage string
	var expiresAt *int64
	var paymentRequestData *objects.PaymentRequestData

	outgoingTransaction, isOutgoing := transaction.(objects.OutgoingPayment)
	if isOutgoing {
		if outgoingTransaction.PaymentPreimage != nil {
			preimage = *outgoingTransaction.PaymentPreimage
		}
		paymentRequestData = outgoingTransaction.PaymentRequestData
	}
	incomingTransaction, isIncoming := transaction.(objects.IncomingPayment)
	if isIncoming {
		paymentRequestEnt, err := svc.client.GetEntity(incomingTransaction.PaymentRequest.Id)
		if err != nil {
			return nil, err
		}
		paymentRequest, isInvoice := (*paymentRequestEnt).(objects.Invoice)
		if !isInvoice {
			return nil, errors.New("incoming transaction payment request is not an invoice")
		}
		paymentRequestDataRaw := paymentRequest.GetData()
		paymentRequestData = &paymentRequestDataRaw
		// NOTE: We're not exposing the preimage for incoming payments.
	}
	if paymentRequestData == nil {
		return nil, errors.New("payment request data is nil")
	}

	encodedPaymentRequest := strings.ToLower((*paymentRequestData).GetEncodedPaymentRequest())
	bolt11, err := decodepay.Decodepay(encodedPaymentRequest)
	if err != nil {
		return nil, err
	}

	if bolt11.Expiry > 0 {
		expiresAtUnix := int64(bolt11.CreatedAt + bolt11.Expiry)
		expiresAt = &expiresAtUnix
	}

	paymentType := "incoming"
	if isOutgoing {
		paymentType = "outgoing"
	}
	return &nip47.Nip47Transaction{
		Type:            paymentType,
		Invoice:         encodedPaymentRequest,
		Description:     bolt11.Description,
		DescriptionHash: bolt11.DescriptionHash,
		Preimage:        preimage,
		PaymentHash:     bolt11.PaymentHash,
		Amount:          bolt11.MSatoshi,
		FeesPaid:        0,
		CreatedAt:       transaction.GetCreatedAt().Unix(),
		SettledAt:       &settledAt,
		ExpiresAt:       expiresAt,
		// TODO: Metadata (e.g. keysend)
	}, nil
}

func (svc *LightsparkService) lightsparkInvoiceToNip47(invoice objects.Invoice) (*nip47.Nip47Transaction, error) {
	encodedPaymentRequest := strings.ToLower(invoice.Data.EncodedPaymentRequest)
	bolt11, err := decodepay.Decodepay(encodedPaymentRequest)
	if err != nil {
		return nil, err
	}
	expiresAt := invoice.Data.ExpiresAt.Unix()
	return &nip47.Nip47Transaction{
		Type:            "incoming",
		Invoice:         invoice.Data.EncodedPaymentRequest,
		Description:     bolt11.Description,
		DescriptionHash: bolt11.DescriptionHash,
		Preimage:        "",
		PaymentHash:     invoice.Data.PaymentHash,
		Amount:          bolt11.MSatoshi,
		FeesPaid:        0,
		CreatedAt:       invoice.GetCreatedAt().Unix(),
		SettledAt:       nil,
		ExpiresAt:       &expiresAt,
		// TODO: Metadata (e.g. keysend)
	}, nil
}

func (svc *LightsparkService) waitForPaymentCompletion(paymentId string) (*objects.Transaction, error) {
	payment, err := svc.client.GetEntity(paymentId)
	if err != nil {
		return nil, err
	}
	castPayment, didCast := (*payment).(objects.Transaction)
	if !didCast {
		return nil, errors.New("failed to cast payment to transaction")
	}
	startTime := time.Now()
	for castPayment.GetStatus() != objects.TransactionStatusSuccess && castPayment.GetStatus() != objects.TransactionStatusFailed {
		// 3 minutes timeout for now.
		if time.Since(startTime) > time.Minute*3 {
			return nil, errors.New("payment timed out")
		}
		payment, err = svc.client.GetEntity(paymentId)
		if err != nil {
			return nil, err
		}
		castPayment, didCast = (*payment).(objects.LightningTransaction)
		if !didCast {
			return nil, errors.New("failed to cast payment to transaction")
		}
	}
	if castPayment.GetStatus() == objects.TransactionStatusFailed {
		if reflect.TypeOf(castPayment) == reflect.TypeOf(objects.OutgoingPayment{}) {
			outgoingPayment, ok := castPayment.(objects.OutgoingPayment)
			if !ok {
				return nil, errors.New("failed to cast payment to outgoing payment")
			}
			if outgoingPayment.FailureReason != nil {
				return &castPayment, errors.New(outgoingPayment.FailureReason.StringValue())
			} else {
				return &castPayment, errors.New("payment failed with failure reason unavailable")
			}
		} else {
			return &castPayment, errors.New("payment failed")
		}
	}

	return &castPayment, nil
}
