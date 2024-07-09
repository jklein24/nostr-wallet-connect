package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

var _202407081333_add_oauth_tokens = &gormigrate.Migration{
	ID: "202407081333_add_oauth_tokens",
	Migrate: func(tx *gorm.DB) error {
		return tx.Exec("CREATE INDEX IF NOT EXISTS idx_nostr_events_app_id_and_id ON nostr_events(app_id, id)").Error
	},
	Rollback: func(tx *gorm.DB) error {
		return nil
	},
}
