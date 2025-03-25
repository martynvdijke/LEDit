import time
from channels.generic.websocket import WebsocketConsumer
from .models import GeneralSettings
import random

class ChatConsumer(WebsocketConsumer):
    def connect(self):
        self.accept()
        object = GeneralSettings.objects.get(pk=1)
        all = []
        if object.sonarr.exists():
            all.append(object.sonarr.all()) 
        if object.radarr.exists():
            all.append(object.radarr.all()) 
        if object.readarr.exists():
            all.append(object.readarr.all()) 
        if object.lidarr.exists():
            all.append(object.lidarr.all()) 
        if object.f1.exists():
            all.append(object.f1.all()) 
        if object.wheater.exists():
            all.append(object.wheater.all())
        if object.homeassitant.exists():
            all.append(object.homeassitant.all())
        if object.untapped.exists():
            all.append(object.untapped.all())
        if object.stocks_tracker.exists():
            all.append(object.stocks_tracker.all())
        if object.images.exists():
            all.append(object.images.all())
            
        if object.random:
            random.shuffle(all)
        
        print(object.f1.exists(), object.untapped.exists())
            
        while True:
            for data_sources in all:
                for data_source in data_sources:
                    print(data_source)
                    data = data_source.get_png()
                    self.send(text_data=data)

                    time.sleep(object.timeout)

    def disconnect(self, close_code):
        pass

    def receive(self, text_data):
        pass
