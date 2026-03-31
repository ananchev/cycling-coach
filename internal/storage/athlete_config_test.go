package storage

import (
	"database/sql"
	"errors"
	"testing"
)

func TestSetConfig_InsertAndGet(t *testing.T) {
	db := openTestDB(t)

	if err := SetConfig(db, "ftp_watts", "251"); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}

	val, err := GetConfig(db, "ftp_watts")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if val != "251" {
		t.Errorf("value = %q, want %q", val, "251")
	}
}

func TestSetConfig_UpdatesOnConflict(t *testing.T) {
	db := openTestDB(t)

	if err := SetConfig(db, "ftp_watts", "240"); err != nil {
		t.Fatalf("first SetConfig: %v", err)
	}
	if err := SetConfig(db, "ftp_watts", "251"); err != nil {
		t.Fatalf("second SetConfig: %v", err)
	}

	val, err := GetConfig(db, "ftp_watts")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if val != "251" {
		t.Errorf("value after update = %q, want %q", val, "251")
	}
}

func TestGetConfig_NotFound(t *testing.T) {
	db := openTestDB(t)
	_, err := GetConfig(db, "nonexistent_key")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestGetAllConfig(t *testing.T) {
	db := openTestDB(t)

	pairs := map[string]string{
		"ftp_watts": "251",
		"hr_max":    "184",
		"weight_kg": "90.5",
	}
	for k, v := range pairs {
		if err := SetConfig(db, k, v); err != nil {
			t.Fatalf("SetConfig %s: %v", k, err)
		}
	}

	all, err := GetAllConfig(db)
	if err != nil {
		t.Fatalf("GetAllConfig: %v", err)
	}
	if len(all) != len(pairs) {
		t.Errorf("got %d entries, want %d", len(all), len(pairs))
	}
	for k, want := range pairs {
		if got := all[k]; got != want {
			t.Errorf("all[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestGetAllConfig_EmptyDB(t *testing.T) {
	db := openTestDB(t)
	all, err := GetAllConfig(db)
	if err != nil {
		t.Fatalf("GetAllConfig on empty DB: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty map, got %d entries", len(all))
	}
}
