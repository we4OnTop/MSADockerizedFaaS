from transformers import pipeline

async def handle(request):
    data = await request.json() if request.method == "POST" else {}
    return {text_generation(data["text"])}


def text_generation(prompt, max_length=100, num_return_sequences=1):
    generator = pipeline("text-generation", model="dbmdz/german-gpt2")

    print(f"Generiere Text basierend auf: '{prompt}'")
    generated_texts = generator(
        prompt,
        max_length=max_length,
        num_return_sequences=num_return_sequences,
        do_sample=True,
        temperature=0.7,
        top_k=50,
        top_p=0.95,
        no_repeat_ngram_size=2
    )

    return [g['generated_text'] for g in generated_texts]

