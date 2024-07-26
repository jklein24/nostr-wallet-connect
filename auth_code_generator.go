package main

import (
	"context"
	"errors"
	"github.com/go-oauth2/oauth2/v4"
	"github.com/go-oauth2/oauth2/v4/generates"
	"gorm.io/gorm"
)

type AuthCodeGenerator struct {
	baseGenerator *generates.AuthorizeGenerate
	db            *gorm.DB
}

func NewAuthCodeGenerator(db *gorm.DB) *AuthCodeGenerator {
	return &AuthCodeGenerator{
		baseGenerator: generates.NewAuthorizeGenerate(),
		db:            db,
	}
}

func (acg *AuthCodeGenerator) Token(ctx context.Context, data *oauth2.GenerateBasic) (code string, err error) {
	code, err = acg.baseGenerator.Token(ctx, data)
	if err != nil {
		return "", err
	}
	queryParams := data.Request.URL.Query()
	appId := queryParams.Get("app_id")
	if appId == "" {
		return "", errors.New("missing app_id")
	}

	// Save the code to the database for this pending connection so that the connection can be activated later and
	// returned to the user in the token response.
	acg.db.Table("apps").Where("id = ?", appId).Update("auth_code", code)

	return code, nil
}
