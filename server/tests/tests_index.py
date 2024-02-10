from django.test import Client
from django.test import TestCase

client = Client()

class IndexView(TestCase):
    

    def test_index(self):
        print("test")
        response = client.get("/")
        assert response.status_code == 200