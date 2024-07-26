package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
	"log"
)

var _202407241504_add_oauth_codes = &gormigrate.Migration{
	ID: "202407241504_add_oauth_codes",
	Migrate: func(tx *gorm.DB) error {
		var sql string
		if tx.Dialector.Name() == "postgres" {
			sql = "ALTER TABLE apps ADD COLUMN auth_code TEXT, ADD COLUMN nostr_secret_key TEXT"
		} else if tx.Dialector.Name() == "sqlite" {
			// In sqlite dialect:
			sql = "ALTER TABLE `apps` ADD COLUMN `auth_code` TEXT; ALTER TABLE `apps` ADD COLUMN `nostr_secret_key` TEXT"
		} else {
			log.Fatalf("unsupported database type: %s", tx.Dialector.Name())
		}
		return tx.Exec(sql).Error
	},
	Rollback: func(tx *gorm.DB) error {
		return nil
	},
}
