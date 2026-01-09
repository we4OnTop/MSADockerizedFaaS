from transformers import pipeline

handler_run_config = {
    "is_return_serialized": False
}

async def handle(request):
    try:
        data = await request.json() if request.method == "POST" else {}
        if "text" not in data:
            return {"error": "No text provided"}, 400

        classification_result = text_classify(data["text"])
        return {"classification": classification_result}

    except Exception as e:
        print(f"Error: {e}")
        return {"error": str(e)}, 500


def text_classify(text):
    classifier = pipeline("zero-shot-classification", model="facebook/bart-large-mnli")

    labels = ["Auto", "Haus", "Sport", "Politik", "Technik", "Bildung",
              "Finanzen", "Natur", "Börse", "Immobilien", "Fußball",
              "Regierung", "Wissenschaft"]

    result = classifier(text, candidate_labels=labels, multi_label=False)

    probabilities = dict(zip(result['labels'], result['scores']))

    return probabilities

