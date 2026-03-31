package storage

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestSaveToken_InsertAndGet(t *testing.T) {
	db := openTestDB(t)

	tok := &WahooToken{
		AccessToken:  "access-abc",
		RefreshToken: "refresh-xyz",
		ExpiresAt:    time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC),
	}

	if err := SaveToken(db, tok); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	got, err := GetToken(db)
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got.AccessToken != "access-abc" {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, "access-abc")
	}
	if got.RefreshToken != "refresh-xyz" {
		t.Errorf("RefreshToken = %q, want %q", got.RefreshToken, "refresh-xyz")
	}
}

func TestSaveToken_UpdatesExisting(t *testing.T) {
	db := openTestDB(t)

	tok := &WahooToken{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := SaveToken(db, tok); err != nil {
		t.Fatalf("first SaveToken: %v", err)
	}

	tok.AccessToken = "new-access"
	tok.RefreshToken = "new-refresh"
	if err := SaveToken(db, tok); err != nil {
		t.Fatalf("second SaveToken: %v", err)
	}

	got, err := GetToken(db)
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if got.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, "new-access")
	}
}

func TestGetToken_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := GetToken(db)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}
