import os
import time
import json
import docker
import threading

PIPE_PATH = "/shared/logs/access.pipe"
client = None
ip_to_name_map = {}
request_counts = {}
map_lock = threading.Lock()

def setup_docker():
    global client
    try:
        client = docker.from_env()
        print("🐳 Verbunden mit Docker API.")
    except Exception as e:
        print(f"❌ Docker API Fehler: {e}")

def background_refresher():
    """Läuft unsichtbar im Hintergrund und aktualisiert die Namen"""
    global ip_to_name_map
    while True:
        if client:
            try:
                temp_map = {}
                # Dieser Aufruf ist langsam, aber blockiert jetzt nicht mehr den Log-Reader!
                for container in client.containers.list():
                    try:
                        name = container.name
                        networks = container.attrs['NetworkSettings']['Networks']
                        for _, net_data in networks.items():
                            ip = net_data['IPAddress']
                            if ip:
                                temp_map[ip] = name
                    except Exception:
                        pass

                with map_lock:
                    ip_to_name_map = temp_map
            except Exception:
                pass
        time.sleep(5) # Alle 5 Sek aktualisieren

def print_stats():
    # Terminal löschen (optional, sieht cooler aus)
    # print("\033[2J\033[H", end="")
    print("\n" + "="*60)
    print(f"📊 LIVE STATS (Instant)")
    print("="*60)
    for name, count in request_counts.items():
        short_name = (name[:38] + '..') if len(name) > 38 else name
        print(f"{short_name:<40} | {count}")
    print("="*60, flush=True) # WICHTIG: flush=True erzwingt Ausgabe

def process_log(entry):
    upstream_ip_full = entry.get("upstream_ip", "")
    ip_only = upstream_ip_full.split(":")[0] if upstream_ip_full else ""

    if not ip_only: return

    # Blitzschneller RAM-Zugriff (kein Docker Aufruf hier!)
    name = "UNKNOWN"
    with map_lock:
        name = ip_to_name_map.get(ip_only, "UNKNOWN")

    if name != "UNKNOWN":
        request_counts[name] = request_counts.get(name, 0) + 1
        print_stats()

def access_log_reader():
    print(f"👀 Watcher gestartet.", flush=True)

    # Hintergrund-Thread starten
    t = threading.Thread(target=background_refresher, daemon=True)
    t.start()

    while True:
        if not os.path.exists(PIPE_PATH):
            os.mkfifo(PIPE_PATH)
            os.chmod(PIPE_PATH, 0o666)

        try:
            # HIER IST DER FIX: buffering=1 (Zeilen-Pufferung)
            # Das sorgt dafür, dass Python sofort reagiert, wenn ein \n kommt
            with open(PIPE_PATH, "r", buffering=1) as pipe:
                while True:
                    line = pipe.readline()
                    if not line:
                        time.sleep(0.001) # Winziger Sleep ist ok
                        continue

                    try:
                        entry = json.loads(line)
                        process_log(entry)
                    except json.JSONDecodeError:
                        pass
        except Exception:
            time.sleep(1)

if __name__ == "__main__":
    setup_docker()
    access_log_reader()