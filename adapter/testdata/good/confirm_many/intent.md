# Intent: Confirm many

## Why
A visitor holds several tickets of an event for a while, then buys them all
at once. Either every requested ticket is sold to them, or none is: a
basket where one ticket can no longer be sold must not end up half bought.

## Who
The visitor whose session cookie identifies them (no login in this example).

## Inputs
- `ticket_ids` — the tickets to buy: 1 to 20 ticket ids, each at most once.
  An empty list, more than 20, a repeated id or an id that is not a whole
  number is refused before anything runs (HTTP 400).
- `session` — the visitor's session, set by the server from the session
  cookie; the empty text when there is no valid cookie. The caller never
  sends it.
- `now` — the current time, set by the server (whole seconds since 1970).

## Outputs
- `confirmed` — how many tickets were sold: always the number of ids sent.

A ticket can be sold to this session when, at that moment, this session
holds it and its hold expires later than now: a hold whose `expires_at` is
exactly now has expired; one that expires one second after now has not.

## Failure cases
- **F1** — there is no valid session cookie (the session is empty): nothing
  is written.
- **F2** — at least one requested ticket is held by this session but its hold
  has expired (it expires now or earlier): no ticket is sold, every change
  is rolled back.
- **F3** — at least one requested ticket is not held by this session (held by
  someone else, free, already sold, or no such ticket): no ticket is sold,
  every change is rolled back.

## Out of scope
Taking and releasing holds; payment.
