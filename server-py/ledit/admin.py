# admin.py
from django.contrib import admin
from .models import (
    GeneralSettings,
    Image,
    Sonarr,
    Radarr,
    F1,
    Weather,
    HomeAssistant,
    Untapped,
    Video,
)

admin.site.register(GeneralSettings)
admin.site.register(Image)
admin.site.register(Sonarr)
admin.site.register(Radarr)

admin.site.register(F1)
admin.site.register(Weather)
admin.site.register(HomeAssistant)
admin.site.register(Untapped)

admin.site.register(Video)
