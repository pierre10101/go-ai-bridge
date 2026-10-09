package domain

// InvoiceSummary is one invoice on a list page (number + total).
//
// bridge-en: an invoice summary
type InvoiceSummary struct {
	InvoiceNumber InvoiceNumber `json:"invoice_number"`
	Total         Money         `json:"total"`
}
