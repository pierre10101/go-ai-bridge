# Intent: Rename event

## Why
An organizer fixes the title of one of their own events. The event belongs
to whoever created it (`organizer_id`, the signed-in user then): an
organizer can rename only events they own, never another organizer's, even
by sending that event's id.

## Who
Signed-in users with role `organizer`. Anyone else is refused before the
action runs: HTTP 401 when not signed in, HTTP 403 for any other role.
Admins rename any event with Admin rename event instead.

## Inputs
- `event_id` — the event to rename.
- `title` — its new title, not empty.

The signed-in user is set by the server; a request that sends `user` is a
bad request (HTTP 400).

## Outputs
- `event_id` — the renamed event.
- `title` — its new title.

## Failure cases
- F1: the title is empty: nothing is written.
- F2: the event does not exist, or the signed-in user does not own it
  (another organizer's event): nothing is written, and the answer is the
  same in both cases, so it does not tell whether the event exists.

## Out of scope
Moving an event to another organizer.
