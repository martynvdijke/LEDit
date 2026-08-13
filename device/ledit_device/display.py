"""Display surfaces: a hardware RGB matrix or a file-based preview."""

import os
import sys

from .config import env_int, log

try:
    from PIL import Image, ImageDraw, ImageFont
except ImportError:  # pragma: no cover
    sys.stderr.write("Pillow is required: pip install pillow\n")
    raise


def _truetype_font(size):
    candidates = [
        "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
        "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
    ]
    for path in candidates:
        if os.path.exists(path):
            return ImageFont.truetype(path, size)
    return ImageFont.load_default()


class Display:
    """Abstraction over the output surface (hardware matrix or file preview)."""

    @property
    def width(self):
        raise NotImplementedError

    @property
    def height(self):
        raise NotImplementedError

    def show(self, image):
        raise NotImplementedError


class MatrixDisplay(Display):
    def __init__(self):
        from rgbmatrix import RGBMatrix, RGBMatrixOptions  # deferred import

        options = RGBMatrixOptions()
        options.rows = env_int("LEDIT_ROWS", 64)
        options.cols = env_int("LEDIT_COLS", 64)
        options.chain_length = env_int("LEDIT_CHAIN", 1)
        options.parallel = env_int("LEDIT_PARALLEL", 1)
        options.hardware_mapping = os.getenv("LEDIT_HARDWARE_MAPPING", "regular")
        options.brightness = env_int("LEDIT_BRIGHTNESS", 80)
        options.gpio_slowdown = env_int("LEDIT_GPIO_SLOWDOWN", 1)
        options.pwm_bits = 11
        self.matrix = RGBMatrix(options=options)

    @property
    def width(self):
        return self.matrix.width

    @property
    def height(self):
        return self.matrix.height

    def show(self, image):
        canvas = self.matrix.CreateFrameCanvas()
        canvas.SetImage(image)
        self.matrix.SwapOnVSync(canvas)


class FileDisplay(Display):
    """Writes each frame as a PNG file; for testing without hardware."""

    def __init__(self, outdir, width, height):
        os.makedirs(outdir, exist_ok=True)
        self.outdir = outdir
        self._width = width
        self._height = height
        self.counter = 0

    @property
    def width(self):
        return self._width

    @property
    def height(self):
        return self._height

    def show(self, image):
        self.counter += 1
        path = os.path.join(self.outdir, "frame_%06d.png" % self.counter)
        image.save(path)
        log("info", "saved frame %s" % path)


def make_display():
    preview = os.getenv("LEDIT_PREVIEW_DIR")
    if preview:
        return FileDisplay(preview, env_int("LEDIT_COLS", 64), env_int("LEDIT_ROWS", 64))
    try:
        return MatrixDisplay()
    except ImportError:
        log("warning", "rpi-rgb-led-matrix not installed; falling back to preview mode")
        return FileDisplay("/tmp/ledit_frames", env_int("LEDIT_COLS", 64), env_int("LEDIT_ROWS", 64))
