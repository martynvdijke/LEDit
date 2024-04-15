from django.db import models
# from .singleton_model import models.Model


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
    token = models.CharField(max_length=100, default='')
    url = models.CharField(max_length=100)


class RadarrSettings(models.Model):
    token = models.CharField(max_length=100, default='')
    url = models.CharField(max_length=100)


class F1Settings(models.Model):
    token = models.CharField(max_length=100, default='')
    url = models.CharField(max_length=100)


class WeatherSettings(models.Model):
    token = models.CharField(max_length=100, default='')
    url = models.CharField(max_length=100)


class HomeAssistantSettings(models.Model):
    token = models.CharField(max_length=100, default='')
    url = models.CharField(max_length=100)


class UntappedSettings(models.Model):
    token = models.CharField(max_length=100, default='')
    url = models.CharField(max_length=100)


class StocksTrackerSettings(models.Model):
    token = models.CharField(max_length=100, default='')
    url = models.CharField(max_length=100)


class GeneralSettings(models.Model):
    timeout = models.FloatField(max_length=10)
    sonarr = models.OneToOneField(
        SonarrSettings,
        on_delete=models.CASCADE,
    )
    radarr = models.OneToOneField(
        RadarrSettings,
        on_delete=models.CASCADE,
    )
    f1 = models.OneToOneField(
        F1Settings,
        on_delete=models.CASCADE,
    )
    wheater = models.OneToOneField(
        WeatherSettings,
        on_delete=models.CASCADE,
    )
    homeassitant = models.OneToOneField(
        HomeAssistantSettings,
        on_delete=models.CASCADE,
    )
    untapped = models.OneToOneField(
        UntappedSettings,
        on_delete=models.CASCADE,
    )
    stocks_tracker = models.OneToOneField(
        StocksTrackerSettings,
        on_delete=models.CASCADE,
    )

    images = models.ManyToManyField(Image)
