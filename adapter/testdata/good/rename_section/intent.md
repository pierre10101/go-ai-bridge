# Intent: Rename section

## Why
An organizer renames a section of one of their own events. A section
belongs to its event's organizer: an organizer can rename only sections of
events they own, never another organizer's, even by sending that
section's id.

## Who
Signed-in users with role `organizer`. Anyone else is refused before the
action runs: HTTP 401 when not signed in, HTTP 403 for any other role.
Admins rename any section with Admin rename section instead.

## Inputs
- `section_id` — the section to rename.
- `name` — its new name, not empty.

The signed-in user is set by the server; a request that sends `user` is a
bad request (HTTP 400).

## Outputs
- `section_id` — the renamed section.
- `name` — its new name.

## Failure cases
- F1: the name is empty: nothing is written.
- F2: the section does not exist, or it belongs to an event the signed-in
  user does not own: nothing is written, and the answer is the same in
  both cases.

## Out of scope
Moving a section to another event.
