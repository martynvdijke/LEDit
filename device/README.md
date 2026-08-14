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

# This package (from the LEDit repo's device/ directory)
pip3 install .
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
| `LEDIT_TOKEN`           | *(required)*          | Device token (admin → Devices)   |
| `LEDIT_COLS`            | `64`                  | Panel width                      |
| `LEDIT_ROWS`            | `64`                  | Panel height                     |
| `LEDIT_CHAIN`           | `1`                   | Chained panels                   |
| `LEDIT_PARALLEL`        | `1`                   | Parallel chains                  |
| `LEDIT_HARDWARE_MAPPING`| `regular`             | rpi-rgb-led-matrix mapping       |
| `LEDIT_BRIGHTNESS`      | `80`                  | 0–100                            |
| `LEDIT_GPIO_SLOWDOWN`   | `1`                   | Set >1 on Pi 4 / fast boards     |
| `LEDIT_PREVIEW_DIR`     | *(unset)*             | Save frames as PNGs (no hardware)|

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
