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
        self.counter_tracker = {}

        self.executor = ThreadPoolExecutor(max_workers=5)
        self.r = redis.Redis(host=self.host, port=int(self.port), decode_responses=True)

        try:
            self.r.config_set("notify-keyspace-events", "Ex")
        except redis.exceptions.ResponseError:
            print("Warning: Could not set config. Ensure Redis allows config changes.")

    def listen_to_topic(self):
        pubsub = self.r.pubsub()

        pubsub.psubscribe("FAASFunctions/*", "__keyevent@0__:expired")

        ascii_text = """
        ███████╗ █████╗  █████╗ ███████╗                                  
        ██╔════╝██╔══██╗██╔══██╗██╔════╝                                  
        █████╗  ███████║███████║███████╗                                  
        ██╔══╝  ██╔══██║██╔══██║╚════██║                                  
        ██║     ██║  ██║██║  ██║███████║                                  
        ╚═╝     ╚═╝  ╚═╝╚═╝  ╚═╝╚══════╝                                  
                                                                          
        ██████╗ ██╗   ██╗███╗   ██╗███╗   ██╗██╗███╗   ██╗ ██████╗ ██╗    
        ██╔══██╗██║   ██║████╗  ██║████╗  ██║██║████╗  ██║██╔════╝ ██║    
        ██████╔╝██║   ██║██╔██╗ ██║██╔██╗ ██║██║██╔██╗ ██║██║  ███╗██║    
        ██╔══██╗██║   ██║██║╚██╗██║██║╚██╗██║██║██║╚██╗██║██║   ██║╚═╝    
        ██║  ██║╚██████╔╝██║ ╚████║██║ ╚████║██║██║ ╚████║╚██████╔╝██╗    
        ╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝╚═╝  ╚═══╝╚═╝╚═╝  ╚═══╝ ╚═════╝ ╚═╝    
                                                                          
           ██████╗                                                        
        ██╗██╔══██╗                                                       
        ╚═╝██║  ██║                                                       
        ██╗██║  ██║                                                       
        ╚═╝██████╔╝                                                       
           ╚═════╝                                                        
                                                                                                                                                                                
        """
        print(ascii_text)
        print("\n\nListening for FAAS events and TTL expirations...")

        for message in pubsub.listen():
            if message["type"] not in ("message", "pmessage"):
                continue

            channel = message.get("channel")
            data = message.get("data")

            if "expired" in channel:
                print(f"\nTTL EXPIRED: Container '{data}' is going to be destroyed...")
                self.handle_expiry(data)
                continue

            if channel.startswith("FAASFunctions/"):
                increment_event = True

                for cluster_name, item in self.counter_tracker.items():
                    if cluster_name == channel:
                        increment_event = item < int(data)

                self.counter_tracker[channel] = int(data)

                if increment_event:
                    print(f"\n=>> Standard Event on {channel}")
                    self.orchestrator.start_faas_container(channel.split("//")[1], "msadockerizedfaas-envoy-1")
                else:
                    print("Other Event...ignored")
                continue

    def handle_expiry(self, data):
        container_name = data.split(":")[0]
        self.orchestrator.stop_and_remove_container(container_name)
        print("Successfully stopped " + container_name)