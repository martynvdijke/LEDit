# LEDit Android remote

Flutter app (`xyz.vandijke.ledit`) that turns a phone into a remote for a
[self-hosted LEDit server](../README.md): live matrix preview, brightness,
playlist/scene switching, and priority text/image pushes.

## Features

- **Onboarding** — server URL (no default; type the address of your instance)
  plus browser-based OIDC sign-in: the app opens the server's own login page
  in an embedded browser, lets Authelia (or the configured provider) handle
  credentials, and keeps the `session` cookie issued by the OIDC callback.
  API tokens (Admin → API tokens) and username/password remain available for
  servers without OIDC.
- **Branded icon** — the launcher/adaptive icon and splash screen use the PWA
  mark (`web/static/pwa/icon.svg`) and the ink/phosphor palette from
  `web/static/brand/brand.css`.
- **Dashboard** — every device with liveness, live preview thumbnail, wall-wide
  pause/resume/next.
- **Device screen** — full-resolution live preview, debounced (150ms)
  brightness slider, playlist and scene activation, priority message composer
  with duration picker and image attachment.

Live frames come from the server's `/ws/device/<id>/preview` WebSocket; control
uses the REST endpoints `GET /api/devices`, `GET /api/playlists`,
`GET /api/scenes`, `GET /api/images`, `POST /api/devices/:id/brightness`,
`POST /api/devices/:id/select` and `POST /api/devices/:id/message`.

## Development

```sh
flutter pub get
flutter analyze
flutter test
flutter run              # against a reachable LEDit server
flutter build apk --release
```

A release APK build needs JDK 17+ (the generated Gradle wrapper targets Java
17). Release APKs are signed with Flutter's debug keystore, so they install as
sideload builds; replace the signing config before distributing on a store.
