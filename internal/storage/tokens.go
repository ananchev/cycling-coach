package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// WahooToken represents the single OAuth token row for the Wahoo integration.
// There is always at most one row (id = 1).
type WahooToken struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	UpdatedAt    time.Time
}

// SaveToken upserts the Wahoo OAuth token. Only one token row exists (id = 1).
func SaveToken(db *sql.DB, t *WahooToken) error {
	_, err := db.Exec(`
		INSERT INTO wahoo_tokens(id, access_token, refresh_token, expires_at)
		VALUES(1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			access_token=excluded.access_token,
			refresh_token=excluded.refresh_token,
			expires_at=excluded.expires_at,
			updated_at=CURRENT_TIMESTAMP`,
		t.AccessToken, t.RefreshToken, t.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("storage.SaveToken: %w", err)
	}
	return nil
}

// GetToken returns the stored Wahoo token, or sql.ErrNoRows if none exists.
func GetToken(db *sql.DB) (*WahooToken, error) {
	var t WahooToken
	err := db.QueryRow(`
		SELECT access_token, refresh_token, expires_at, updated_at
		FROM wahoo_tokens WHERE id = 1`,
	).Scan(&t.AccessToken, &t.RefreshToken, &t.ExpiresAt, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("storage.GetToken: %w", err)
	}
	return &t, nil
}
