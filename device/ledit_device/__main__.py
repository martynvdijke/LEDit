"""Command-line entry point.

Run as ``python -m ledit_device`` or via the ``ledit-device`` console script.
"""

import websocket  # websocket-client

from .client import Client
from .config import log, server_url, token
from .display import make_display


def main():
    token_value = token()
    url = "%s/ws/device/%s" % (server_url(), token_value)

    display = make_display()
    client = Client(display)
    log("info", "connecting to %s (matrix %dx%d)" % (url, display.width, display.height))

    ws = websocket.WebSocketApp(
        url,
        on_message=client.on_message,
        on_error=client.on_error,
        on_close=client.on_close,
    )
    # run_forever with reconnect=True keeps the device online across drops.
    ws.run_forever(ping_interval=30, ping_timeout=10, reconnect=5)


if __name__ == "__main__":
    main()
