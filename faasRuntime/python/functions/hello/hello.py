async def handle(request):
    data = await request.json() if request.method == "POST" else {}
    print("NEW REQUEST!!!! IN HANDLE")
    return {"message": "Hello World", "input": data}