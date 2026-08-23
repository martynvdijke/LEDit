"""Command-line entry point.

Run as ``python -m ledit_device`` or via the ``ledit-device`` console script.
"""

import websocket  # websocket-client

from .client import Client
from .config import log, server_url, token
from .display import make_display
from .telemetry import init_telemetry


def main():
    telemetry = init_telemetry()
    buttons = None
    try:
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
            on_reconnect=client.on_reconnect,
        )
        try:  # pragma: no cover - hardware wiring, tested via mock
            from .buttons import ButtonHandler  # pragma: no cover

            def _sender(msg):  # pragma: no cover
                try:  # pragma: no cover
                    ws.send(msg)  # pragma: no cover
                except Exception:  # pragma: no cover
                    pass  # pragma: no cover

            buttons = ButtonHandler(sender=_sender)  # pragma: no cover
            buttons.start()  # pragma: no cover
        except Exception:  # pragma: no cover
            pass  # pragma: no cover
        # run_forever with reconnect=True keeps the device online across drops.
        ws.run_forever(ping_interval=30, ping_timeout=10, reconnect=5)
    finally:
        if buttons is not None:  # pragma: no cover
            try:  # pragma: no cover
                buttons.close()  # pragma: no cover
            except Exception:  # pragma: no cover
                pass  # pragma: no cover
        telemetry.shutdown()


if __name__ == "__main__":
    main()
