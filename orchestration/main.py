from orchestrationManager import OrchestrationManager
from redisMessangerConnector import RedisMessangerConnector


def main():
    orchestrator = OrchestrationManager()
    images = orchestrator.check_image_integrity()
    print(orchestrator.push_all_faas_clusters_in_envoy())
    print(orchestrator.push_listener_in_envoy())
    messangerConnector = RedisMessangerConnector(images, orchestrator)
    print("hjdshj")
    messangerConnector.listen_to_topic("newFAASCounter")
    print("listening to topic newFAASCounter")

if __name__ == "__main__":
    main()
