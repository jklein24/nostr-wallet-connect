package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"github.com/nbd-wtf/go-nostr"
	"gorm.io/gorm"
	"strconv"
	"strings"

	"github.com/go-oauth2/oauth2/v4"
	"github.com/google/uuid"
)

// NewAccessTokenGenerator create to generate the access token instance
func NewAccessTokenGenerator(db *gorm.DB) *AccessTokenGenerator {
	return &AccessTokenGenerator{
		db: db,
	}
}

// AccessTokenGenerator generate the access token
type AccessTokenGenerator struct {
	db *gorm.DB
}

// Token based on the UUID generated token
func (ag *AccessTokenGenerator) Token(ctx context.Context, data *oauth2.GenerateBasic, isGenRefresh bool) (string, string, error) {
	buf := bytes.NewBufferString(data.Client.GetID())
	buf.WriteString(data.UserID)
	buf.WriteString(strconv.FormatInt(data.CreateAt.UnixNano(), 10))
	refresh := ""
	if isGenRefresh {
		refresh = base64.URLEncoding.EncodeToString([]byte(uuid.NewSHA1(uuid.Must(uuid.NewRandom()), buf.Bytes()).String()))
		refresh = strings.ToUpper(strings.TrimRight(refresh, "="))
	}

	code := data.Request.FormValue("code")
	if code != "" {
		app := &App{}
		err := ag.db.Table("apps").First(app, &App{
			AuthCode: code,
		}).Error
		if err != nil {
			return "", "", err
		}
		access := app.NostrSecretKey
		return access, refresh, nil
	}

	secret := data.TokenInfo.GetAccess()
	app := &App{}
	err := ag.db.Table("apps").First(app, &App{
		NostrSecretKey: secret,
	}).Error
	if err != nil {
		return "", "", err
	}

	access := nostr.GeneratePrivateKey()
	pubkey, err := nostr.GetPublicKey(access)
	if err != nil {
		return "", "", err
	}

	app.NostrPubkey = pubkey
	app.NostrSecretKey = access
	err = ag.db.Table("apps").Save(app).Error
	if err != nil {
		return "", "", err
	}

	return access, refresh, nil
}
