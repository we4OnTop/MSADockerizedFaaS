import os
from concurrent.futures.thread import ThreadPoolExecutor

import redis


class RedisMessangerConnector:
    def __init__(self, available_images_dic, orchestrator):
        self.available_images_dic = available_images_dic

        self.orchestrator = orchestrator

        self.redis_topic = os.environ.get('FAAS_REDIS_TOPIC', 'newFAASCounter')

        self.host = os.environ.get('REDIS_URL', 'localhost')

        self.port = os.environ.get('REDIS_PORT', '6379')

        self.r = redis.Redis(

            host=self.host,

            port=int(self.port),

            decode_responses=True

        )

        self.executor = ThreadPoolExecutor(max_workers=5)
        # ... your existing init code ...
        self.r = redis.Redis(host=self.host, port=int(self.port), decode_responses=True)

        # Ensure Redis has Expiry notifications enabled via code
        try:
            self.r.config_set("notify-keyspace-events", "Ex")
        except redis.exceptions.ResponseError:
            print("⚠️ Warning: Could not set CONFIG. Ensure Redis allows config changes.")

    def listen_to_topic(self, topic):
        pubsub = self.r.pubsub()

        # We subscribe to two patterns:
        # 1. Your standard FAAS topics
        # 2. The internal Redis expiration events
        pubsub.psubscribe("FAASFunctions/*", "__keyevent@0__:expired")

        print("👂 Listening for FAAS events and TTL expirations...")

        for message in pubsub.listen():
            if message["type"] not in ("message", "pmessage"):
                continue

            channel = message.get("channel")
            data = message.get("data")  # For expired events, 'data' is the KEY NAME

            # --- CASE A: EXPIRED EVENT ---
            if "expired" in channel:
                print(f"⏰ TTL EXPIRED: Key '{data}' is gone.")
                # data is the name of the key that just died
                # Example: If key 'FAASFunctions/my-func' expires, data = 'FAASFunctions/my-func'
                self.handle_expiry(data)
                continue

            # --- CASE B: STANDARD FAAS EVENT ---
            if channel.startswith("FAASFunctions/"):
                print(f"\n📩 Standard Event on {channel}")
                # ... your existing logic for increments ...
                self.orchestrator.start_faas_container(channel.split("//")[1], "msadockerizedfaas-envoy-1")

                print(f"➕ Increment event → new value: {int(data)}")
                continue

    def handle_expiry(self, data):
        container_name = data.split(":")[0]
        self.orchestrator.stop_and_remove_container(container_name)
        print("Stopped " + container_name)