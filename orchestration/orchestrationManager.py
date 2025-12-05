import docker
from datetime import datetime

class OrchestrationManager:
    def __init__(self):
        self.client = docker.from_env()


    def build_image(self, context_path: str, dockerfile: str, tag: str) -> str:
        image, _ = self.client.images.build(
            path=context_path,
            dockerfile=dockerfile,
            # tag=tag,
        )
        return image.tags[0] if image.tags else image.id

    def start_container(self, image_ref: str, name: str | None = None, **run_kwargs) -> str:
        container = self.client.containers.run(
            image_ref,
            detach=True,
            name=name,
            **run_kwargs,
        )
        return container.id

    def stop_container(self, container_id: str):
        self.client.containers.get(container_id).stop()

    def check_container_status(self, container_id):
        container = self.client.containers.get(container_id=container_id)
        container.status
        stats = container.stats(stream=False)

        return stats.keys()
