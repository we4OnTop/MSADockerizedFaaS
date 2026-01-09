from starlette.responses import HTMLResponse

handler_run_config = {
    "is_return_serialized": True
}

async def handle(request):
    data = await request.json() if request.method == "POST" else {}

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