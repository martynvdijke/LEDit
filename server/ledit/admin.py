# admin.py
from django.contrib import admin
from .models import (
    GeneralSettings,
    Image,
    SonarrSettings,
    RadarrSettings,
    F1Settings,
    WeatherSettings,
    HomeAssistantSettings,
    UntappedSettings,
    StocksTrackerSettings,
)

admin.site.register(GeneralSettings)
admin.site.register(Image)
admin.site.register(SonarrSettings)
admin.site.register(RadarrSettings)
admin.site.register(F1Settings)
admin.site.register(WeatherSettings)
admin.site.register(HomeAssistantSettings)
admin.site.register(UntappedSettings)
admin.site.register(StocksTrackerSettings)
