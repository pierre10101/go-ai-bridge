# Intent: Release example

## Why
A person who holds a seat can give it back before the hold runs out, so
someone else can take it. Only the session that holds the seat can free it.

## Who
The visitor whose session cookie identifies them (no login in this example).

## Inputs
- `seat_id` — the seat to release.
- `session` — the visitor's session, set by the server from the session
  cookie; 0 when there is no valid cookie. The caller never sends it.

## Outputs
- `seat_id` — the seat that was released.

## Failure cases
- F1: there is no valid session cookie (the session is 0).
- F2: the seat does not exist: nothing changed.
- F3: the seat exists but this session does not hold it (it is free or
  someone else holds it): nothing changed.

## Out of scope
Holding and confirming seats.
