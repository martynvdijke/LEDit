from django.db import models
from .render import Render


class HomeAssistant(Render):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)
