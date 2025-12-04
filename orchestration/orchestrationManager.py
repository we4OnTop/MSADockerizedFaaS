import docker 

class orchestrationManager:
    def __init__(self):
        self.client = docker.from_env()

        # TODO: Build images 
        # TODO: Start base image 

    def startContainer(self, imageName):
        self.client.containers.run(imageName, detach=True)

    def stopContainer(self, containerID):
        self.client.containers.get(container_id=containerID).stop()
