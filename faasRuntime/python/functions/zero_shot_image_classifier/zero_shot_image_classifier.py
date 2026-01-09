import base64
import io
from PIL import Image
from transformers import pipeline

handler_run_config = {
    "is_return_serialized": False
}

async def handle(request):
    data = await request.json() if request.method == "POST" else {}
    return {image_classifier(data["image"])}


def image_classifier(base64_string):
    labels = ["Katze", "Hund", "Baum", "Auto", "Sonne", "Gebäude", "Flugzeug", "Blume", "Person"]
    classifier = pipeline("zero-shot-image-classification", model="openai/clip-vit-base-patch32")
    try:
        if "," in base64_string:
            base64_string = base64_string.split(",")[1]

        img_data = base64.b64decode(base64_string)

        image = Image.open(io.BytesIO(img_data))

        if image.mode != "RGB":
            image = image.convert("RGB")

    except Exception as e:
        return {"error": f"Ungültiges Bildformat oder Base64-String: {str(e)}"}

    results = classifier(image, candidate_labels=labels)

    results.sort(key=lambda x: x['score'], reverse=True)

    return results