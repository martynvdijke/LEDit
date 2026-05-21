from django.db import models
from .render import Render
import base64
from ..themes.f1 import F1_THEME


class F1(Render):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)

    def get_png(self):
        my_dict = {
            "Next Race": "Monaco GP",
            "Time": "14:00",
            "Leader": "Max Verstappen",
            "Points": "125",
        }
        image_bytes = self.render_dict(my_dict, theme=F1_THEME)
        encoded_string = base64.b64encode(image_bytes)
        message = {"format": "PNG", "image": str(encoded_string)}
        return message
