package main

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/getAlby/nostr-wallet-connect/nip47"
	"github.com/go-oauth2/oauth2/v4"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip05"
	"github.com/nbd-wtf/go-nostr/nip19"
	log "github.com/sirupsen/logrus"
	"strings"
)

type Nip05VerificationState string

const (
	NIP_05_VERIFICATION_STATE_NONE     Nip05VerificationState = "none"
	NIP_05_VERIFICATION_STATE_VERIFIED Nip05VerificationState = "verified"
	NIP_05_VERIFICATION_STATE_INVALID  Nip05VerificationState = "invalid"
)

const NIP_68_IDENTITY_KIND = 13195

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

	filter := nostr.Filter{
		Kinds:   []int{nostr.KindProfileMetadata, NIP_68_IDENTITY_KIND},
		Authors: []string{hexPubKey},
		Limit:   2,
	}
	events, err := relay.QuerySync(ctx, filter)
	if err != nil {
		return nil, err
	}

	profile := make(map[string]interface{})
	appIdentity := &nip47.Nip68AppIdentity{}
	for _, event := range events {
		valid, err := event.CheckSignature()
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, errors.New("invalid signature")
		}

		if event.Kind == nostr.KindProfileMetadata {
			err = json.Unmarshal([]byte(event.Content), &profile)
			if err != nil {
				return nil, err
			}
		} else if event.Kind == NIP_68_IDENTITY_KIND {
			err = json.Unmarshal([]byte(event.Content), appIdentity)
			if err != nil {
				return nil, err
			}
		}
	}

	if len(profile) == 0 {
		return nil, errors.New("no profile metadata found")
	}

	// Prefer using app identity if present:
	if appIdentity != nil {
		return cs.lookupByAppIdentity(ctx, hexPubKey, relayUrl, *appIdentity)
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
	nip05VerificationState, err := cs.verifyNip05(ctx, &nip05Address, hexPubKey)
	if err != nil {
		return nil, err
	}
	log.Infof("NIP05 verification state: %s", *nip05VerificationState)

	return &NostrClientInfo{
		Npub:                   hexPubKey,
		Relay:                  relayUrl,
		Name:                   profile["name"].(string),
		ImageUrl:               &imageUrl,
		Nip05Domain:            domain,
		Nip05VerificationState: *nip05VerificationState,
		DisplayName:            &displayName,
	}, nil
}

func (cs NostrClientStore) verifyNip05(ctx context.Context, nip05Address *string, hexPubKey string) (*Nip05VerificationState, error) {
	if nip05Address == nil || *nip05Address == "" {
		verificationState := NIP_05_VERIFICATION_STATE_NONE
		return &verificationState, nil
	}

	nip05res, err := nip05.QueryIdentifier(ctx, nip05.NormalizeIdentifier(*nip05Address))
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
	return &nip05VerificationState, nil
}

func (cs NostrClientStore) lookupByAppIdentity(ctx context.Context, hexPubKey string, relayUrl string, appIdentity nip47.Nip68AppIdentity) (oauth2.ClientInfo, error) {
	appIdentityStr, err := json.Marshal(appIdentity)
	log.Infof("Using app identity for client info: %s", appIdentityStr)
	nip05Address := appIdentity.Nip05
	var domain *string
	if nip05Address != nil {
		if strings.Contains(*nip05Address, "@") {
			domain = &strings.Split(*nip05Address, "@")[1]
		} else {
			domain = nip05Address
		}
	}
	nip05VerificationState, err := cs.verifyNip05(ctx, nip05Address, hexPubKey)
	if err != nil {
		return nil, err
	}
	log.Infof("NIP05 verification state: %s", *nip05VerificationState)

	// TODO: Look for labels from known authorities. There are none yet :-p.

	return &NostrClientInfo{
		Npub:                   hexPubKey,
		Relay:                  relayUrl,
		Name:                   appIdentity.Name,
		ImageUrl:               appIdentity.Image,
		Nip05Domain:            domain,
		Nip05VerificationState: *nip05VerificationState,
		DisplayName:            &appIdentity.Name,
		AllowedRedirectUris:    appIdentity.AllowedRedirectUris,
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
	Nip05VerificationState Nip05VerificationState
	DisplayName            *string
	AllowedRedirectUris    []string
}

func (ci *NostrClientInfo) GetID() string {
	return ci.Npub
}

func (ci *NostrClientInfo) GetSecret() string {
	return ""
}

func (ci *NostrClientInfo) GetDomain() string {
	if ci.AllowedRedirectUris != nil && len(ci.AllowedRedirectUris) > 0 {
		return ci.AllowedRedirectUris[0]
	}

	if ci.Nip05Domain != nil {
		return *ci.Nip05Domain
	}
	return ""
}

func (ci *NostrClientInfo) IsPublic() bool {
	return true
}

func (ci *NostrClientInfo) GetUserID() string {
	return ""
}
