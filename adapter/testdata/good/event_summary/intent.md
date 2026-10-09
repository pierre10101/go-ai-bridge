# Intent: Event summary

## Why
Any visitor's event page shows how many sections the event has, whether
the visitor organizes it (to show an edit button), and with which role
they are signed in. A visitor who is not signed in sees `yours` 0 and no
role, so the page needs no separate signed-out path; this is a public page,
so it needs no sign-in at all.

## Who
Anyone, signed in or not.

## Inputs
- `event_id` — the event, from the path.

The signed-in user and their role are set by the server (0 and the empty
text when not signed in); a request that sends `user` or `role` in the
query string is a bad request (HTTP 400).

## Outputs
- `event_id` — the event.
- `sections` — how many sections it has (0 for an event that does not
  exist).
- `yours` — 1 if the signed-in user organizes it, otherwise 0.
- `viewer_role` — the signed-in user's role, or the empty text.

## Failure cases
None.
