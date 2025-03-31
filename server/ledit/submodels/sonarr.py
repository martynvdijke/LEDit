from django.db import models
from .render import Render
import base64


class Sonarr(Render):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)

    def get_png(self):
        my_dict = {
            "name": "Test Project",
            "version": "1.0",
            "status": "active",
            "date": "2024-03-25",
        }
        image_bytes = self.render_dict(my_dict)
        encoded_string = base64.b64encode(image_bytes)
        message = {"format": "PNG", "image": str(encoded_string)}
        return message
