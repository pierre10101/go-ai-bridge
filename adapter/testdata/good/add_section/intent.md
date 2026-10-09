# Intent: Add section

## Why
An organizer adds a section (a named block of seats with a capacity) to
one of their own events. A section belongs to its event, so to the event's
organizer: an organizer can add sections only to events they own, never to
another organizer's, even by sending that event's id.

## Who
Signed-in users with role `organizer`. Anyone else is refused before the
action runs: HTTP 401 when not signed in, HTTP 403 for any other role.

## Inputs
- `event_id` — the event to add the section to.
- `name` — the section's name, not empty.
- `capacity` — how many seats it has, at least 1.

The signed-in user is set by the server; a request that sends `user` is a
bad request (HTTP 400).

## Outputs
- `event_id` — the event the section was added to.
- `name` — the new section's name.

## Failure cases
- F1: the name is empty: nothing is written.
- F2: the capacity is less than 1: nothing is written.
- F3: the event does not exist, or the signed-in user does not own it
  (another organizer's event): no section is added, and the answer is the
  same in both cases, so it does not tell whether the event exists.

## Out of scope
Moving a section to another event, removing sections.
