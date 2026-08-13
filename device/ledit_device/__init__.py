"""LEDit device client package.

Streams frames from a LEDit server over WebSocket and renders them onto an
RGB LED matrix (HUB75 panels via ``rpi-rgb-led-matrix``).

The device connects OUT to the server, so no inbound port or static IP is
required. Each device authenticates with its own token (admin -> Devices).
"""

from .client import Client
from .display import Display, FileDisplay, MatrixDisplay

__version__ = "1.0.0"

__all__ = ["Client", "Display", "MatrixDisplay", "FileDisplay", "__version__"]
