# Google Play submission

What the Play Console asks for, and the answers that match what LEDit actually
does. Update this file whenever a permission, SDK or server feature changes:
the Data safety form is a legal declaration, not a formality.

## Before the first upload

| Requirement | Where | LEDit answer |
| --- | --- | --- |
| App must exist | Play Console → Create app | Name, default language, "App" + "Free" |
| Privacy policy URL | Store listing → Privacy policy | `https://<your-host>/privacy` — must load without signing in |
| Account deletion URL | App content → Data deletion | `https://<your-host>/account/delete` |
| Data deletion in app | App content → Data deletion | Yes: Settings → Delete account |
| Sign-in details | App content → App access | A test account with a real role, or "all functionality is restricted" when the instance is invite-only |
| Target API level | App bundle | `targetSdk = 36` in `mobile/android/app/build.gradle.kts` |

Play requires the deletion URL to be reachable *and* to explain what happens,
not just to exist. `/account/delete` does both. The in-app action calls
`DELETE /api/v1/me` with the typed username as confirmation, then clears the
local secure storage.

## Data safety answers

The Android app talks to the self-hosted server's `/api` and `/ws` routes only.
These answers assume the default build: no crash reporting, no advertising SDK,
no third-party analytics, no push service. The only declared permission is
`INTERNET`; the app has no camera, location, contacts, microphone or health
permission.

**Does your app collect or share any of the required user data types?** Yes.

| Data type | Collected | Shared | Purpose | Optional? | Ephemeral? |
| --- | --- | --- | --- | --- | --- |
| Personal info → Email address | Yes, if the server account has one | No | Account management | No (supplied by the server administrator) | No |
| Personal info → Name | Yes (username) | No | Account management, app functionality | No (required to sign in) | No |
| Personal info → User IDs | Yes (account ID, token owner ID) | No | Account management, authentication | No | No |
| Photos and videos → Photos | Yes, when the user picks an image to upload as a display source | No | App functionality | Yes | No |
| App activity → Other actions | Yes (feed control, device selection, brightness, messages) | No | App functionality | No | No |
| App info and performance → Other app performance data | No crash logs or diagnostics are collected by the app | No | — | — | — |

Notes that matter during review:

- **No location.** The app never requests device location. The server's
  latitude/longitude is an administrator setting used for sunrise/sunset; it is
  not read from the phone.
- **No push notifications.** There is no FCM or other push SDK in this build.
- **"Shared" means transferred to a third party.** Data goes only to the
  operator's own LEDit server. If the operator configures notification, AI or
  datasource integrations on the server, those are the operator's processing
  arrangements and are described in `/privacy`, not app-level sharing.
- **Nothing is sold**, so "sharing for advertising" is No everywhere.
- **Photos** are selected through the Android photo picker and uploaded only
  after an explicit user action; the app does not read the photo library.
- Credentials are stored with `flutter_secure_storage`, and the release manifest
  sets `android:usesCleartextTraffic="false"`. HTTP is allowed only in debug
  builds for LAN development.

## Permissions and in-app safety

| Permission | Asked when | Rationale the reviewer can see |
| --- | --- | --- |
| `INTERNET` | Always | The app is a remote for a self-hosted server |
| Photo picker | Tapping upload in the image picker | The user chooses one image to add as a display source; cancelling is fine |

The app has no user-to-user social features, no public feed and no in-app
purchases. Guest photo upload and the guest remote are web pages served by the
operator, not part of the Android app. Account deletion is reachable from
Settings and from the public URL submitted as the Data deletion link.

## Backup and credential handling

`android:allowBackup="false"`, `android:fullBackupContent="false"` and
`res/xml/data_extraction_rules.xml` exclude every backup and device-transfer
domain, so the bearer token, session cookie and cached server address cannot be
restored onto another device. Play does not require this, but a backed-up
session token would be a data-protection problem the Data safety form would
have to disclose.

## Content rating

Questionnaire answers: no violence, no sexual content, no profanity, no
gambling, no controlled substances, no user-generated public content inside the
app. The app displays operator-configured dashboards and messages, which can
contain arbitrary text. The expected rating is Everyone / PEGI 3. Answer the
questionnaire honestly if the operator's deployment changes what the app shows;
do not under-declare user-generated content to get a lower rating.

## Ads and target audience

- Contains ads: No.
- Target age group: 16+ (the privacy policy commits to 16, matching the GDPR
  default for consent in most member states).
- Not designed for children; do not opt into the Families programme.
- No in-app purchases.

## Testing requirements

Personal developer accounts created after November 2023 must run a closed test
with at least 12 testers for 14 continuous days before production access is
granted. Internal testing does not count towards it.

- Internal testing: up to 100 testers, no review, link is
  `https://play.google.com/apps/internaltest/<id>` from the Testers tab.
- Closed testing: `https://play.google.com/apps/testing/<package>`, up to 1,000
  testers, needs a review pass.

Provide a test account (username + password or API token) that can sign in to
the hosted instance used for review. If the app is only usable against a private
server, mark it as restricted and explain the access requirement.

## Uploading

Build a signed release bundle:

```sh
cd mobile
flutter build appbundle --release
```

The bundle is signed only if `mobile/android/key.properties` exists. When
building by hand, pass `--build-number=$(git rev-list --count HEAD)` so Play
does not reject the upload as a downgrade. The version name comes from
`pubspec.yaml`.

## Service account

Play Console → Users and permissions must include the service account used by
CI, with *Release to testing tracks* at minimum. Without that grant the
Publisher API answers 404 for the package even when the credentials are valid —
a missing grant looks exactly like a missing app.
