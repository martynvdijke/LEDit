from dataclasses import dataclass
from typing import Tuple, List
from PIL import Image, ImageDraw

@dataclass
class Icon:
    coordinates: List[Tuple[int, int]]
    color: Tuple[int, int, int]
    size: int = 24

class Theme:
    def __init__(self, name, background_color, accent_color, text_color, title, font_size=24):
        self.name = name
        self.background_color = background_color
        self.accent_color = accent_color
        self.text_color = text_color
        self.title = title
        self.font_size = font_size

    def draw_icon(self, draw: ImageDraw, x: int, y: int):
        if self.icon:
            for point in self.icon.coordinates:
                draw.rectangle(
                    [
                        x + point[0], 
                        y + point[1], 
                        x + point[0] + self.icon.size, 
                        y + point[1] + self.icon.size
                    ],
                    fill=self.icon.color
                )

# Predefined themes
CYBER_THEME = Theme(
    "cyber",
    background_color=(40, 42, 54),    # Dark backdrop
    accent_color=(80, 250, 123),      # Neon green
    text_color=(139, 233, 253),       # Cyan
    title="SYSTEM STATUS"
)

F1_THEME = Theme(
    "f1",
    background_color=(33, 33, 33),    # Dark grey
    accent_color=(255, 24, 1),        # F1 Red
    text_color=(255, 255, 255),       # White
    title="F1 STATUS",
    font_size=28
)

UNTAPPD_THEME = Theme(
    "untappd",
    background_color=(255, 196, 37),  # Untappd Yellow
    accent_color=(76, 89, 104),       # Untappd Blue
    text_color=(33, 33, 33),          # Dark Grey
    title="BEER STATUS"
)

DEFAULT_THEME = CYBER_THEME