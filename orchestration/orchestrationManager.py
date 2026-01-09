import asyncio
import os
import subprocess
import threading
import uuid

import redis
from asyncore import loop
from concurrent.futures.thread import ThreadPoolExecutor
from pathlib import Path

import docker

import requests
import yaml


class OrchestrationManager:
    def __init__(self):
        faas_config_yaml_path = os.environ.get(
            "FAAS_CONFIG_YAML_PATH",
            "./../faasRuntime/global-function-definition.yml"
        )
        self.redis_host = os.environ.get('REDIS_URL', 'localhost')
        self.faas_root_folder_path = os.environ.get("FAAS_ROOT_FOLDER_PATH", "./../faasRuntime")
        self.BASE_FAAS_PORT = os.environ.get("BASE_FAAS_PORT", "5000")
        self.ENVOY_XDS_ADDRESS = os.environ.get("ENVOY_XDS_ADDRESS", "xds_connector:8081")
        self.client = docker.from_env()

        self.active_containers_dict = {}

        with open(faas_config_yaml_path, "r") as f:
            try:
                self.CONFIGURATION_FAAS_YAML = yaml.safe_load(f)
                print("YAML syntax is valid.")
            except yaml.YAMLError as exc:
                print(f"Syntax error in YAML file: {exc}")

    def check_image_integrity(self):
        current_images = []
        working_images = {}
        deprecated_images = []
        functions_dict = {}

        futures = []

        for func_id, func_data in self.CONFIGURATION_FAAS_YAML["usable-functions"].items():
            func_name = func_data.get("name") or func_id
            version = func_data["version"]
            current_images.append(f"{func_name}:{version}")

            key = func_data.get("name") or func_id
            functions_dict[key] = func_data

        images = self.client.images.list()

        existing_tags = [tag for image in images for tag in image.tags]
        existing_tags_without_version = [tag.split(":")[0] for tag in existing_tags]

        for image_name in current_images:
            print(f"Current Image: {image_name}")
            print(f"Existing Image: {existing_tags}")
            if image_name in existing_tags:
                working_images[f"{image_name.split(":")[0]}"] = image_name
            elif image_name.split(":")[0] in existing_tags_without_version:
                deprecated_images.append(image_name)

        if len(deprecated_images) > 0:
            self.clear_deprecated_images(deprecated_images)

        not_found_images = [img for img in current_images if img not in working_images.values()]

        built_images = {}

        missing_image_executor = ThreadPoolExecutor(max_workers=5)
        for image_name in not_found_images:
            print(f"Rebuilding Image: {image_name}")
            split_image_name = image_name.split(":")
            configuration_data = functions_dict.get(split_image_name[0])["configuration"]

            arg_payload = {
            "EXTRA_DEPS": configuration_data["extra_dependencies"],
            "APP_FILE": configuration_data["base_function_file_name"],
            "FUNCTION_DATA_DIR": f"{configuration_data["function_folder"]}",
            }

            for item in Path.cwd().iterdir():
                print(item)

            futures.append(
                missing_image_executor.submit(
                    self.build_image,
                    context_path=f"{self.faas_root_folder_path}/{configuration_data["programing_language"]}",
                    dockerfile=f"Dockerfile",
                    function_name=split_image_name[0],
                    version=split_image_name[1],
                    buildargs=arg_payload,
                )
            )
            built_images[split_image_name[0]] = image_name

        for future in futures:
            future.result()
        missing_image_executor.shutdown(wait=True)

        image_dict = working_images | built_images
        print(f"Images: {image_dict}")
        return image_dict


    def build_image(self, context_path: str, dockerfile: str, function_name: str, version: str, buildargs: dict[str, str]):
        print(f"[{function_name}] Starting build...")

        # Use the low-level API (client.api) to get a stream of logs
        response_stream = self.client.api.build(
            path=context_path,
            dockerfile=dockerfile,
            tag=f"{function_name}:{version}",
            buildargs=buildargs,
            nocache=False,  # Keep this FALSE for speed!
            rm=True,        # Remove intermediate containers
            decode=True     # Decodes the JSON stream automatically
        )

        for chunk in response_stream:
            # 1. Catch Errors immediately
            if 'error' in chunk:
                raise Exception(f"[{function_name}] Build Error: {chunk['error']}")

            # 2. Print "Step" logs (e.g., "Step 1/5 : RUN pip install...")
            if 'stream' in chunk:
                line = chunk['stream'].strip()
                if line:
                    print(f"[FAAS-Image-building...][{function_name}] {line}")

            # 3. Print Download Status (e.g., "Downloading", "Extracting")
            # Note: We skip the detailed progress bar strings because they clutter the logs in multi-threading
            elif 'status' in chunk:
                status = chunk['status']
                # Only print major status updates to avoid spamming the console
                if "Downloading" in status or "Extracting" in status:
                    # Optional: Print ID if available (e.g., layer hash)
                    layer_id = chunk.get('id', '')
                    print(f"[{function_name}] Docker Layer {layer_id}: {status}")

        print(f"[{function_name}] Build Complete.")
        return f"{function_name}:{version}"

    def _log_watcher_thread(self, container_name, faas_name, ready_signal):
        r = redis.Redis(host=self.redis_host, port=6379, db=0)

        print(f"[Log-Watcher] Suche in {container_name} nach: '{ready_signal}'")
        try:
            container = self.client.containers.get(container_name)
            log_stream = container.logs(stream=True, follow=True)

            for line in log_stream:
                if ready_signal in line.decode('utf-8'):
                    print(f"[Log-Watcher] Signal '{ready_signal}' gefunden!")

                    r.set(f"{container_name}:timer", 1, ex=30)

                    self.append_updated_faas_containers(faas_name)
                    break

        except Exception as e:
            print(f"[Log-Watcher] Fehler: {e}")

    def start_faas_container(self, faas_name: str, router_container_name: str,  **run_kwargs):
        uuid_for_faas = uuid.uuid4()
        container_name = f"FAAS-{uuid_for_faas}-container"

        READY_SIGNAL = "running on"

        payload = {
            "CONTAINER_NAME": container_name,
            "REDIS_HOST": self.redis_host
        }

        print(f"🚀 Starte FaaS-Container: {container_name}")
        try:
            image_tag = self.get_image_tags(faas_name)[0]
            faas_container = self.client.containers.run(
                image_tag,
                name=container_name,
                detach=True,
                environment=payload,
                **run_kwargs
            )
        except Exception as e:
            print(f"❌ Start-Fehler: {e}")
            return None

        watcher_thread = threading.Thread(
            target=self._log_watcher_thread,
            args=(container_name, faas_name, READY_SIGNAL),
            daemon=True
        )
        watcher_thread.start()

        try:
            faas_network = self.get_router_network(router_container_name)
            faas_network.connect(faas_container)
        except Exception as e:
            print(f"❌ Netzwerk-Fehler: {e}")

        if faas_name in self.active_containers_dict:
            self.active_containers_dict[faas_name].append(f"{uuid_for_faas}")
        else:
            self.active_containers_dict[faas_name] = [f"{uuid_for_faas}"]

        return faas_container


    def change_health_of_faas_in_cluster(self, faas_name: str, status: str):
        test = {
            "faas-id": faas_name,
            "status": status
        }
        url = f"http://{self.ENVOY_XDS_ADDRESS}/changeHealth"
        try:
            response = requests.put(url, json=test)
            response.raise_for_status()
            server_reply = response.json()
            print("Server received:", server_reply)
        except Exception as e:
            print(f"Request failed: {e}")


    def append_updated_faas_containers(self, faas_name: str):
        test = {
            "name": "hello",
            "base-routes": [
            ],
            "fallback-routes": [
            ]
        }
        url = f"http://{self.ENVOY_XDS_ADDRESS}/cluster"
        try:
            response = requests.put(url, json=self.build_faas_cluster_append_payload(faas_name))
            response.raise_for_status()
            server_reply = response.json()
            print("Server received:", server_reply)
        except Exception as e:
            print(f"Request failed: {e}")

    def build_faas_cluster_append_payload(self, faas_cluster_name: str, priority_0_containers=None,
                                          priority_1_containers=None) -> dict:
        if priority_0_containers is None:
            priority_0_containers = self.active_containers_dict[faas_cluster_name]
        if priority_1_containers is None:
            priority_1_containers = []

        prio_0_dict = [
            {"address": f"FAAS-{addr}-container", "port": int(5000)}
            for addr in priority_0_containers
        ]
        prio_1_dict = [
            #{"address": "envoy", "port": 10000}
        ]

        return {
            "name": faas_cluster_name,
            "base-routes": prio_0_dict,
            "fallback-routes": prio_1_dict,
            "replace": True
        }

    def get_router_network(self, router_id: str):
        network = self.client.networks.get(
            "msadockerizedfaas_faas-net"
        )
        return network

    def get_image_tags(self, repo_name: str):
        found_tags = []
        all_images = self.client.images.list()
        for img in all_images:
            for tag in img.tags:
                if tag.split(":")[0] == repo_name:
                    found_tags.append(tag)

        return found_tags

    def stop_and_remove_container(self, container_id: str):
        self.change_health_of_faas_in_cluster(container_id, "draining")

        uuid_only = container_id.split("FAAS-")[1].split('-container')[0]

        target_cluster = None

        for cluster_name, container_list in self.active_containers_dict.items():
            if uuid_only in container_list:
                target_cluster = cluster_name
                container_list.remove(uuid_only)
                print(f"✅ {uuid_only} aus Cluster '{cluster_name}' entfernt.")
                break

        print("Derzeit aktive Cluster und Container: ", self.active_containers_dict)

        self.append_updated_faas_containers(target_cluster)

        try:
            container = self.client.containers.get(container_id)
            print(f"Stopping and removing container {container_id}...")

            container.remove(force=True)

        except docker.errors.NotFound:
            print(f"Container {container_id} not found, nothing to do.")
        except docker.errors.APIError as e:
            print(f"Docker API error while removing container: {e}")

    def clear_deprecated_images(self, deprecated_images: list):
        for image_name in deprecated_images:
            print(f"Clear deprecated Image: {image_name}")
            image_name = image_name.split(":")
            try:
                self.client.images.remove(image=image_name)
                print(f"Deleted image: {image_name}")
            except Exception as e:
                print(f"Could not delete {image_name}: {e}")

    def get_container_port(self, container_id: str) -> dict:
            container = self.client.containers.get(container_id)
            container.reload()
            ports = container.attrs["NetworkSettings"]["Ports"]
            binding = ports.get("8080/tcp")
            if not binding:
                return None

            return binding[0]["HostPort"]

    def push_all_faas_clusters_in_envoy(self):
        url = f"http://{self.ENVOY_XDS_ADDRESS}/cluster"

        pushing_list_base = self.CONFIGURATION_FAAS_YAML["usable-functions"].items()
        pushing_list = []
        try:
            response = requests.get(url)
            response.raise_for_status()
            server_reply = response.json()
            print("Server received:", server_reply)

            clusters_data = server_reply.get("clusters", [])
            existing_names = {c["name"] for c in clusters_data}

            for key, config in pushing_list_base:
                target_name = config.get("name", key)

                if target_name not in existing_names:
                    pushing_list.append(target_name)

            print("Remaining items to push:", pushing_list)
        except Exception as e:
            print(f"Request failed: {e}")


        for func_name in pushing_list:
            payload = {
                "name": func_name,
                "dns-type": "strict",
                "http-protocol": "http1",
                "base-routes": [
                ],
                "fallback-routes": [
                ]
            }
            try:
                response = requests.post(url, json=payload)
                response.raise_for_status()
                server_reply = response.json()
                print("Server received:", server_reply)
            except Exception as e:
                print(f"Request failed: {e}")

    def push_listener_in_envoy(self):
        url = f"http://{self.ENVOY_XDS_ADDRESS}/listener"

        pushing_list_base = list(self.CONFIGURATION_FAAS_YAML["usable-functions"].items())
        pushing_list = []

        try:
            response = requests.get(url)
            response.raise_for_status()
            server_reply = response.json()
            print("Server received:", server_reply)

            listeners_dict = server_reply.get("listeners", {})

            existing_names = set()
            for func_list in listeners_dict.values():
                existing_names.update(func_list)

            print("Already executing on Envoy:", existing_names)

            for key, config in pushing_list_base:
                target_name = config.get("name", key)
                if target_name not in existing_names:
                    pushing_list.append((key, config))

            print("Remaining items to push:", pushing_list)

        except Exception as e:
            print(f"Request failed: {e}")

        routes = []
        for func_id, func_data in pushing_list:
            func_name = func_data.get("name") or func_id
            routes.append(self.create_route_for_listener(f"/{func_name}", func_name, "local_service"))
            print(self.create_listener_payload(routes))
        try:
            response = requests.post(url, json=self.create_listener_payload(routes))
            response.raise_for_status()
            server_reply = response.json()
            print("Server received:", server_reply)
        except Exception as e:
            print(f"Request failed: {e}")



#"""
#{
#  "count": 1,
#  "listeners": {
#    "listener_0": [
#      "hello"
#    ]
#  }
#}
#"""



    @staticmethod
    def create_listener_payload(routes):
        return {
            "routes": routes,
            "virtualHosts": [
                {
                    "name": "local_service",
                    "domains": ["*"]
                }
            ],
            "listenerConfiguration": {
                "listener_name": "listener_0",
                "listener_address": "0.0.0.0",
                "listener_port": 10000
            }
        }

    @staticmethod
    def create_route_for_listener(prefix, cluster_to_use, virtual_host, regex_rewrite=True):
        route = {
            "prefix": prefix,
            "cluster_to_use": cluster_to_use,
            "virtual_host": virtual_host,
        }
        if regex_rewrite:
            route["regex_rewrite"] = {
                "regex": ".*",
                "substitution": "/"
            }

        return route

"""
        POST
        http: // localhost: 8081 / listener
        Content - Type: application / json

        {
            "routes": [
                {
                    "prefix": "/hello",
                    "cluster_to_use": "hello",
                    "virtual_host": "local_service",
                    "regex_rewrite": {
                        "regex": ".*",
                        "substitution": "/"
                    }
                }
            ],
            "virtualHosts": [
                {
                    "name": "local_service",
                    "domains": ["*"]
                }
            ],
            "listenerConfiguration": {
                "listener_name": "listener_0",
                "listener_address": "0.0.0.0",
                "listener_port": 10000
            }
        }
"""