# admin.py
from django.contrib import admin
from .models import (
    GeneralSettings,
    Image,
    Sonarr,
    Radarr,
    Readarr,
    Lidarr,
    F1,
    Weather,
    HomeAssistant,
    Untapped,
    StocksTracker,
    CyrptoTracker,
    Ical
)

admin.site.register(GeneralSettings)
admin.site.register(Image)
admin.site.register(Sonarr)
admin.site.register(Radarr)
admin.site.register(Readarr)
admin.site.register(Lidarr)
admin.site.register(F1)
admin.site.register(Weather)
admin.site.register(HomeAssistant)
admin.site.register(Untapped)
admin.site.register(StocksTracker)
admin.site.register(CyrptoTracker)
admin.site.register(Ical)