# Intent: Admin rename event

## Why
An admin fixes the title of any event, whoever owns it (for example an
offensive title). Only admins may do this: the app declares that the
`admin` role bypasses ownership, so this action is not limited to events
the admin owns.

## Who
Signed-in users with role `admin`. Anyone else is refused before the action
runs: HTTP 401 when not signed in, HTTP 403 for any other role (an
organizer renames their own events with Rename event).

## Inputs
- `event_id` — the event to rename.
- `title` — its new title, not empty.

## Outputs
- `event_id` — the renamed event.
- `title` — its new title.

## Failure cases
- F1: the title is empty: nothing is written.
- F2: the event does not exist: nothing is written.

## Out of scope
Moving an event to another organizer.
