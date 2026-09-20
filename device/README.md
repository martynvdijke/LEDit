# LEDit Device Client

A small Python package for Raspberry Pi Zero (or any Pi) devices driving an RGB
LED matrix (HUB75 panels). It connects **out** to your LEDit server over
WebSocket, pulls frames, and renders them onto the panel.

Because the device pulls from the server, there is no inbound port, no static
IP, and no credentials beyond a per-device token. Updating the server requires
no changes to the device — new features appear automatically on the next frame.

## How it works

1. The server renders each source (F1, weather, calendar, news, stocks, …) to a
   PNG at the device's configured width × height.
2. Frames stream to the device at `ws://<server>/ws/device/<token>`.
3. The client decodes each frame and pushes it to the matrix.
4. The cycle interval (how long each source shows) is configured **per device**
   on the server (`refresh_interval`, default 60 seconds).

## Requirements

- Python 3.8+
- [rpi-rgb-led-matrix](https://github.com/hzeller/rpi-rgb-led-matrix) (C++
  library + Python bindings, installed separately)
- `Pillow`, `websocket-client`, and the OpenTelemetry packages (installed
  automatically by pip)

### Install on a Pi Zero

```bash
# System packages
sudo apt update && sudo apt install -y python3-pip python3-pil git

# rpi-rgb-led-matrix (build Python bindings)
git clone https://github.com/hzeller/rpi-rgb-led-matrix.git
cd rpi-rgb-led-matrix
make build-python PYTHON=$(which python3)
sudo make install-python PYTHON=$(which python3)

# This package (published to PyPI on every LEDit release)
pip3 install ledit
```

Or from a checkout of this repo:

```bash
pip3 install ./device
```

For development (editable install):

```bash
pip3 install -e .
```

## Configuration

All configuration is via environment variables:

| Variable                | Default               | Purpose                          |
| ----------------------- | --------------------- | -------------------------------- |
| `LEDIT_SERVER`          | `ws://localhost:8080` | WebSocket URL of the server      |
| `LEDIT_TOKEN`           | *(required)*          | Device token (admin → Devices); may be omitted after auto-provisioning (persisted to `~/.config/ledit/token`) |
| `LEDIT_UPDATE_INTERVAL` | `3600`                | Firmware OTA poll interval (seconds); `0` disables |
| `LEDIT_UPDATE_CHANNEL`  | *(empty)*             | OTA channel (empty = server default channel) |
| `LEDIT_COLS`            | `64`                  | Panel width                      |
| `LEDIT_ROWS`            | `64`                  | Panel height                     |
| `LEDIT_CHAIN`           | `1`                   | Chained panels                   |
| `LEDIT_PARALLEL`        | `1`                   | Parallel chains                  |
| `LEDIT_HARDWARE_MAPPING`| `regular`             | rpi-rgb-led-matrix mapping       |
| `LEDIT_BRIGHTNESS`      | `80`                  | Startup brightness, 0–100 (live hint overrides) |
| `LEDIT_GPIO_SLOWDOWN`   | `1`                   | Set >1 on Pi 4 / fast boards     |
| `LEDIT_PREVIEW_DIR`     | *(unset)*             | Save frames as PNGs (no hardware)|
| `LEDIT_SPECTRUM`        | `0`                   | Opt in to the audio spectrum tap (`1`/`true`) |
| `LEDIT_BUTTON_SHORT_MS` | `500`                 | Nominal short-press window (ms)  |
| `LEDIT_BUTTON_LONG_MS`  | `800`                 | Hold threshold; press ≥ this emits `hold` (ms) |
| `LEDIT_BUTTON_HOLD_REPEAT_MS` | `0`             | Repeat `hold` every N ms while held (`0` = once) |

## Protocol v2 (brightness, spectrum, buttons)

The client connects with `?protocol=2`. Servers that understand it reply with a
`{"type":"welcome","protocol":2,"capabilities":["brightness","spectrum","hold"]}`
message; if no welcome arrives the client stays in v1 mode with no brightness
hints and no spectrum. All v2 fields are optional and additive — old servers
and old `wscat` clients keep working unchanged.

- **Brightness**: frames may carry a `brightness` integer (0–100). When present
  and in range the client applies it to the running `rpi-rgb-led-matrix`
  instance live, without recreating the matrix. Until the first hint the
  `LEDIT_BRIGHTNESS` startup value is used. Absent or out-of-range values leave
  brightness unchanged.
- **Spectrum (opt-in, default off)**: with `LEDIT_SPECTRUM=1`, when the server
  advertises `spectrum` and the current frame source is the audio visualizer
  (`audio:visualizer`, or the built-in display name `Audio Visualizer`), the
  client captures microphone audio best-effort and sends
  `{"type":"spectrum","bins":[...]}` (16 bins, 0–255) at ~20 Hz. No microphone
  or optional audio library simply means no spectrum is sent — never a crash.
  Requires `numpy`; `sounddevice` is used opportunistically when installed.
- **Buttons**: a short press (released before `LEDIT_BUTTON_LONG_MS`) sends the
  existing `{"action":"next"}` / `{"action":"pause"}` on release. A press held
  to or beyond `LEDIT_BUTTON_LONG_MS` sends `{"action":"hold"}`, optionally
  repeating every `LEDIT_BUTTON_HOLD_REPEAT_MS` while held. Debounce is
  preserved.
- **v1 compatibility**: a v1 server (no welcome) or a v1 device (no `protocol`
  param) degrades to v1 behaviour. Frames never change key names or the PNG
  format.


## OpenTelemetry

The device exports **traces, metrics, and logs** to an OTLP-compatible backend
(the same way the LEDit server does). Everything is off by default — if
`OTEL_EXPORTER_OTLP_ENDPOINT` is not set the client runs exactly as before,
with no telemetry overhead.

| Variable                       | Default          | Purpose                                        |
| ------------------------------ | ---------------- | ---------------------------------------------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT`  | *(unset)*        | OTLP collector endpoint; unset disables telemetry |
| `OTEL_EXPORTER_OTLP_PROTOCOL`  | `grpc`           | `grpc` or `http/protobuf`                      |
| `OTEL_SERVICE_NAME`            | `ledit-device`   | Service name attached to exported telemetry   |
| `OTEL_RESOURCE_ATTRIBUTES`     | *(unset)*        | Extra resource attributes (e.g. `rack=42,zone=west`) |
| `OTEL_TRACES_SAMPLER`          | *(default)*      | `always_on`, `always_off`, `traceidratio`, `parentbased_*` |

Spans cover the WebSocket lifecycle (message received, image/text render,
connection errors) and metrics include `device.frames_rendered_total`,
`device.connection_errors_total`, and `device.reconnects_total`. Device logs
are forwarded to the OTLP backend with trace-context correlation.

Example with a local collector:

```bash
LEDIT_TOKEN=<token> OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 ledit-device
```

## Getting the token

1. Open the LEDit admin UI → **Devices**.
2. Create a device (name + matrix size + refresh interval).
3. Copy the generated **token** (and full connection URL) from the table.

## Discovery and auto-provisioning

Unprovisioned devices can advertise themselves via mDNS and be enrolled from the server without manually copying a token.

- **Advertisement**: DNS-SD service `_ledit._tcp.local` with TXT records `id` (stable fingerprint), `model`, `version`, `proto`, `nonce`. The token is never advertised.
- **Fingerprint**: `fingerprint()` reads `/etc/machine-id` when available, otherwise a random ID persisted at `~/.config/ledit/device_id`. Stable across reboots.
- **Nonce**: `new_nonce()` generates a fresh value per boot and is included in the TXT records.
- **Optional dependency**: `zeroconf` is required only for discovery. Install with `pip install 'ledit[discovery]'`. If missing, advertising/provisioning is skipped with a warning and manual `LEDIT_TOKEN` mode is unaffected.
- **API**: `discovery.start_advertising()` / `discovery.stop_advertising()` and `discovery.provision(server_url, fingerprint, nonce, interval, timeout)` which polls `GET /api/device/provision?fingerprint=…&nonce=…`.

**Enabling flow**:

1. Start the device without `LEDIT_TOKEN` (with the discovery extra installed). It begins advertising.
2. In the server admin UI go to **Admin → Discovery** — the device appears as pending.
3. Enroll it. The server binds the fingerprint+nonce to a token.
4. The device polls `GET /api/device/provision` until the token is returned (once), persists it to `~/.config/ledit/token` (configurable via `LEDIT_CONFIG_DIR`), and then connects to `/ws/device/<token>`. Subsequent boots use the persisted token and `LEDIT_TOKEN` may be omitted.

## Firmware OTA

`firmware.check_and_update(server_url, token, current_version, channel)` polls the server manifest, downloads the artifact, verifies `sha256`, and stages the update atomically.

- Polls `GET /api/device/firmware?version=<current>&channel=<channel>` (channel from `LEDIT_UPDATE_CHANNEL`).
- Downloads from the manifest `url` (or `/api/device/firmware/<version>/artifact`), verifies `sha256` (and `size` when provided).
- Stages to `~/.config/ledit/staging/` (or `LEDIT_STAGING_DIR`) as `firmware-<version>.bin` with an `activate` marker; the running process is never overwritten. A failed or interrupted update leaves the previous version bootable.
- Non-fatal on network/parse errors — logs a warning and returns.
- Polling interval is `LEDIT_UPDATE_INTERVAL` (default 3600 s); set `0` to disable.

## Inbound webhook signing

When a signing secret is configured in **Admin → Webhook settings**, inbound webhook requests must be signed. This is separate from LEDit's *outbound* webhooks (which sign the body only).

- Headers:
  - `X-LEDit-Timestamp: <unix seconds>`
  - `X-LEDit-Signature: sha256=<hex>` where hex is `HMAC-SHA256(secret, "<timestamp>.<raw-body>")` — the timestamp string, a literal `.`, and the raw request body.
- Verification: missing, stale (>300 s, configurable via `signing_window_seconds`), or mismatched signatures get a generic `401`.
- When no signing secret is set, the legacy `X-API-Key` / `?token=` auth is unchanged. If both a signing secret and an API key/token are configured, both are required.

## Run

Installed as a package, run the console script:

```bash
LEDIT_SERVER=ws://ledit.local:8080 LEDIT_TOKEN=<token> ledit-device
```

Or without installing (from the `device/` directory):

```bash
LEDIT_SERVER=ws://ledit.local:8080 LEDIT_TOKEN=<token> python3 -m ledit_device
```

The client reconnects automatically on network drops.

### Test without hardware

```bash
LEDIT_SERVER=ws://localhost:8080 LEDIT_TOKEN=<token> \
  LEDIT_PREVIEW_DIR=/tmp/ledit_frames python3 -m ledit_device
```

This writes each received frame as a PNG into `LEDIT_PREVIEW_DIR`.

## Package layout

```
  device/
    pyproject.toml          # package metadata + console script
    ledit_device/
      __init__.py           # version + public exports
      __main__.py           # entry point (python -m ledit_device)
      config.py             # env-var config + logging
      display.py            # MatrixDisplay / FileDisplay abstractions
      client.py             # WebSocket frame handling + rendering
      telemetry.py          # OpenTelemetry init/shutdown (traces, metrics, logs)
    tests/
      test_client.py        # unit tests (no hardware required)
      test_telemetry.py     # telemetry unit tests
```

## Running tests

The unit tests use a `FileDisplay` (writes PNGs to a temp dir), so they run
without a panel or the `rgbmatrix` bindings installed:

```bash
cd device
python3 -m unittest discover -s tests -v
```
