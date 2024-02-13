import json
import base64
import os

from channels.generic.websocket import WebsocketConsumer


class ChatConsumer(WebsocketConsumer):
    def connect(self):
        self.accept()

    def disconnect(self, close_code):
        pass

    def receive(self, text_data):
        text_data_json = json.loads(text_data)
        message = text_data_json["message"]
        with open("ledit/test.png", "rb") as image_file:
            encoded_string = base64.b64encode(image_file.read())
            data = f"{encoded_string}"
            self.send(text_data=data)

#https://stackoverflow.com/questions/62325555/django-channels-sending-data-on-connect