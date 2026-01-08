import importlib.util
import os
from starlette.applications import Starlette
from starlette.responses import JSONResponse
from starlette.requests import Request
from starlette.routing import Route

def load_handler():
    print("NEW REQUEST!!!!")
    HANDLER_DIR = os.environ['HANDLER_DIR']
    HANDLER_NAME = os.environ['HANDLER_NAME']
    spec = importlib.util.spec_from_file_location(
        HANDLER_NAME, os.path.join(HANDLER_DIR, f"{HANDLER_NAME}")
    )
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module.handle


BASE_HANDLER_FUNCTION = load_handler()

async def invoke_function(request: Request):
    try:
        result = await BASE_HANDLER_FUNCTION(request)
        return JSONResponse(result)
    except Exception as e:
        return JSONResponse({"Internal error while executing function, try again later..."}, status_code=500)

routes = [
    Route("/", endpoint=invoke_function),
]

app = Starlette(routes=routes)
