# Intent: List customer invoices

## Why
Finance staff need to see a customer's invoices, newest first, a page at a
time, so they can quote invoice numbers on the phone without loading every
row at once.

## Who
Signed-in users with role `finance` or `admin` (signing in is the app's,
out of scope here). Anyone else is refused before the action runs: HTTP 401
when not signed in, HTTP 403 for any other role.

## Inputs
- `customer_id` — path `{id}`; the customer whose invoices to list (must exist).
- `after` — query; keyset cursor: only invoices with a lower `seq` than this.
  Leave it out for the newest page; for the next page, send the previous
  answer's `next_after`.
- `limit` — query; page size, at most the domain's maximum page size; leave
  it out for the default page size (both in bridge-en's runtime/page).

## Outputs
- `invoices` — a list (possibly empty) of invoice summaries: `invoice_number`
  and `total` (cents + currency), newest first.
- `next_after` — the cursor of the next page: the last listed `seq` when the
  page is full, 0 when there is no next page.

Listing never writes and must not hold the write lock: it reads one snapshot
in a read-only transaction, so creating invoices is not blocked by it.

## Failure cases
- F1: the customer does not exist.
- F2: `limit` is outside 1 to the maximum page size.
- F3: `after` is zero or negative (omit it for the first page instead).

## Out of scope
Creating, editing or voiding invoices; filtering by date or amount; OFFSET paging.
