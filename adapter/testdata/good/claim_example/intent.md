# Intent: Claim example

## Why
A person holds a seat for a while before confirming it. Two people must never
hold the same seat, and a hold that is not confirmed in time frees the seat
for someone else.

## Who
Anyone booking a seat (auth is out of scope for this example).

## Inputs
- `seat_id` — the seat to hold.
- `person_id` — who holds it (must be greater than zero).
- `now` — the current time, set by the server (whole seconds since 1970).

## Outputs
- `seat_id`, `held_by`, `held_at` — the hold that was taken.

The seat can be claimed when it is free, or when its current hold was taken
10 minutes or more before now: a hold taken exactly 10 minutes ago no longer
blocks the seat; a hold taken 9 minutes 59 seconds ago still does.

## Failure cases
- **F1** — the seat is held by a hold taken less than 10 minutes before now
  (or the seat does not exist): nothing changed.
- **F2** — `person_id` is zero or negative.

## Out of scope
Confirming or releasing a hold.
