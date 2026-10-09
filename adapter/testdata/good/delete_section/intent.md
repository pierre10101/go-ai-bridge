# Intent: Delete section

## Why
An organizer removes a section from one of their own events. A section
belongs to its event's organizer: an organizer can delete only sections of
events they own, never another organizer's, even by sending that
section's id. A section that still has seats is kept: its seats must be
removed first.

## Who
Signed-in users with role `organizer`. Anyone else is refused before the
action runs: HTTP 401 when not signed in, HTTP 403 for any other role.
Admins delete any section with Admin delete section instead.

## Inputs
- `section_id` — the section to delete.

The signed-in user is set by the server; a request that sends `user` is a
bad request (HTTP 400).

## Outputs
- `section_id` — the deleted section.

## Failure cases
- F1: the section does not exist, or it belongs to an event the signed-in
  user does not own: nothing is deleted, and the answer is the same in
  both cases.

## Out of scope
Deleting the section's seats with it (schema.sql restricts the delete
while it has seats: the delete fails with HTTP 500 and nothing is deleted).
