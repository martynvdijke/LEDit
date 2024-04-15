import base64
import time
from channels.generic.websocket import WebsocketConsumer
from .models import GeneralSettings

class ChatConsumer(WebsocketConsumer):
    def connect(self):
        self.accept()
        object = GeneralSettings.objects.get(pk=1)

        while True:
            images = object.images.all()
            for image in images:
                with open(image.image.path, "rb") as image_file:
                    encoded_string = base64.b64encode(image_file.read())
                    data = f"{encoded_string}"
                    self.send(text_data=data)
            
                time.sleep(object.timeout)

    def disconnect(self, close_code):
        pass

    def receive(self, text_data):
        pass
