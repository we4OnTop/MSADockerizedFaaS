import asyncio
import time

from starlette.responses import HTMLResponse

handler_run_config = {
    "is_return_serialized": True
}

async def handle(request):
    data = await request.json() if request.method == "POST" else {}
    print("NEW REQUEST!!!! IN HANDLE")
    #--- START 20 SECOND LOOP ---
    start_time = time.time()

    # Loop while the difference between now and start is less than 20
    while time.time() - start_time < 20:
        print("Working...")

        # IMPORTANT: Use await asyncio.sleep to keep the server responsive
        # This pauses THIS request, but lets the server handle OTHERS.
        await asyncio.sleep(1)
        # --- END LOOP ---
    html_content = """
    <!DOCTYPE html>
    <html>
    <head>
        <title>Status</title>
        <style>body { font-family: sans-serif; padding: 2rem; }</style>
    </head>
    <body>
        <h1>Request Received!</h1>
        <p>Everything is working correctly.</p>
    </body>
    </html>
    """

    return HTMLResponse(content=html_content)