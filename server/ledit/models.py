from django.db import models


class LEDitSettings(models.Model):
    timeout = models.FloatField(max_length=10)
    sonarr = models.BooleanField(default=False)
    radarr = models.BooleanField(default=False)
    f1 = models.BooleanField(default=False)
    wheater = models.BooleanField(default=False)
    homeassitant = models.BooleanField(default=False)
    untapped = models.BooleanField(default=False)
    stocks_tracker = models.BooleanField(default=False)
    crypto_tracker = models.BooleanField(default=False)
