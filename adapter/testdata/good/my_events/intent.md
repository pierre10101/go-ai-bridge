# Intent: My events

## Why
An organizer's dashboard shows how many events they organize and how many
sections those events have in total. Only their own: another organizer's
events and sections are never counted.

## Who
Signed-in users with role `organizer`. Anyone else is refused before the
action runs: HTTP 401 when not signed in, HTTP 403 for any other role.

## Inputs
None from the caller. The signed-in user is set by the server; a request
that sends `user` in the query string is a bad request (HTTP 400).

## Outputs
- `events` — how many events the signed-in user organizes.
- `sections` — how many sections those events have.

## Failure cases
None.
