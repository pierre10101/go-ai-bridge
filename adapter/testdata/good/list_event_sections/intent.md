# Intent: List event sections

## Why
Visitors browse one page of an event's sections (name, capacity) together
with the event's title, newest section first, without loading every section
at once.

## Who
Anyone, signed in or not (Public).

## Inputs
- `event_id` — path `{id}`; the event whose sections to list.
- `after` — query; keyset cursor: only sections with a lower `id` than this.
  Leave it out for the newest page; for the next page, send the previous
  answer's `next_after` (stop when that is 0 — do not send `after=0`).
- `limit` — query; page size, at most the domain's maximum page size; leave
  it out for the default page size (both in bridge-en's runtime/page).

## Outputs
- `sections` — a list (possibly empty) of sections with the event's title,
  newest first.
- `next_after` — the cursor of the next page: the last listed `id` when the
  page is full, 0 when there is no next page.

## Failure cases
- F1: `limit` is outside 1 to the maximum page size.
- F2: `after` is zero or negative (omit it for the first page instead).

## Out of scope
Adding or renaming sections; filtering by capacity.
