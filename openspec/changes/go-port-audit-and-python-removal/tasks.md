## 1. Audit Go Port Completeness

- [x] 1.1 Compare Python Django datasource models vs Go ent schema and datasource implementations — confirm all covered (Sonarr, Radarr, F1, Weather, HomeAssistant, Untappd, Image, Video, Crypto, RSS, Calendar, Stock, TextSlide)
- [x] 1.2 Compare Python websocket consumer feed loop with Go websocket handler — confirm timeout, random shuffle, and datasource cycling are covered
- [x] 1.3 Compare Python themes (default, f1, untapped) with Go render/themes/ — confirm parity
- [x] 1.4 Check Python `device_settings` and `render/icon_utils.py` for any Go gaps
- [x] 1.5 Identify empty/unused Go directories (`middleware/`, `models/`) and orphaned `client/` Python package

## 2. Remove Python Django Code

- [x] 2.1 Delete `server-py/` entire directory tree
- [x] 2.2 Delete `client/` directory (orphaned Python client)
- [x] 2.3 Delete empty `go/middleware/` and `go/models/` directories

## 3. Verify Clean Build

- [x] 3.1 Run `go build ./...` — confirm success
- [x] 3.2 Run `go test ./...` — confirm all tests pass
- [x] 3.3 Search for stale references to `server-py`, `manage.py`, `django` in non-Python files — confirm none

## 4. Commit and Push

- [x] 4.1 Stage all changes and commit with message: `chore: remove orphaned Python Django code and empty Go scaffolding`
- [ ] 4.2 Push to remote
