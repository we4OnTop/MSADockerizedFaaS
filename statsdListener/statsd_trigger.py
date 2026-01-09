import socket
import requests
import re
import os
import sys

# --- CONFIGURATION ---
UDP_IP = "0.0.0.0" # Must be 0.0.0.0 to listen inside Docker
UDP_PORT = 8125
# This hostname 'webhook' matches the service name in docker-compose
REDIS_MESSANGER_URL = os.getenv("REDIS_MESSANGER_URL", "http://redis-messanger:5000")
TO_IGNORE_CLUSTERS = ['xds_cluster', 'jaeger']

# Regex to capture the gauge value
# Example metric: envoy.cluster.service_cluster.upstream_rq_pending_active:5|g
PATTERN = re.compile(r"envoy\.cluster\.(?P<cluster_name>.*?)\.upstream_rq_pending_active:(?P<value>\d+)\|g")


def start_listener():
    LAST_UNIQUE_VALUE = 0
    # Create UDP socket
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind((UDP_IP, UDP_PORT))

    print(f"✅ StatsD Trigger listening on UDP {UDP_IP}:{UDP_PORT}")
    print(f"   Target Webhook: {REDIS_MESSANGER_URL}")

    while True:
        try:
            # 1. Receive Data (Blocking wait)
            data, addr = sock.recvfrom(4096)
            message = data.decode("utf-8")

            # 2. Parse Lines (StatsD sends multiple metrics separated by \n)
            for line in message.splitlines():
                match = PATTERN.search(line)
                if match:
                    cluster_name = match.group("cluster_name")
                    if cluster_name in TO_IGNORE_CLUSTERS:
                        pass

                    queue_size = int(match.group("value"))

                    print("START Size: ", queue_size)

                    if LAST_UNIQUE_VALUE != queue_size and LAST_UNIQUE_VALUE < queue_size:
                        print("Size: ", queue_size)
                        print(line)
                        LAST_UNIQUE_VALUE = queue_size
                        requests.post(f"{REDIS_MESSANGER_URL}/pushFAASIncrement", json={
                            "function-name": cluster_name,
                        })
                    else:
                        pass

        except Exception as e:
            print(f"Error in listener loop: {e}")

if __name__ == "__main__":
    start_listener()