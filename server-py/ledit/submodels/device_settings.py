from django.db import models


class DeviceSettings(models.Model):
    ip = models.GenericIPAddressField()
    port = models.IntegerField(default="6270")
    username = models.CharField(max_length=100)
    password = models.CharField(max_length=100)
    width = models.IntegerField(default="64")
    height = models.IntegerField(default="64")
