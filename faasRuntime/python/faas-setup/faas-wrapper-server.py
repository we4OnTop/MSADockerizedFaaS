import importlib.util
import os
from contextlib import asynccontextmanager
import redis.asyncio as redis

from starlette.applications import Starlette
from starlette.responses import JSONResponse
from starlette.requests import Request
from starlette.routing import Route

redis_client = None
CONTAINER_NAME = None

@asynccontextmanager
async def lifespan(app):
    global redis_client
    global CONTAINER_NAME
    CONTAINER_NAME = os.environ["CONTAINER_NAME"]
    redis_host = os.environ.get("REDIS_HOST", "localhost")
    redis_client = redis.Redis(host=redis_host, port=6379, db=0, decode_responses=True)
    print("Redis Pool Connected")

    yield

    await redis_client.aclose()
    print("Redis Pool Closed")

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
        "config": module.handler_run_config
    }

BASE_HANDLER_FUNCTION = load_handler_data()

async def invoke_function(request: Request):
    try:
        config = BASE_HANDLER_FUNCTION['config']
        result = await BASE_HANDLER_FUNCTION['handler'](request)

        if config.get('is_return_serialized', False):
            return result
        else:
            return JSONResponse(result)

    except Exception as e:
        print(f"Error: {e}")
        return JSONResponse(
            {"error": "Internal error while executing function, try again later..."},
            status_code=500
        )

async def endpoint_wrapper(request: Request):
    try:
        faas_name = os.environ.get("CONTAINER_NAME", "unknown-func")
        print(faas_name)
        async with redis_client.pipeline() as pipe:
            await pipe.delete(f"{faas_name}:timer")
            await pipe.incr(f"{faas_name}:{faas_name}")
            await pipe.execute()

    except Exception as e:
        print(f"Redis Error: {e}")

    response = await invoke_function(request)

    return response

routes = [
    Route("/", endpoint=endpoint_wrapper, methods=["GET", "POST"]),
]

app = Starlette(routes=routes, lifespan=lifespan)