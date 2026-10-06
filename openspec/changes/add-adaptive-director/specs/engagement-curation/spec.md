# Spec: engagement-curation

## ADDED Requirements

### Requirement: Canonical engagement signal capture

The system SHALL record per-source engagement signals using the canonical `"<type>:<id>"` cache key for every source, replacing name-based attribution. Signal kinds SHALL be: `display` and `dwell` (recorded when a source begins and ends its slot), `skip` (recorded when `next`/skip advances away from the on-screen source), `pin` (recorded when a source becomes the pinned source on a feed controller and deduplicated while it remains pinned), and `pause`/`resume` (recorded when a feed controller pauses on the on-screen source and when it resumes, closing a dwell window). A signal SHALL be attributed to the source that was on-screen at the moment of the event. When no current source is known, the system SHALL increment the existing unattributed counter and SHALL NOT corrupt any per-source count.

#### Scenario: Display and skip use the same key

- **WHEN** source `news:3` is displayed and the operator presses next
- **THEN** a display/dwell signal and a skip signal SHALL both be recorded under the key `news:3`

#### Scenario: Pin records a hold

- **WHEN** a feed controller pins source `transit:12` and the same key stays pinned across repeated evaluations
- **THEN** exactly one pin signal SHALL be recorded for the pin transition and no additional pin signal SHALL be recorded while it remains pinned

#### Scenario: Pause and resume close a dwell window

- **WHEN** a feed controller pauses while `weather:1` is on-screen and resumes 90 seconds later
- **THEN** a pause signal SHALL be attributed to `weather:1` and a resume dwell of 90 seconds SHALL be added to that key

#### Scenario: Unattributed event is counted, not misfiled

- **WHEN** a skip arrives before any source has been displayed
- **THEN** the unattributed counter SHALL increment and no per-source skip SHALL be recorded

### Requirement: Bounded signal store

Engagement signals SHALL be held in memory in a bounded ring buffer with a global cap, a per-source cap, and a per-kind cap; when a cap is reached the oldest signals SHALL be dropped first. The store SHALL NOT persist signals across restarts and SHALL NOT perform a database write per signal. Reading or summarizing the store SHALL be concurrency-safe and SHALL not block the feed loop on database access.

#### Scenario: Oldest signals drop first

- **WHEN** a source accumulates more signals than the per-source cap
- **THEN** the oldest signals for that source SHALL be dropped and the newest retained

#### Scenario: Store does not grow without bound

- **WHEN** the process runs for days with continuous displays, skips, pauses and pins
- **THEN** the total number of retained signals SHALL never exceed the global cap

#### Scenario: Restart resets curation

- **WHEN** the server restarts
- **THEN** the signal store SHALL start empty and adaptive weights SHALL return to cold-start behavior until new signals accumulate

### Requirement: Deterministic decayed weight computation

The system SHALL provide pure functions that aggregate engagement signals into per-source statistics and turn those statistics into weights summing to 1.0. Each signal SHALL be decayed by age with half-life `HalfLifeDays` and signals older than `WindowDays` SHALL be dropped before aggregation. A source's trusted skip rate SHALL be `weightedSkips / weightedDisplays` only when its raw display count is at least `MinDisplaysForSkipTrust`; otherwise the skip penalty SHALL be zero. The score SHALL be `decayedDisplays * (1 - beta * skipRate) * (1 + holdBoost + dwellBoost)` clamped at zero, where `holdBoost` is capped and derived from pins and pauses relative to decayed displays, and `dwellBoost` is capped and derived from capped dwell seconds relative to a target dwell. The existing exploration (`epsilon` mixing), per-source floor, floor cap `1/N`, cold-start equal weights and renormalization SHALL apply unchanged. Given identical signal sets, candidates and configuration, the computation SHALL be deterministic and SHALL run in time linear in the bounded signal count plus the candidate count.

#### Scenario: Decay favors recent engagement

- **WHEN** source A has signals 13 days old and source B has the same signals yesterday with half-life 7 days
- **THEN** B's weight SHALL be higher than A's

#### Scenario: Pins and pauses raise a weight

- **WHEN** sources A and B have identical displays and skips but A has recent pin and pause holds
- **THEN** A's weight SHALL be higher than B's

#### Scenario: Dwell is capped

- **WHEN** a source has a single pause/resume dwell far larger than the dwell cap
- **THEN** its dwell boost SHALL be capped and the resulting weight SHALL remain finite and bounded

#### Scenario: Skip penalty still respects the trust threshold

- **WHEN** a source has 3 displays and 2 skips
- **THEN** its skip penalty SHALL be zero, exactly as in the existing adaptive behavior

#### Scenario: Floor and exploration are preserved

- **WHEN** any source would otherwise receive a near-zero weight
- **THEN** its final weight SHALL be at least the configured floor after renormalization and all weights SHALL sum to 1.0 within floating tolerance

#### Scenario: Deterministic output

- **WHEN** the aggregation and weighting functions are called twice with the same signals, candidates and clock
- **THEN** both calls SHALL return identical maps

### Requirement: Adaptive rotation applies to global mode only

Engagement-weighted selection SHALL apply only when the ordering mode is `adaptive` AND the connection's effective content mode is `global`. Devices in `playlist` or `scheduled` mode (explicitly or inherited from a group) SHALL keep their resolvable sources in saved order and SHALL NOT be reordered by weights; the WebSocket feed and push transports SHALL implement the same rule. When the ordering mode is `random` or `sequential`, no engagement signal SHALL be read and existing behavior SHALL be byte-for-byte preserved. When adaptive is selected but the weights cache is empty, selection SHALL fall back to uniform random and keep the wall non-empty.

#### Scenario: Global adaptive rotation tracks weights

- **WHEN** ordering mode is adaptive, the effective content mode is global, and weights are `{A:0.6, B:0.3, C:0.1}`
- **THEN** sampled next-slot frequencies over many rotations SHALL approximate those weights

#### Scenario: Playlist order is never reordered

- **WHEN** ordering mode is adaptive and a device's effective content mode is `playlist`
- **THEN** the device SHALL play the playlist's resolvable sources in saved order, using the same selection behavior as before this change

#### Scenario: Scheduled mode is unaffected

- **WHEN** ordering mode is adaptive and a device resolves to a scheduled playlist
- **THEN** the scheduled playlist's sources SHALL cycle in authored order without engagement weighting

#### Scenario: Random and sequential ignore engagement

- **WHEN** ordering mode is `random` or `sequential` while engagement signals exist
- **THEN** selection SHALL use the existing uniform-random or sequential behavior and SHALL NOT query the signal store

#### Scenario: Empty weights fall back safely

- **WHEN** ordering mode is adaptive but the weights cache is empty
- **THEN** the next slot SHALL be chosen uniformly at random from the available sources

### Requirement: Explainability API and admin view

`GET /api/analytics/weights` SHALL retain its existing response keys (`weights`, `displays`, `skips`, `computedAt`, `config`, `collectingData`, `floorClampedSources`) and SHALL add a per-source breakdown containing the canonical key, label, weight, raw signal counts (displays, skips, pins, pauses, dwell seconds), decayed display count, trusted skip rate, skip penalty, hold boost, dwell boost, whether the weight was clamped by the floor or is cold-start, and a `reasons` array drawn from a closed vocabulary (`high_dwell`, `frequent_skip`, `pin_hold`, `pause_hold`, `exploration`, `floor`, `cold_start`, `excluded`). The admin analytics page SHALL render the breakdown with per-source weight bars and reason chips. When there is insufficient data, the endpoint SHALL return equal weights with `collectingData=true`.

#### Scenario: Breakdown explains a raised source

- **WHEN** a pinned, frequently dwelled source is weighted above its display share
- **THEN** its breakdown SHALL include `pin_hold` and/or `high_dwell` in `reasons` and show nonzero boosts

#### Scenario: Breakdown explains a lowered source

- **WHEN** a source has a trusted skip rate above zero
- **THEN** its breakdown SHALL show the skip penalty and `frequent_skip` in `reasons`

#### Scenario: Existing consumers keep working

- **WHEN** a client reads only the previously documented keys of `GET /api/analytics/weights`
- **THEN** those keys SHALL be present with the same meanings

#### Scenario: Cold start is labeled

- **WHEN** total windowed displays are below the collecting threshold
- **THEN** the response SHALL set `collectingData=true` and per-source reasons SHALL include `cold_start`

### Requirement: Opt-out and decay policy

The system SHALL expose engagement policy controls on the admin settings surface: per-signal-class toggles (`displays`, `skips`, `pins`, `pauses`, `dwell`), a per-source exclusion list, and the existing decay/window/floor/exploration knobs extended with hold weight, dwell weight and target dwell. Selecting `random` or `sequential` ordering SHALL bypass engagement entirely. Disabling all added signal classes SHALL reproduce the pre-change displays-and-skips weighting. Excluded sources SHALL receive only the exploration/floor share and SHALL be reported with the `excluded` reason. All controls SHALL be validated server-side with documented ranges, and defaults SHALL preserve the pre-change behavior.

#### Scenario: Signal class opt-out takes effect

- **WHEN** an administrator disables the `pauses` signal class
- **THEN** pause and resume events SHALL no longer influence weights and the breakdown SHALL reflect zero pause contribution

#### Scenario: Excluded source is neutral

- **WHEN** a source is added to the exclusion list
- **THEN** its weight SHALL be based on the exploration/floor share and its breakdown SHALL include the `excluded` reason

#### Scenario: Out-of-range tuning rejected

- **WHEN** an administrator submits a half-life outside 1-30 days
- **THEN** the server SHALL reject the save with a descriptive error

#### Scenario: Defaults are backward compatible

- **WHEN** an upgraded server has adaptive ordering enabled and no engagement policy configured
- **THEN** weights SHALL include engagement signals but the ordering floor, exploration and cold-start behavior SHALL match the existing adaptive kernel

### Requirement: Recompute cadence and cache

Engagement statistics and weights SHALL be cached in memory and recomputed on the existing 5-minute ticker and on the existing debounced trigger after a skip, using the bounded signal store rather than a per-frame database query. The feed loop SHALL read only the cache. Aggregate state retained between recomputes SHALL be bounded by the store caps; the cache SHALL fall back to equal weights with a single warning when it is empty.

#### Scenario: Recompute rides the existing cadence

- **WHEN** the 5-minute ticker fires
- **THEN** the weights cache SHALL be refreshed from the bounded signal store before the next selection

#### Scenario: Skip schedules a debounced recompute

- **WHEN** a skip signal is recorded
- **THEN** a recompute SHALL be scheduled after the existing debounce rather than executed per event

#### Scenario: Feed selection reads only the cache

- **WHEN** the feed resolver chooses the next source
- **THEN** it SHALL NOT read the signal store or the database for weights at that moment
