package storage

import (
	"database/sql"
	"fmt"
)

// SetConfig upserts a key-value entry in athlete_config.
func SetConfig(db *sql.DB, key, value string) error {
	_, err := db.Exec(`
		INSERT INTO athlete_config(key, value)
		VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET
			value=excluded.value,
			updated_at=CURRENT_TIMESTAMP`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("storage.SetConfig: %w", err)
	}
	return nil
}

// GetConfig returns the value for the given key, or sql.ErrNoRows if not found.
func GetConfig(db *sql.DB, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM athlete_config WHERE key = ?`, key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("storage.GetConfig: %w", err)
	}
	return value, nil
}

// GetAllConfig returns all key-value entries as a map.
func GetAllConfig(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT key, value FROM athlete_config`)
	if err != nil {
		return nil, fmt.Errorf("storage.GetAllConfig: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("storage.GetAllConfig: scan: %w", err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage.GetAllConfig: rows: %w", err)
	}
	return out, nil
}
