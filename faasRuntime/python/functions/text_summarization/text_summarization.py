from transformers import pipeline

async def handle(request):
        data = await request.json() if request.method == "POST" else {}
        return {summarize_text_simple(data["text"])}


def summarize_text_simple(text):
    summarizer = pipeline("summarization", model="facebook/bart-large-cnn")

    summary = summarizer(text, max_length=130, min_length=30, do_sample=False)

    return summary[0]['summary_text']