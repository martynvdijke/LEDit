from django.db import models

class Image(models.Model):
    image = models.ImageField(upload_to="custom_images")

class DeviceSettings(models.Model):
    ip = models.GenericIPAddressField()
    port = models.IntegerField(default="6270")
    username = models.CharField(max_length=100)
    password = models.CharField(max_length=100)
    width = models.IntegerField(default="64")
    height = models.IntegerField(default="64")

class SonarrSettings(models.Model):
    token = models.CharField(max_length=100)
    url = models.CharField(max_length=100)

class RadarrSettings(models.Model):
    token = models.CharField(max_length=100)
    url = models.CharField(max_length=100)

class F1Settings(models.Model):
    pass

class WeatherSettings(models.Model):
    pass

class HomeAssistantSettings(models.Model):
    token = models.CharField(max_length=100)
    url = models.CharField(max_length=100)

class UntappedSettings(models.Model):
    pass

class StocksTrackerSettings(models.Model):
    pass

class GeneralSettings(models.Model):
    timeout = models.FloatField(max_length=10)
    sonarr = models.BooleanField(default=False)
    radarr = models.BooleanField(default=False)
    f1 = models.BooleanField(default=False)
    wheater = models.BooleanField(default=False)
    homeassitant = models.BooleanField(default=False)
    untapped = models.BooleanField(default=False)
    stocks_tracker = models.BooleanField(default=False)
    crypto_tracker = models.BooleanField(default=False)
    images = models.ManyToManyField(Image)
