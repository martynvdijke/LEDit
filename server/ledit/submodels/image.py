import os
from django.db import models
from .render import Render
import base64


class Image(Render):
    image = models.ImageField(upload_to="custom_images")

    def get_png(self):
        # Get file extension from path
        _, extension = os.path.splitext(self.image.path)
        # Remove the dot and convert to uppercase
        format_type = extension[1:].upper()

        # Validate format
        supported_formats = ["PNG", "JPEG", "JPG", "GIF"]
        if format_type not in supported_formats:
            format_type = "PNG"  # Default to PNG if unsupported format

        # Handle JPG extension
        if format_type == "JPG":
            format_type = "JPEG"

        with open(self.image.path, "rb") as image_file:
            encoded_string = base64.b64encode(image_file.read())
            message = {"format": format_type, "image": str(encoded_string)}
            return message
