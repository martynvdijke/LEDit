from django.db import models
from PIL import Image, ImageDraw, ImageFont
from io import BytesIO
import base64
import os
import p5
import threading

class RenderModel(models.Model):

    class Meta:
        abstract = True

    # def __init__(self, width, height):
    #     self.width = width
    #     self.height = height

    def setup(self):
        pass


    def draw(self):
        p5.size(128,64)
        font = p5.load_font("PixelifySans.ttf")
        p5.text_font(font)
        p5.no_loop()
        p5.fill(0)
        p5.background(204)
        p5.text("LAXfdsf", (0, 10))
        p5.text("LHR", (0, 70))
        p5.text("TXL", (0, 100))
        file_path = "test.png"
        p5.save_frame(file_path)
        exit(0)


    def render(self):
        image = Image.new("RGB", (200, 200), "white")
        draw = ImageDraw.Draw(image)

        draw.rectangle((50, 50, 150, 150), fill="red")
        png_data = BytesIO()

        image.save(png_data, format="PNG")
        png_data.seek(0)  # Reset the stream position to the beginning

        return png_data.getvalue()
    
    def run(self):
        p5.run(sketch_setup=self.setup,sketch_draw=self.draw, mode="P2D", renderer="skia")

    def get_png(self):
        
        thread = threading.Thread(target=self.run)
        thread.start()
        thread.join()
        
        # width = 640
        # height = 640
        # image = Image.new("RGB", (width, height), "white")
        # draw = ImageDraw.Draw(image)
        # print(os.getcwd())
        # p5.run()
        # self.draw()


        # draw.rectangle((50, 50, 150, 150), fill="red")
        # font = ImageFont.truetype("PixelifySans.ttf")
        # text = self.url
        # draw.text((width/2, height/2), text, font=font, anchor="mm", fill="black")

        # png_data = BytesIO()
        # image.save(png_data, format="PNG")
        # png_data.seek(0)  # Reset the stream position to the beginning
        with open("test.png", "rb") as image_file:
            encoded_string = base64.b64encode(image_file.read())
            data = f"{encoded_string}"
        return data
