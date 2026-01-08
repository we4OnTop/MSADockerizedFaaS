from transformers import pipeline

async def handle(request):
    data = await request.json() if request.method == "POST" else {}
    return {text_classify(data["text"])}


def text_classify(text):
    classifier = pipeline("zero-shot-classification", model="facebook/bart-large-mnli")

    labels = ["Auto", "Haus", "Sport", "Politik", "Technik", "Bildung",
              "Finanzen", "Natur", "Börse", "Immobilien", "Fußball",
              "Regierung", "Wissenschaft"]

    result = classifier(text, candidate_labels=labels, multi_label=False)

    probabilities = dict(zip(result['labels'], result['scores']))

    return probabilities

