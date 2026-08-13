# LEDit Device Client

A single-file Python client for Raspberry Pi Zero (or any Pi) devices driving an
RGB LED matrix (HUB75 panels). It connects **out** to your LEDit server over
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

- Python 3.7+
- [rpi-rgb-led-matrix](https://github.com/hzeller/rpi-rgb-led-matrix) (C++
  library + Python bindings)
- `pip install pillow websocket-client`

### Install on a Pi Zero

```bash
# System packages
sudo apt update && sudo apt install -y python3-pip python3-pil git

# Python deps
pip3 install websocket-client

# rpi-rgb-led-matrix (build Python bindings)
git clone https://github.com/hzeller/rpi-rgb-led-matrix.git
cd rpi-rgb-led-matrix
make build-python PYTHON=$(which python3)
sudo make install-python PYTHON=$(which python3)
```

## Configuration

All configuration is via environment variables:

| Variable               | Default               | Purpose                          |
| ---------------------- | --------------------- | -------------------------------- |
| `LEDIT_SERVER`         | `ws://localhost:8080` | WebSocket URL of the server      |
| `LEDIT_TOKEN`          | *(required)*          | Device token (admin → Devices)   |
| `LEDIT_COLS`           | `64`                  | Panel width                      |
| `LEDIT_ROWS`           | `64`                  | Panel height                     |
| `LEDIT_CHAIN`          | `1`                   | Chained panels                   |
| `LEDIT_PARALLEL`       | `1`                   | Parallel chains                  |
| `LEDIT_HARDWARE_MAPPING`| `regular`            | rpi-rgb-led-matrix mapping       |
| `LEDIT_BRIGHTNESS`     | `80`                  | 0–100                            |
| `LEDIT_GPIO_SLOWDOWN`  | `1`                   | Set >1 on Pi 4 / fast boards     |
| `LEDIT_PREVIEW_DIR`    | *(unset)*             | Save frames as PNGs (no hardware)|

## Getting the token

1. Open the LEDit admin UI → **Devices**.
2. Create a device (name + matrix size + refresh interval).
3. Copy the generated **token** (and full connection URL) from the table.

## Run

```bash
LEDIT_SERVER=ws://ledit.local:8080 LEDIT_TOKEN=<token> python3 ledit_client.py
```

The client reconnects automatically on network drops.

### Test without hardware

```bash
LEDIT_SERVER=ws://localhost:8080 LEDIT_TOKEN=<token> \
  LEDIT_PREVIEW_DIR=/tmp/ledit_frames python3 ledit_client.py
```

This writes each received frame as a PNG into `LEDIT_PREVIEW_DIR`.
