import os
from django.core.validators import FileExtensionValidator
from django.db import models
from .render import Render
import base64


class Video(Render):
    video = models.FileField(
        upload_to="custom_videos",
        validators=[FileExtensionValidator(allowed_extensions=["mp4"])],
        help_text="Upload MP4 files only",
    )

    def get_png(self):
        """
        Returns the video file as base64 encoded string
        """
        if not os.path.exists(self.video.path):
            return {"format": "ERROR", "image": "File not found"}

        with open(self.video.path, "rb") as video_file:
            video_data = video_file.read()
            encoded_video = base64.b64encode(video_data)

            return {"format": "MP4", "image": str(encoded_video)}
