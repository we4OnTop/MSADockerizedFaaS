import docker
from datetime import datetime

class orchestrationManager:
    def __init__(self):
        self.client = docker.from_env()
        """        
        self.image = self.build_image(".", "Dockerfile", "tag")
        self.container_id = self.start_container(self.image)
        print(self.check_container_status(self.container_id))
        """


    def build_image(self, context_path: str, dockerfile: str, tag: str) -> str:
        image, _ = self.client.images.build(
            path=context_path,
            dockerfile=dockerfile,
            # tag=tag,
        )
        return image.tags[0] if image.tags else image.id

    def start_container(self, image_ref: str, **run_kwargs) -> str:
        container = self.client.containers.run(
            image_ref,
            detach=True,
            **run_kwargs,
        )
        return container.id

<<<<<<< HEAD
    def stop_container(self, container_id: str):
        self.client.containers.get(container_id).stop()
=======
    def stop_and_remove_container(self, container_id: str):
        container = self.client.containers.get(container_id)
        container.stop()
        container.remove()
>>>>>>> e405973 (change stop container function to stop and kill container)

    def check_container_status(self, container_id):
        container = self.client.containers.get(container_id=container_id)
        container.status
        stats = container.stats(stream=False)

        return stats.keys()
