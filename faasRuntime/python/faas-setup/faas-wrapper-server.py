import importlib.util
import os
from starlette.applications import Starlette
from starlette.responses import JSONResponse
from starlette.requests import Request
from starlette.routing import Route

def load_handler_data():
    HANDLER_DIR = os.environ['HANDLER_DIR']
    HANDLER_NAME = os.environ['HANDLER_NAME']
    spec = importlib.util.spec_from_file_location(
        HANDLER_NAME, os.path.join(HANDLER_DIR, f"{HANDLER_NAME}")
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return {
        "handler": module.handle,
        "config": module.handler_run_config # This expects a dict variable
    }

BASE_HANDLER_FUNCTION = load_handler_data()

async def invoke_function(request: Request):
    try:
        config = BASE_HANDLER_FUNCTION['config']
        result = await BASE_HANDLER_FUNCTION['handler'](request)

        # If the handler says it returns a pre-made Response object
        if config.get('is_return_serialized', False):
            return result
        else:
            # Otherwise, wrap the raw data in JSON
            return JSONResponse(result)

    except Exception as e:
        print(f"Error: {e}") # Helpful to print the actual error to logs
        # FIX: Use a dictionary or list, NOT a set {"String"}
        return JSONResponse(
            {"error": "Internal error while executing function, try again later..."},
            status_code=500
        )

routes = [
    Route("/", endpoint=invoke_function, methods=["GET", "POST"]), # Added methods to be safe
]

app = Starlette(routes=routes)