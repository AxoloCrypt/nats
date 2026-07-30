package ble

import "testing"

func TestDeriveVendor_KnownCompanyID(t *testing.T) {
	companyID := uint16(76) // Apple, Inc.
	got := DeriveVendor(Advertisement{CompanyID: &companyID})
	if got != "Apple, Inc." {
		t.Fatalf("expected \"Apple, Inc.\", got %q", got)
	}
}

func TestDeriveVendor_NilCompanyID(t *testing.T) {
	got := DeriveVendor(Advertisement{})
	if got != "unknown" {
		t.Fatalf("expected \"unknown\" for a nil CompanyID, got %q", got)
	}
}

func TestDeriveVendor_UnmappedCompanyID(t *testing.T) {
	companyID := uint16(0xFFFE) // not a real assigned company ID
	got := DeriveVendor(Advertisement{CompanyID: &companyID})
	if got != "unknown" {
		t.Fatalf("expected \"unknown\" for an unmapped CompanyID, got %q", got)
	}
}
