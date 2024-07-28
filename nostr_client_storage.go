package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-oauth2/oauth2/v4"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip05"
	"github.com/nbd-wtf/go-nostr/nip19"
	log "github.com/sirupsen/logrus"
	"strings"
)

const (
	NIP_05_VERIFICATION_STATE_NONE     = "none"
	NIP_05_VERIFICATION_STATE_VERIFIED = "verified"
	NIP_05_VERIFICATION_STATE_INVALID  = "invalid"
)

type NostrClientStore struct {
}

// GetByID according to the ID for the client information
func (cs NostrClientStore) GetByID(ctx context.Context, id string) (oauth2.ClientInfo, error) {
	parts := strings.Split(id, " ")
	if len(parts) != 2 {
		return nil, errors.New("invalid public key. Should be in the format <npub> <relay>")
	}
	hexPubKey := parts[0]
	relayUrl := parts[1]
	if !strings.HasPrefix(relayUrl, "wss://") && !strings.HasPrefix(relayUrl, "ws://") {
		return nil, errors.New("invalid relay url")
	}

	if strings.HasPrefix(hexPubKey, "npub") {
		_, decodedPubkey, err := nip19.Decode(hexPubKey)
		if err != nil {
			return nil, err
		}
		hexPubKey = decodedPubkey.(string)
	}

	if !nostr.IsValidPublicKeyHex(hexPubKey) {
		return nil, errors.New("invalid public key")
	}

	relay, err := nostr.RelayConnect(ctx, relayUrl)
	if err != nil {
		return nil, err
	}

	filters := []nostr.Filter{{
		Kinds:   []int{nostr.KindProfileMetadata},
		Authors: []string{hexPubKey},
		Limit:   1,
	}}
	sub, err := relay.Subscribe(ctx, filters)
	if err != nil {
		return nil, err
	}

	// Wait for the first event
	event, ok := <-sub.Events
	if !ok {
		return nil, errors.New("not found")
	}

	valid, err := event.CheckSignature()
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, errors.New("invalid signature")
	}

	profile := make(map[string]interface{})
	err = json.Unmarshal([]byte(event.Content), &profile)
	if err != nil {
		return nil, err
	}

	imageUrl := profile["picture"].(string)
	nip05Address := profile["nip05"].(string)
	var domain *string
	if nip05Address != "" {
		if strings.Contains(nip05Address, "@") {
			domain = &strings.Split(nip05Address, "@")[1]
		} else {
			domain = &nip05Address
		}
	}
	displayName := profile["display_name"].(string)
	nip05res, err := nip05.QueryIdentifier(ctx, nip05.NormalizeIdentifier(nip05Address))
	if err != nil {
		log.Errorf("Failed to query nip05: %v", err)
		return nil, err
	}
	nip05VerificationState := NIP_05_VERIFICATION_STATE_NONE
	if nip05res != nil {
		if nip05res.PublicKey == hexPubKey {
			nip05VerificationState = NIP_05_VERIFICATION_STATE_VERIFIED
		} else {
			nip05VerificationState = NIP_05_VERIFICATION_STATE_INVALID
		}
	}
	log.Infof("NIP05 verification state: %s", nip05VerificationState)

	return &NostrClientInfo{
		Npub:                   hexPubKey,
		Relay:                  relayUrl,
		Name:                   profile["name"].(string),
		ImageUrl:               &imageUrl,
		Nip05Domain:            domain,
		Nip05VerificationState: nip05VerificationState,
		DisplayName:            &displayName,
	}, nil
}

func (cs NostrClientStore) Set(id string, cli oauth2.ClientInfo) (err error) {
	// No-op
	return
}

type NostrClientInfo struct {
	Npub                   string
	Relay                  string
	Name                   string
	ImageUrl               *string
	Nip05Domain            *string
	Nip05VerificationState string
	DisplayName            *string
}

func (ci *NostrClientInfo) GetID() string {
	return ci.Npub
}

func (ci *NostrClientInfo) GetSecret() string {
	return ""
}

func (ci *NostrClientInfo) GetDomain() string {
	if ci.Nip05Domain == nil {
		return ""
	}
	return *ci.Nip05Domain
}

func (ci *NostrClientInfo) IsPublic() bool {
	return true
}

func (ci *NostrClientInfo) GetUserID() string {
	return ""
}
