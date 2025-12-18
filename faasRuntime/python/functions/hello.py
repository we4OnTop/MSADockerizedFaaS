def get_constants() -> dict:
    return {
        "request-body-type": "JSON",
        "method": "POST",
        "return-body-type": "JSON"
    }

#TODO: INCLUDE CONFIGURATION FOR WRAPPER SOMETHING LIKE ABOVE /\

async def handle(request):
    data = await request.json() if request.method == "POST" else {}
    return {"message": "Hello World", "input": data}