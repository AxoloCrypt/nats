package ble

// DeriveVendor resolves Vendor from the standardized Bluetooth SIG CompanyID
// in manufacturer-specific data when present (AD-5), and returns "unknown"
// otherwise — it never falls back to an OUI/MAC-prefix lookup, since many
// BLE devices randomize their advertised address (RPA/NRPA), decoupling it
// from any fixed OUI even though address randomization isn't universal.
// Unlike enrich/oui's convention (empty string on a lookup miss, papered
// over by a Writer later), DeriveVendor always returns a resolved value: a
// real vendor name or the literal string "unknown".
func DeriveVendor(adv Advertisement) string {
	if adv.CompanyID == nil {
		return "unknown"
	}

	vendor, ok := companyIDVendors[*adv.CompanyID]
	if !ok {
		return "unknown"
	}

	return vendor
}
