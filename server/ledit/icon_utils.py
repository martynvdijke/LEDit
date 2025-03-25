from PIL import ImageFont
from dataclasses import dataclass
from typing import Dict, Any

@dataclass
class IconConfig:
    font_path: str
    unicode_char: str
    size: int
    color: tuple

class IconManager:
    def __init__(self):
        self.icon_font = None
        self.pixel_font = None
        try:
            self.icon_font = ImageFont.truetype("fonts/materialdesignicons-webfont.ttf", 24)
            self.pixel_font = ImageFont.truetype("fonts/PixelifySans.ttf", 24)
        except:
            print("Warning: Icon fonts not loaded")
    
    def draw_icon(self, draw, x, y, icon_config: IconConfig):
        try:
            font = ImageFont.truetype(icon_config.font_path, icon_config.size)
            draw.text((x, y), icon_config.unicode_char, font=font, fill=icon_config.color)
        except:
            # Fallback to a pixel square if font loading fails
            draw.rectangle([x, y, x + 10, y + 10], fill=icon_config.color)