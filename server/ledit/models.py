from django.db import models
from ledit.submodels.sonarr import Sonarr
from ledit.submodels.radarr import Radarr
from ledit.submodels.f1 import F1
from ledit.submodels.wheater import Weather
from ledit.submodels.homeassitant import HomeAssistant
from ledit.submodels.untapped import Untapped
from ledit.submodels.image import Image
from ledit.submodels.video import Video



class GeneralSettings(models.Model):
    timeout = models.FloatField(max_length=10)
    random = models.BooleanField()
    width = models.IntegerField(default="64")
    height = models.IntegerField(default="64")

    sonarr = models.ManyToManyField(Sonarr, blank=True)
    radarr = models.ManyToManyField(Radarr, blank=True)

    f1 = models.ManyToManyField(F1, blank=True)
    wheater = models.ManyToManyField(Weather, blank=True)
    homeassitant = models.ManyToManyField(HomeAssistant, blank=True)
    untapped = models.ManyToManyField(Untapped, blank=True)

    images = models.ManyToManyField(Image, blank=True)
    videos = models.ManyToManyField(Video, blank=True)
