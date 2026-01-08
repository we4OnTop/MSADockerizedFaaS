from orchestrationManager import OrchestrationManager
from redisMessangerConnector import RedisMessangerConnector


def main():
    orchestrator = OrchestrationManager()
    images = orchestrator.check_image_integrity()

    print("Pushing base clusters in envoy...")
    orchestrator.push_all_faas_clusters_in_envoy()
    print("Pushing base listeners in envoy...")
    orchestrator.push_listener_in_envoy()

    messangerConnector = RedisMessangerConnector(images, orchestrator)
    messangerConnector.listen_to_topic()

if __name__ == "__main__":
    main()
