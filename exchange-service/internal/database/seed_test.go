package database

import "testing"

func TestSeedCatalogHasExpectedCoverage(t *testing.T) {
	exchanges := seedExchanges()
	listings := seedListings()

	if len(exchanges) < 3 {
		t.Fatalf("expected at least 3 exchanges, got %d", len(exchanges))
	}
	if len(listings) < 10 {
		t.Fatalf("expected at least 10 listings, got %d", len(listings))
	}
}

func TestBuildSeedHistory_IsDeterministicAndThirtyDaysLong(t *testing.T) {
	first := buildSeedHistory("AAPL", 214.33, 68123412)
	second := buildSeedHistory("AAPL", 214.33, 68123412)

	if len(first) != 30 {
		t.Fatalf("expected 30 history entries, got %d", len(first))
	}
	if len(second) != 30 {
		t.Fatalf("expected 30 history entries on repeated build, got %d", len(second))
	}
	for i := range first {
		if !first[i].Date.Equal(second[i].Date) ||
			first[i].Price != second[i].Price ||
			first[i].High != second[i].High ||
			first[i].Low != second[i].Low ||
			first[i].Change != second[i].Change ||
			first[i].Volume != second[i].Volume {
			t.Fatalf("history mismatch at index %d", i)
		}
	}
}
