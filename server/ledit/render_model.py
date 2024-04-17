from django.db import models
from PIL import Image, ImageDraw, ImageFont
from io import BytesIO
import base64
import os

class RenderModel(models.Model):

    class Meta:
        abstract = True

    def render(self):
        image = Image.new("RGB", (200, 200), "white")
        draw = ImageDraw.Draw(image)

        draw.rectangle((50, 50, 150, 150), fill="red")
        png_data = BytesIO()
        image.save(png_data, format="PNG")
        png_data.seek(0)  # Reset the stream position to the beginning

        return png_data.getvalue()

    def get_png(self):
        width = 640
        height = 640
        image = Image.new("RGB", (width, height), "white")
        draw = ImageDraw.Draw(image)
        print(os.getcwd())

        draw.rectangle((50, 50, 150, 150), fill="red")
        font = ImageFont.truetype("PixelifySans.ttf")
        text = self.url
        draw.text((width/2, height/2), text, font=font, anchor="mm", fill="black")

        png_data = BytesIO()
        image.save(png_data, format="PNG")
        png_data.seek(0)  # Reset the stream position to the beginning
        base64_str = base64.b64encode(png_data.getvalue())
        data = f"{base64_str}"
        return data
