# Intent: Admin delete section

## Why
An admin removes any section, whoever organizes its event (for example a
duplicate created by mistake). Only admins may do this; organizers delete
sections of their own events with Delete section.

## Who
Signed-in users with role `admin`, which bypasses ownership. Anyone else is
refused before the action runs: HTTP 401 when not signed in, HTTP 403 for
any other role.

## Inputs
- `section_id` — the section to delete.

## Outputs
- `section_id` — the deleted section.

## Failure cases
- F1: there is no such section: nothing is deleted.

## Out of scope
Deleting the section's seats with it (schema.sql restricts the delete
while it has seats).
