package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/getAlby/nostr-wallet-connect/nip47"

	"github.com/nbd-wtf/go-nostr"
	"github.com/sirupsen/logrus"
)

func (svc *Service) HandleLookupUserEvent(ctx context.Context, request *nip47.Nip47Request, event *nostr.Event, app App, ss []byte) (result *nostr.Event, err error) {

	nostrEvent := NostrEvent{App: app, NostrId: event.ID, Content: event.Content, State: "received"}
	err = svc.db.Create(&nostrEvent).Error
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"eventId":   event.ID,
			"eventKind": event.Kind,
			"appId":     app.ID,
		}).Errorf("Failed to save nostr event: %v", err)
		return nil, err
	}

	hasPermission, code, message := svc.hasPermission(&app, event, request.Method, 0)

	if !hasPermission {
		svc.Logger.WithFields(logrus.Fields{
			"eventId":   event.ID,
			"eventKind": event.Kind,
			"appId":     app.ID,
		}).Errorf("App does not have permission: %s %s", code, message)

		return svc.createResponse(event, nip47.Nip47Response{
			ResultType: request.Method,
			Error: &nip47.Nip47Error{
				Code:    code,
				Message: message,
			}}, ss)
	}

	lookupUserParams := &nip47.Nip47LookupUserParams{}
	err = json.Unmarshal(request.Params, lookupUserParams)
	// TODO: Validate the address more.
	if err != nil || lookupUserParams.Lud16 == "" {
		svc.Logger.WithFields(logrus.Fields{
			"eventId":   event.ID,
			"eventKind": event.Kind,
			"appId":     app.ID,
		}).Infof("Failed to fetch user info: %v", err)
		nostrEvent.State = NOSTR_EVENT_STATE_HANDLER_ERROR
		svc.db.Save(&nostrEvent)
		return svc.createResponse(event, nip47.Nip47Response{
			ResultType: request.Method,
			Error: &nip47.Nip47Error{
				Code:    NIP_47_ERROR_INTERNAL,
				Message: fmt.Sprintf("Invalid params: %s", err.Error()),
			},
		}, ss)
	}

	svc.Logger.WithFields(logrus.Fields{
		"eventId":   event.ID,
		"eventKind": event.Kind,
		"appId":     app.ID,
	}).Info("Fetching user")

	response, err := svc.lnClient.LookupUser(ctx, event.PubKey, lookupUserParams.Lud16)
	if err != nil {
		svc.Logger.WithFields(logrus.Fields{
			"eventId":   event.ID,
			"eventKind": event.Kind,
			"appId":     app.ID,
		}).Infof("Failed to fetch user info: %v", err)
		nostrEvent.State = NOSTR_EVENT_STATE_HANDLER_ERROR
		svc.db.Save(&nostrEvent)
		return svc.createResponse(event, nip47.Nip47Response{
			ResultType: request.Method,
			Error: &nip47.Nip47Error{
				Code:    NIP_47_ERROR_INTERNAL,
				Message: fmt.Sprintf("Something went wrong while fetching user info: %s", err.Error()),
			},
		}, ss)
	}

	nostrEvent.State = NOSTR_EVENT_STATE_HANDLER_EXECUTED
	svc.db.Save(&nostrEvent)
	return svc.createResponse(event, nip47.Nip47Response{
		ResultType: request.Method,
		Result:     response,
	}, ss)
}
