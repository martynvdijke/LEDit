from django.db import models
from .render_model import RenderModel
import base64
from .themes import F1_THEME, UNTAPPD_THEME


class Image(RenderModel):
    image = models.ImageField(upload_to="custom_images")
    
    def get_png(self):
        with open(self.image.path, "rb") as image_file:
            encoded_string = base64.b64encode(image_file.read())
            data = f"{encoded_string}"
        return data


class DeviceSettings(models.Model):
    ip = models.GenericIPAddressField()
    port = models.IntegerField(default="6270")
    username = models.CharField(max_length=100)
    password = models.CharField(max_length=100)
    width = models.IntegerField(default="64")
    height = models.IntegerField(default="64")


class Sonarr(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)

    def get_png(self):
        my_dict = {
        "name": "Test Project",
        "version": "1.0",
        "status": "active",
        "date": "2024-03-25"
        }
        image_bytes = self.render_dict(my_dict)
        encoded_string = base64.b64encode(image_bytes)
        data = f"{encoded_string}"
        return data

class Radarr(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)


class Readarr(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)


class Lidarr(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)


class F1(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)
    def get_png(self):
        my_dict = {
            "Next Race": "Monaco GP",
            "Time": "14:00",
            "Leader": "Max Verstappen",
            "Points": "125"
        }
        image_bytes = self.render_dict(my_dict, theme=F1_THEME)
        encoded_string = base64.b64encode(image_bytes)
        return f"{encoded_string}"

class Weather(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)


class HomeAssistant(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)


class Untapped(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)
    def get_png(self):
        my_dict = {
            "Last Beer": "IPA",
            "Rating": "4.5/5",
            "ABV": "6.5%",
            "Brewery": "Craft Bros"
        }
        image_bytes = self.render_dict(my_dict, theme=UNTAPPD_THEME)
        encoded_string = base64.b64encode(image_bytes)
        return f"{encoded_string}"

class StocksTracker(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)


class CyrptoTracker(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)


class Ical(RenderModel):
    token = models.CharField(max_length=100, default="")
    url = models.CharField(max_length=100)


class GeneralSettings(models.Model):
    timeout = models.FloatField(max_length=10)
    random = models.BooleanField()
    width = models.IntegerField(default="64")
    height = models.IntegerField(default="64")

    sonarr = models.ManyToManyField(Sonarr, blank=True)
    radarr = models.ManyToManyField(Radarr, blank=True)
    readarr = models.ManyToManyField(Readarr, blank=True)
    lidarr = models.ManyToManyField(Lidarr, blank=True)

    f1 = models.ManyToManyField(F1, blank=True)
    wheater = models.ManyToManyField(Weather, blank=True)
    homeassitant = models.ManyToManyField(HomeAssistant, blank=True)
    untapped = models.ManyToManyField(Untapped, blank=True)
    stocks_tracker = models.ManyToManyField(StocksTracker, blank=True)

    images = models.ManyToManyField(Image, blank=True)
