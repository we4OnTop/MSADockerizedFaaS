from starlette.responses import HTMLResponse
from starlette.responses import JSONResponse
handler_run_config = {
    "is_return_serialized": True
}

async def handle(request):
    try:
        data = await request.json() if request.method == "POST" else {}

        val1 = int(data.get("value1", 0))
        val2 = int(data.get("value2", 0))
        ergebnis = val1 * val2

        return JSONResponse({"result": ergebnis})

    except Exception as e:
        return JSONResponse({"error": str(e)}, status_code=400)