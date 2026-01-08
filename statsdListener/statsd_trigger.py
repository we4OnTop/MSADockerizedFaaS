import socket
import requests
import re
import os
import sys

# --- CONFIGURATION ---
UDP_IP = "0.0.0.0" # Must be 0.0.0.0 to listen inside Docker
UDP_PORT = 8125
# This hostname 'webhook' matches the service name in docker-compose
WEBHOOK_URL = os.getenv("WEBHOOK_URL", "http://webhook:5000/alert")

# Regex to capture the gauge value
# Example metric: envoy.cluster.service_cluster.upstream_rq_pending_active:5|g
PATTERN = re.compile(r"cluster\.service_cluster\.upstream_rq_pending_active:(\d+)\|g")

def start_listener():
    # Create UDP socket
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind((UDP_IP, UDP_PORT))

    print(f"✅ StatsD Trigger listening on UDP {UDP_IP}:{UDP_PORT}")
    print(f"   Target Webhook: {WEBHOOK_URL}")

    while True:
        try:
            # 1. Receive Data (Blocking wait)
            data, addr = sock.recvfrom(4096)
            message = data.decode("utf-8")

            print("--------------------------------------------------")
            print(f"📦 Received Packet from {addr}:")
            print(message)
            print("--------------------------------------------------")

            # 2. Parse Lines (StatsD sends multiple metrics separated by \n)
            for line in message.splitlines():
                match = PATTERN.search(line)
                if match:
                    queue_size = int(match.group(1))

                    # 3. Trigger Logic
                    if queue_size > 0:
                        print(f"🚨 QUEUE ALERT! Size: {queue_size} | Firing Webhook...")
                        try:
                            requests.post(WEBHOOK_URL, json={
                                "alert": "Queue Spike",
                                "queue_size": queue_size
                            }, timeout=1.0)
                        except Exception as e:
                            print(f"❌ Webhook failed: {e}")
                    else:
                        # Optional: Print heartbeat for empty queue
                        # print(f"Queue is clean: {queue_size}")
                        pass

        except Exception as e:
            print(f"Error in listener loop: {e}")

if __name__ == "__main__":
    start_listener()