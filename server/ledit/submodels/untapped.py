from django.db import models
from .render import Render
import base64
from ..themes.untapped import UNTAPPED_THEME


class Untapped(Render):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)

    def get_png(self):
        my_dict = {
            "Last Beer": "IPA",
            "Rating": "4.5/5",
            "ABV": "6.5%",
            "Brewery": "Craft Bros",
        }
        image_bytes = self.render_dict(my_dict, theme=UNTAPPED_THEME)
        encoded_string = base64.b64encode(image_bytes)
        message = {"format": "PNG", "image": str(encoded_string)}
        return message
