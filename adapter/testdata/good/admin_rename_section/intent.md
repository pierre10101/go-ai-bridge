# Intent: Admin rename section

## Why
An admin fixes the name of any section, whoever organizes its event (for
example a support request). Only admins may do this; organizers rename
sections of their own events with Rename section.

## Who
Signed-in users with role `admin`, which bypasses ownership. Anyone else is
refused before the action runs: HTTP 401 when not signed in, HTTP 403 for
any other role.

## Inputs
- `section_id` — the section to rename.
- `name` — its new name, not empty.

## Outputs
- `section_id` — the renamed section.
- `name` — its new name.

## Failure cases
- F1: the name is empty: nothing is written.
- F2: there is no such section: nothing is written.

## Out of scope
Moving a section to another event.
