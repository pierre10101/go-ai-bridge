# Intent: Claim example

## Why
A visitor holds a seat for a while before confirming it. Two visitors must
never hold the same seat, and a hold that is not confirmed in time frees the
seat for someone else.

## Who
The visitor whose session cookie identifies them (no login in this example).
Who holds the seat is always that session, never an id sent in the request.

## Inputs
- `seat_id` — the seat to hold.
- `session` — the visitor's session, set by the server from the session
  cookie; 0 when there is no valid cookie. The caller never sends it.
- `now` — the current time, set by the server (whole seconds since 1970).

## Outputs
- `seat_id`, `now` — the seat that was held and the server's current time.
  The hold expires 10 minutes (600 seconds) after `now`.

The seat can be claimed when it is free, or when its current hold has
expired: a hold whose `expires_at` is exactly now has expired and no longer
blocks the seat; one that expires one second after now still does.

## Failure cases
- F1: the seat is held (by any session, this one included) and its hold
  expires later than now, or the seat does not exist: nothing changed.
- F2: there is no valid session cookie (the session is 0): nothing is
  written.

## Out of scope
Confirming or releasing a hold.
