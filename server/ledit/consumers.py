import base64

from channels.generic.websocket import WebsocketConsumer


class ChatConsumer(WebsocketConsumer):
    def connect(self):
        self.accept()

        while True:
            with open("ledit/test.png", "rb") as image_file:
                encoded_string = base64.b64encode(image_file.read())
                data = f"{encoded_string}"
                self.send(text_data=data)

    def disconnect(self, close_code):
        pass

    def receive(self, text_data):
        pass
