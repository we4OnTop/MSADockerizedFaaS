from transformers import pipeline

handler_run_config = {
    "is_return_serialized": False
}

async def handle(request):
    data = await request.json() if request.method == "POST" else {}
    return {"summary": summarize_text_simple(data["text"])}

def summarize_text_simple(text):
    summarizer = pipeline("summarization", model="facebook/bart-large-cnn")

    input_length = len(text.split())

    calc_max = min(130, input_length)
    calc_min = min(30, max(5, input_length // 2))

    summary = summarizer(text, max_length=calc_max, min_length=calc_min, do_sample=False)

    return summary[0]['summary_text']