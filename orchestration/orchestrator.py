from flask import Flask, request, jsonify
from orchestration import orchestrationManager
from orchestrationManager import OrchestrationManager

class Orchestrator:
    def __init__(self):
        self.app = Flask(__name__)
        self.orchestrationManager = OrchestrationManager()
        self.register_routes()

    def run(self):
        self.app.run(host="0.0.0.0", port=5000, debug=True)

    def register_routes(self):
        @self.app.route("/containers/<function_name>", methods=["POST"])
        def create_container(function_name):
            data = request.get_json()
            image_name = data.get("imageName")
            # TODO: Create Container based on function 
            container_id = self.orchestrationManager.start_container(image_name)



            container_port = orchestrationManager.get_container_port(container_id=container_id)
            
            
            return jsonify({"id": container_id}), 201

        @self.app.route("/containers/<image_name>", methods=["DELETE"])
        def delete_container(image_name):
            self.orchestrationManager.stop_and_remove_container(image_name)
            return "", 204

if __name__ == "__main__":
    Orchestrator().run()
