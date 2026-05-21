from dataclasses import dataclass
from typing import Tuple, List
from PIL import ImageDraw


@dataclass
class Icon:
    coordinates: List[Tuple[int, int]]
    color: Tuple[int, int, int]
    size: int = 24


class Theme:
    def __init__(
        self, name, background_color, accent_color, text_color, title, font_size=24
    ):
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
                        y + point[1] + self.icon.size,
                    ],
                    fill=self.icon.color,
                )
