# Legal pages and account deletion

LEDit ships the three pages a hosted, account-based service needs: a privacy
policy, terms of use, and an account-deletion page. They are public routes,
reachable without signing in, because a store reviewer and a prospective user
both need to read them before they have an account.

| Route | Template | Purpose |
| --- | --- | --- |
| `GET /privacy` | `web/templates/legal.html` (`privacy.html`) | What is stored, why, who processes it, retention, rights |
| `GET /terms` | `web/templates/legal.html` (`terms.html`) | Account rules, acceptable use, availability, liability |
| `GET /account/delete` | `web/templates/legal.html` (`account_delete.html`) | How to delete, and what happens to the data |

The sidebar links to all three under **System**.

## Configuration

The pages render neutral text with no configuration, but an instance that
collects real data should name its operator. All four values are environment
only (restart to apply), never database-backed:

```sh
LEGAL_CONTROLLER_NAME="Example Labs BV"
LEGAL_CONTACT_EMAIL=privacy@example.com
LEGAL_CONTACT_ADDRESS="1 Example Street, 1000 AA Amsterdam"
LEGAL_HOSTING_REGION="the Netherlands"
BASE_URL=https://ledit.example.com
```

- `LEGAL_CONTROLLER_NAME` is the GDPR "controller". Empty falls back to
  "the operator of this LEDit instance".
- `LEGAL_CONTACT_EMAIL` adds a `mailto:` contact and enables the email fallback
  on the deletion page. Empty removes both rather than inventing an address.
- `LEGAL_CONTACT_ADDRESS` is optional; a postal address is good practice but not
  mandatory in the text.
- `LEGAL_HOSTING_REGION` describes where the server runs, for the transfer
  paragraph. Empty falls back to "the European Union".
- `BASE_URL` is the public origin used for links in the page and in store
  metadata. It is not otherwise required by the legal handler.

The policy adapts to the deployment: the Umami paragraph appears only when
Umami is enabled, tracing only when an OTLP endpoint is configured, the SMTP
processor only when email settings have a host, and the OIDC paragraph only
when `OIDC_ENABLED` is true. Integration processors are listed as
*conditionally configured* rather than claimed as active. Google Calendar is
described as an operator-supplied private iCal URL: LEDit does not use Google
OAuth and receives no Google account profile or scopes.

## Account deletion

`POST /settings/account/delete` (browser session) and `DELETE /api/v1/me`
(session or bearer token) both call `internal/account.Service.Delete`, and both
require the username as a typed confirmation (`confirm` form field or JSON
body). In one transaction the service:

1. Verifies the typed confirmation against the username (`@username` is also
   accepted).
2. Refuses to delete the only admin, so the instance cannot be locked out.
3. Deletes every `WebSession` row for the user, which also clears the
   in-memory session cache.
4. Revokes any `ApiToken` whose `owner_id` matches the user.
5. Deletes the `User` row, which clears the password hash, email, OIDC subject
   and auth method in one step.

The SQLite write is a single transaction; a failure leaves the account intact.

### Preserved instance data

Devices, groups, playlists, schedules, scenes, themes, layouts, guest photos,
timelapse frames, delivery logs and notifications are not attached to a user
row. They are deliberately preserved: they belong to the instance and its
displays, not to one person's account.

### API and guest tokens

LEDit's `ApiToken` rows carry an `owner_id` from the API-token administration
table, not a foreign key to `User`. Deletion therefore revokes only tokens whose
`owner_id` happens to equal the deleting user's ID. Guest-remote tokens have no
owner column at all — they are instance credentials created by an administrator
and are not personal account data. Both kinds must be revoked in **Admin → API
tokens** / **Admin → Guest remote** when an operator wants to retire them. This
is stated on the deletion page rather than pretending one user's closure
revokes instance-wide credentials.

## Account export

`GET /api/v1/me/export` returns the resolved account profile (ID, username,
role, optional email, auth method, creation and last-login timestamps) and API
token metadata (ID, name, prefix, role, timestamps). Raw token secrets are
never stored and therefore never exported.

## Checklist for an EU launch

- [ ] Set all four `LEGAL_*` values and `BASE_URL`, then open `/privacy` and
      confirm they render.
- [ ] Confirm the public origin is HTTPS; the policy links are relative.
- [ ] Sign a data processing agreement with the hosting provider.
- [ ] Check the hosting region matches `LEGAL_HOSTING_REGION` and that transfers
      outside the EEA (if any) rely on SCCs.
- [ ] Decide a retention period for rotated backups and write it down; the
      policy says backups rotate out without promising a number.
- [ ] Keep a record of processing activities — for a single-instance deployment
      this can be a page in your own docs.
- [ ] Publish `https://<host>/account/delete` in the Play listing (see
      `docs/play-store.md`).
