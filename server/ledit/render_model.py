from django.db import models
from PIL import Image, ImageDraw, ImageFont
from io import BytesIO
import base64
from .themes import DEFAULT_THEME, CYBER_THEME, F1_THEME, UNTAPPD_THEME


class RenderModel(models.Model):
    class Meta:
        abstract = True

    def setup(self):
        pass

    def render(self):
        image = Image.new("RGB", (200, 200), "white")
        draw = ImageDraw.Draw(image)

        draw.rectangle((50, 50, 150, 150), fill="red")
        png_data = BytesIO()

        image.save(png_data, format="PNG")
        png_data.seek(0)  # Reset the stream position to the beginning

        return png_data.getvalue()

    def render_dict(self, data_dict, image_size=(400, 400), theme=DEFAULT_THEME):
        """
        Renders a dictionary into a pixel-art style image with theme support
        Args:
            data_dict (dict): Dictionary containing data to render
            image_size (tuple): Size of the output image (width, height)
            theme (Theme): Theme object containing styling information
        """
        # Create base image with theme colors
        image = Image.new("RGB", image_size, theme.background_color)
        draw = ImageDraw.Draw(image)
        
        try:
            font = ImageFont.truetype("fonts/PixelifySans.ttf", theme.font_size)
        except:
            font = ImageFont.load_default()
        
        # Draw pixelated border
        pixel_size = 8
        for i in range(0, image_size[0], pixel_size):
            # Top border
            draw.rectangle([i, 0, i+pixel_size-1, pixel_size-1], 
                        fill=theme.accent_color if i % (pixel_size*2) == 0 else theme.text_color)
            # Bottom border
            draw.rectangle([i, image_size[1]-pixel_size, i+pixel_size-1, image_size[1]], 
                        fill=theme.accent_color if i % (pixel_size*2) != 0 else theme.text_color)
        
        for i in range(0, image_size[1], pixel_size):
            # Left border
            draw.rectangle([0, i, pixel_size-1, i+pixel_size-1], 
                        fill=theme.accent_color if i % (pixel_size*2) == 0 else theme.text_color)
            # Right border
            draw.rectangle([image_size[0]-pixel_size, i, image_size[0], i+pixel_size-1], 
                        fill=theme.accent_color if i % (pixel_size*2) != 0 else theme.text_color)
        
        # Calculate text positioning
        y_position = 50
        margin = 40
        
        # Draw title from theme
        draw.text((margin, 20), theme.title, font=font, fill=theme.accent_color)
        
        # Draw separator line
        draw.line([(margin, y_position), (image_size[0]-margin, y_position)], 
                fill=theme.text_color, width=2)
        y_position += 20
        
        # Draw each key-value pair
        for key, value in data_dict.items():
            # Draw pixel marker
            draw.rectangle([margin-15, y_position+8, margin-7, y_position+16], 
                        fill=theme.accent_color)
            
            # Draw text
            text = f"{key}: {value}"
            draw.text((margin, y_position), text, font=font, fill=theme.text_color)
            y_position += 35
        
        # Add scanline effect
        for y in range(0, image_size[1], 4):
            draw.line([(0, y), (image_size[0], y)], 
                    fill=(0, 0, 0, 50), width=1)
        
        # Convert to bytes
        png_data = BytesIO()
        image.save(png_data, format="PNG")
        png_data.seek(0)
        
        return png_data.getvalue()

    def get_png(self):
        with open("test.png", "rb") as image_file:
            encoded_string = base64.b64encode(image_file.read())
            data = f"{encoded_string}"
        return data
