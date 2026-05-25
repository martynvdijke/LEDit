## [1.0.3](https://github.com/martynvdijke/LEDit/compare/v1.0.2...v1.0.3) (2026-05-25)


### Bug Fixes

* remove stalePr from renovate.json (removed in Renovate v37) ([04c6a5e](https://github.com/martynvdijke/LEDit/commit/04c6a5e5fe3b67e4cce2a0944f78f264d367733e))

## [1.0.2](https://github.com/martynvdijke/LEDit/compare/v1.0.1...v1.0.2) (2026-05-25)


### Bug Fixes

* add packages: write permission to release job for GHCR push ([dc0ad62](https://github.com/martynvdijke/LEDit/commit/dc0ad62a1c3eaa06319825180f9245141c37a4b9))

## [1.0.1](https://github.com/martynvdijke/LEDit/compare/v1.0.0...v1.0.1) (2026-05-25)


### Bug Fixes

* bump builder image to golang:1.26-alpine for go 1.26.3 compat ([93efde7](https://github.com/martynvdijke/LEDit/commit/93efde7a4707140d8c3972a8692d3d009e75cf1b))

# 1.0.0 (2026-05-25)


### Bug Fixes

* **ci:** install firefox in CI for playwright tests ([5def24c](https://github.com/martynvdijke/LEDit/commit/5def24c9819c514362c0db0b954c3e2a1ab0fcb1))
* grant explicit permissions to reusable workflow call in release.yaml ([ed40ece](https://github.com/martynvdijke/LEDit/commit/ed40ece1813a124ec2cec894af851685d83237df))
* isolate Playwright projects to fix Firefox CI test failures ([13df0b1](https://github.com/martynvdijke/LEDit/commit/13df0b126f554c9266d708b49c8addab7f7a5d9c))
* remove stalePrAge from renovate.json (removed in Renovate v37) ([9ef1e14](https://github.com/martynvdijke/LEDit/commit/9ef1e14a1d6a7d0614b92b3fede078b4831a3cec))
* **ui:** add autocomplete to auth fields, replace inline onclick handlers ([3175330](https://github.com/martynvdijke/LEDit/commit/3175330356002bed7b03a881dc01cf4400ba6867))


### Features

* add full admin CRUD for all datasource types ([8f2a35e](https://github.com/martynvdijke/LEDit/commit/8f2a35e6df929579c9892731ce19f719e45ad5ad))
* add gif + mp4 support refactor models ([dcc187a](https://github.com/martynvdijke/LEDit/commit/dcc187ae74eddf23fb27b858c05b297378ea7c5c))
* add hardware devices, theme editor, auth, and analytics ([9f93bc5](https://github.com/martynvdijke/LEDit/commit/9f93bc55b4aaede41455a5c783627ba277d58ded))
* add new datasources, PWA, feed experience, text editor + tests ([e36dff4](https://github.com/martynvdijke/LEDit/commit/e36dff4b8ecf6c19c830127f691796b265f19e22))
* add push notification system with webhook support ([3d6d771](https://github.com/martynvdijke/LEDit/commit/3d6d77108aae8bc690a69e4dcf4b0b3586783f43))
* add real API integrations for all datasources ([7b7d4e9](https://github.com/martynvdijke/LEDit/commit/7b7d4e9a02dad8c628f8c2418b7c2f91717175d0))
* add REST API for external feed control ([cb9c081](https://github.com/martynvdijke/LEDit/commit/cb9c0815846785e5c09617a27f244c849ecbfaaf))
* add scheduling and playlist system ([f7642bc](https://github.com/martynvdijke/LEDit/commit/f7642bc3c4c57ef7c2f6016f9430a266c2f0fce5))
* added automatic websocket connect + started on models ([a412a93](https://github.com/martynvdijke/LEDit/commit/a412a937037261e0c35e55a1e1c60e804808861b))
* added bootstap template + websocket bassis ([94fcdd6](https://github.com/martynvdijke/LEDit/commit/94fcdd65317fd9e8734e144e7743c49c289fd168))
* added index setup ([37d4945](https://github.com/martynvdijke/LEDit/commit/37d4945c892f21f80876a5ab65add97af66b3c76))
* migrate Python Django server to Go (gin + ent + gorilla/websocket) ([30c3f0d](https://github.com/martynvdijke/LEDit/commit/30c3f0d9e9859f7637573cf439e060c563ec12d2))
* move to uv and ruff, addded basic png cration ([98e28ab](https://github.com/martynvdijke/LEDit/commit/98e28ab3a5b8478d1c300bc608caa583accc00b3))
* overhaul live feed UI with controls and matrix overlay ([31c55d3](https://github.com/martynvdijke/LEDit/commit/31c55d33a8b870c5b0ad69b176b88e5fa69996f4))
* playing image feed ([f4fe2b0](https://github.com/martynvdijke/LEDit/commit/f4fe2b0795d0448779e86ac9307e2c1d74f4232a))
* repo scaffolding ([d97d7c0](https://github.com/martynvdijke/LEDit/commit/d97d7c0f8015deb64a0794618d6ee39acb805b96))
* reworked models into abstract models inheriting from a base RenderModel ([c07d78d](https://github.com/martynvdijke/LEDit/commit/c07d78deb043457518209e32af6992e9dd60d2b0))
* started on models ([60f2f0d](https://github.com/martynvdijke/LEDit/commit/60f2f0d99c29dfda5501b8ba367cdfab01942a16))
* update datasources, handlers, and tests ([2b85ef4](https://github.com/martynvdijke/LEDit/commit/2b85ef47aac16b9202e9cd55650c90b1de2f9cac))

# Changelog
