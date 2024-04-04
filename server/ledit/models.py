from django.db import models


class SiteSettings(models.Model):
    site_name = models.CharField(max_length=100)
    logo = models.ImageField(upload_to="logos/")
    email = models.EmailField()
    # Add more fields as needed


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
