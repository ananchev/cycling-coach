package fit_test

import (
	"testing"
	"time"

	fitpkg "cycling-coach/internal/fit"
)

const sampleFIT = "../../testdata/sample.fit"

func TestParseFile_ReturnsActivity(t *testing.T) {
	parsed, err := fitpkg.ParseFile(sampleFIT)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if len(parsed.Records) == 0 {
		t.Fatal("expected at least one record")
	}
	if parsed.Session.DurationSec <= 0 {
		t.Errorf("DurationSec = %f, want > 0", parsed.Session.DurationSec)
	}
}

func TestParseFile_RecordsHaveTimestamps(t *testing.T) {
	parsed, err := fitpkg.ParseFile(sampleFIT)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	zero := time.Time{}
	for i, r := range parsed.Records {
		if r.Timestamp == zero {
			t.Errorf("record[%d] has zero timestamp", i)
			break
		}
	}
}

func TestParseFile_SessionHasStartTime(t *testing.T) {
	parsed, err := fitpkg.ParseFile(sampleFIT)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	if parsed.Session.StartTime.IsZero() {
		t.Error("Session.StartTime is zero")
	}
}

func TestParseFile_NonExistentFile(t *testing.T) {
	_, err := fitpkg.ParseFile("/nonexistent/path.fit")
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
}

func TestNormalizedPower_ReturnsZeroForFewRecords(t *testing.T) {
	records := make([]fitpkg.Record, 10)
	for i := range records {
		records[i].Power = 200
	}
	np := fitpkg.NormalizedPower(records)
	if np != 0 {
		t.Errorf("NormalizedPower with <30 records = %f, want 0", np)
	}
}

func TestNormalizedPower_ConstantPower(t *testing.T) {
	// With constant power, NP should equal mean power.
	records := make([]fitpkg.Record, 100)
	for i := range records {
		records[i].Power = 200
	}
	np := fitpkg.NormalizedPower(records)
	// Allow ±1 watt tolerance for rolling window edge effects.
	if np < 199 || np > 201 {
		t.Errorf("NormalizedPower for constant 200W = %f, want ~200", np)
	}
}

func TestFilterRecords_ValidPower(t *testing.T) {
	records := []fitpkg.Record{
		{Power: 0},
		{Power: 150},
		{Power: 0},
		{Power: 200},
	}
	filtered := fitpkg.FilterRecords(records, func(r fitpkg.Record) bool { return r.ValidPower() })
	if len(filtered) != 2 {
		t.Errorf("FilterRecords = %d records, want 2", len(filtered))
	}
}

func TestMeanPower_IgnoresZeros(t *testing.T) {
	records := []fitpkg.Record{
		{Power: 0},
		{Power: 100},
		{Power: 200},
		{Power: 300},
	}
	mean := fitpkg.MeanPower(records)
	// 3 valid records: (100+200+300)/3 = 200
	if mean != 200.0 {
		t.Errorf("MeanPower = %f, want 200.0", mean)
	}
}

func TestMeanPower_NoValidRecords(t *testing.T) {
	records := []fitpkg.Record{{Power: 0}, {Power: 0}}
	mean := fitpkg.MeanPower(records)
	if mean != 0 {
		t.Errorf("MeanPower with no valid records = %f, want 0", mean)
	}
}
