package ble

// scanner holds the single registered BLEScanner. Mirrors
// core/engine/registry.go's self-registration pattern (AD-6 precedent) so a
// future Android companion-app bridge (AD-3, deferred) can register a
// second BLEScanner implementation later without core/ble changing at all.
var scanner BLEScanner

func RegisterScanner(s BLEScanner) {
	scanner = s
}

func GetScanner() (BLEScanner, bool) {
	return scanner, scanner != nil
}
