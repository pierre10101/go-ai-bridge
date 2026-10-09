# Intent: Create event

## Why
An organizer puts a new event on sale. The event belongs to whoever is
signed in: the organizer is the signed-in user from the app's sign-in
session, never an id sent in the request.

## Who
Signed-in users with role `organizer` or `admin`. Anyone else is refused
before the action runs: HTTP 401 when not signed in, HTTP 403 for any other
role (for example a `customer`). Nothing is written then.

## Inputs
- `title` — the event's name, not empty.
- `starts_at` — when it starts, unix seconds; later than now.

The signed-in user and their role are set by the server; a request that
sends `user` or `role` is a bad request (HTTP 400).

## Outputs
- `event_id` — the new event.
- `organizer_id` — the signed-in user, who now owns the event.
- `created_as` — the role they created it with.

## Failure cases
- F1: the title is empty: nothing is written.
- F2: the event starts now or earlier (`starts_at` is no later than the
  current time): nothing is written.

## Out of scope
Signing in, passwords and sessions (the app does them), editing events.
