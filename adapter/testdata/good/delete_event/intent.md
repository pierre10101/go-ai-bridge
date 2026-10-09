# Intent: Delete event

## Why
An organizer cancels one of their own events and removes it, with its
sections. An organizer can delete only events they own, never another
organizer's, even by sending that event's id. An event whose sections
still have seats is kept: the seats must be removed first.

## Who
Signed-in users with role `organizer`. Anyone else is refused before the
action runs: HTTP 401 when not signed in, HTTP 403 for any other role.

## Inputs
- `event_id` — the event to delete.

The signed-in user is set by the server; a request that sends `user` is a
bad request (HTTP 400).

## Outputs
- `event_id` — the deleted event.

## Failure cases
- F1: the event does not exist, or the signed-in user does not own it:
  nothing is deleted, and the answer is the same in both cases.

## Out of scope
Deleting seats (schema.sql restricts the delete while a section of the
event has seats: the delete fails with HTTP 500 and nothing is deleted).
