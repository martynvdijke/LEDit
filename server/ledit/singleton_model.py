from django.db import models


class SingletonModel(models.Model):
    # Your fields here

    class Meta:
        abstract = True

    @classmethod
    def load(cls):
        try:
            return cls.objects.get()
        except cls.DoesNotExist:
            return cls()

    def save(self, *args, **kwargs):
        self.pk = 1  # Ensure that only one instance exists
        super().save(*args, **kwargs)

    def delete(self, *args, **kwargs):
        pass  # Prevent deletion of the singleton instance