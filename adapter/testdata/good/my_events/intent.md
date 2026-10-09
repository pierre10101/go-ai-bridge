# Intent: My events

## Why
Any visitor's page shows how many events they organize, and with which
role they are signed in. A visitor who is not signed in sees 0 and no
role, so the page needs no separate signed-out path.

## Who
Anyone, signed in or not.

## Inputs
None from the caller. The signed-in user and their role are set by the
server (0 and the empty text when not signed in); a request that sends
`user` or `role` in the query string is a bad request (HTTP 400).

## Outputs
- `events` — how many events the signed-in user organizes (0 when not
  signed in: no event has organizer 0).
- `role` — the signed-in user's role, or the empty text.

## Failure cases
None.
