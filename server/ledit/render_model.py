from django.db import models
from PIL import Image, ImageDraw, ImageFont
from io import BytesIO
import base64
import os
import p5

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

    def setup(self):
        p5.size(128,64)
        font = p5.create_font("PixelifySans.ttf", 32)
        p5.text_font(font)
        p5.no_loop()
        p5.fill(0)
        p5.background(204)

    def draw(self):
        p5.background(204)
        p5.text("LAX", (0, 40))
        p5.text("LHR", (0, 70))
        p5.text("TXL", (0, 100))
    
        p5.save_canvas("test.png")
    

    def get_png(self):
        width = 640
        height = 640
        image = Image.new("RGB", (width, height), "white")
        draw = ImageDraw.Draw(image)
        print(os.getcwd())
        p5.run()
        self.draw()


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
