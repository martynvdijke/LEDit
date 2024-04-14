# admin.py
from django.contrib import admin
from .models import GeneralSettings, Image

admin.site.register(GeneralSettings)
admin.site.register(Image)