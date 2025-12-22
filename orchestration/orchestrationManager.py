import docker
from datetime import datetime


class OrchestrationManager:
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
            ports={"5000/tcp": 0},
            detach=True,
            **run_kwargs,
        )
        return container.id

    def stop_and_remove_container(self, container_id: str):
        container = self.client.containers.get(container_id)
        container.stop()
        container.remove()

    def check_container_status(self, container_id):
        container = self.client.containers.get(container_id=container_id)
        container.status
        stats = container.stats(stream=False)

        return stats.keys()

def get_container_port(self, container_id: str) -> dict:
        container = self.client.containers.get(container_id)
        container.reload()
        ports = container.attrs["NetworkSettings"]["Ports"]
        binding = ports.get("8080/tcp")
        if not binding:
            return None

        return binding[0]["HostPort"]