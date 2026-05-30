package api

import "testing"

func TestDownloadCatalogHasPagedCategories(t *testing.T) {
	counts := map[string]int{}
	for _, item := range downloadCatalog {
		counts[item.Category]++
	}
	if counts["standard"] < 24 {
		t.Fatalf("standard catalog too small: %d", counts["standard"])
	}
	if counts["abliterated"] < 8 {
		t.Fatalf("abliterated catalog too small: %d", counts["abliterated"])
	}
}

func TestCatalogPaginationDefaults(t *testing.T) {
	if got := normalizeCatalogCategory(""); got != "standard" {
		t.Fatalf("category = %q", got)
	}
	if got := parseCatalogLimit(""); got != 12 {
		t.Fatalf("limit = %d", got)
	}
	if got := parseCatalogLimit("500"); got != 48 {
		t.Fatalf("max limit = %d", got)
	}
	if got := parseCatalogOffset("-1"); got != 0 {
		t.Fatalf("offset = %d", got)
	}
}
