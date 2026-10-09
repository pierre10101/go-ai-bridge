# Intent: Create invoice

## Why
Finance staff need to bill a known customer for a single amount and get back a
stable, human-readable invoice number they can quote on the phone.

## Who
An authenticated finance user (auth is out of scope for this example).

## Inputs
- `customer_id` — the customer to bill (must already exist).
- `amount_cents` — the amount in minor units (cents), greater than zero.
- `currency` — ISO 4217 code; we invoice in ZAR, USD and EUR.

All three are required. A request that leaves one out, or sends it as null,
is a bad request (HTTP 400) and none of the failure cases below is checked.

## Outputs
- `invoice_number` — `INV-` followed by six digits, issued in sequence.
- `customer_id` and `total` (cents + currency) as stored.

## Failure cases
- F1: the customer does not exist.
- F2: the amount is zero or negative.
- F3: the currency is not one we invoice in.

## Out of scope
Line items, tax, discounts, editing or voiding invoices.
