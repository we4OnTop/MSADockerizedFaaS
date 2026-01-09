from transformers import pipeline
import soundfile as sf

handler_run_config = {
    "is_return_serialized": False
}

async def handle(request):
    data = await request.json() if request.method == "POST" else {}
    return {text_to_speech_german(data["text"], output_filename="zusammenfassung.wav")}

def text_to_speech_german(text, output_filename="zusammenfassung.wav"):
    synthesizer = pipeline("text-to-speech", model="facebook/mms-tts-deu")

    speech = synthesizer(text)

    sf.write(output_filename, speech["audio"][0], speech["sampling_rate"])
    print(f"Erfolgreich gespeichert: {output_filename}")

