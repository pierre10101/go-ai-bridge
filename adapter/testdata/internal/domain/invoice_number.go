package domain

import (
	"fmt"

	"github.com/pierre10101/go-ai-bridge/runtime/assert"
	"github.com/pierre10101/go-ai-bridge/runtime/shape"
)

// MaxInvoiceSeq is the largest sequence an InvoiceNumber can hold.
const MaxInvoiceSeq = 999_999

// InvoiceNumber is the human-facing invoice identifier, e.g. INV-000042.
//
// bridge-en: an invoice number
type InvoiceNumber string

// InvoiceNumberFor formats a database sequence as an InvoiceNumber.
func InvoiceNumberFor(seq int64) InvoiceNumber {
	assert.Pre(seq > 0, "invoice sequence is positive")
	assert.Pre(seq <= MaxInvoiceSeq, "invoice sequence fits six digits")
	return InvoiceNumber(fmt.Sprintf("INV-%06d", seq))
}

// IsValidInvoiceNumber reports whether n has the shape INV-dddddd and is not INV-000000.
func IsValidInvoiceNumber(n InvoiceNumber) bool {
	return shape.Has(string(n), "INV-######") && n != "INV-000000"
}
