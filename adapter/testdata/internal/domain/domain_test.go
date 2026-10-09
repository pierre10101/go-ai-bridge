package domain

import "testing"

func TestInvoiceNumberFor(t *testing.T) {
	if got := InvoiceNumberFor(42); got != "INV-000042" {
		t.Fatalf("got %q", got)
	}
}

func TestIsValidInvoiceNumber(t *testing.T) {
	for n, want := range map[InvoiceNumber]bool{
		"INV-000001": true, "INV-999999": true, "INV-000000": false, "INV-00001": false, "INV-0000001": false,
		"XYZ-000001": false, "INV-00000a": false, "inv-000001": false, "INV-00000 ": false, "": false,
	} {
		if got := IsValidInvoiceNumber(n); got != want {
			t.Errorf("%q: got %v want %v", n, got, want)
		}
	}
}

func TestIsSupportedCurrency(t *testing.T) {
	if !IsSupportedCurrency("ZAR") || IsSupportedCurrency("zar") || IsSupportedCurrency("GBP") {
		t.Fatal("currency list changed")
	}
}

func TestInvoiceNumberForPanicsOnZero(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("want panic")
		}
	}()
	InvoiceNumberFor(0)
}
