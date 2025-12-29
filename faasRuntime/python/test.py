import socket
import subprocess

BIND_IP = "127.0.0.1"
BIND_PORT = 8125
THRESHOLD = 1

sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
sock.bind((BIND_IP, BIND_PORT))

print("👀 Watching ALL clusters for pending requests...")

while True:
    data, _ = sock.recvfrom(128)
    message = data.decode('utf-8')

    # Message looks like: "cluster.backend_api.upstream_rq_pending_active:5|g"

    if "pending_active" in message:
        try:
            # 1. Parse the Value (the number of pending requests)
            # Split by ':' first -> ["cluster.backend_api.upstream...", "5|g"]
            parts = message.split(':')
            metric_path = parts[0]
            value_part = parts[1].split('|')[0]
            queue_size = int(value_part)

            if queue_size >= THRESHOLD:
                # 2. Parse the Cluster Name
                # metric_path is "cluster.backend_api.upstream_rq_pending_active"
                # Split by '.' -> ['cluster', 'backend_api', 'upstream_rq_pending_active']
                name_parts = metric_path.split('.')

                # The cluster name is usually at index 1
                cluster_name = name_parts[1]

                print(f"🚨 ALERT: Cluster '{cluster_name}' is struggling! Queue: {queue_size}")

                # 3. Take Action based on the specific cluster
                if cluster_name == "payments_service":
                    subprocess.Popen(["docker", "run", "-d", "payments:latest"])
                elif cluster_name == "users_service":
                    subprocess.Popen(["docker", "run", "-d", "users:latest"])

        except Exception as e:
            print(f"Error parsing: {e}")