from starlette.responses import HTMLResponse, Response

handler_run_config = {
    "is_return_serialized": True
}

async def handle(request):
    try:
        data = await request.json() if request.method == "POST" else {}
        html_content = """
<!DOCTYPE html>
<html lang="de">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Orchestration Functions</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <style>
        .glass {
            background: rgba(30, 41, 59, 0.7);
            backdrop-filter: blur(10px);
            border: 1px solid rgba(255,255,255,0.1);
            transition: all 0.2s;
        }
        .glass:hover { transform: translateY(-2px); border-color: rgba(255,255,255,0.2); }
        .endpoint-tag {
            font-family: monospace;
            background: #1e293b;
            color: #38bdf8;
            padding: 2px 6px;
            border-radius: 4px;
            font-size: 0.7rem;
        }
    </style>
</head>
<body class="bg-[#0b0f1a] text-slate-200 min-h-screen p-4 md:p-8">
<div class="max-w-7xl mx-auto">
    <header class="mb-10">
        <h1 class="text-4xl font-black text-white flex items-center">
            <span class="text-blue-500 mr-3">🚀</span> Orchestration Functions
        </h1>
    </header>

    <h3 class="text-slate-500 uppercase text-xs font-bold mb-4 tracking-widest">Mathematical Services</h3>
    <div class="grid grid-cols-1 md:grid-cols-4 gap-6 mb-12">
        
        <div class="glass p-5 rounded-2xl border-l-4 border-l-yellow-500">
            <div class="flex justify-between mb-3"><h2 class="font-bold">Addition</h2><span class="endpoint-tag">/addition</span></div>
            <div class="flex gap-2 mb-3">
                <input id="add-v1" type="number" class="w-full bg-slate-900 border border-slate-700 rounded p-2 text-sm" placeholder="A">
                <input id="add-v2" type="number" class="w-full bg-slate-900 border border-slate-700 rounded p-2 text-sm" placeholder="B">
            </div>
            <button onclick="callMath('addition', 'add-v1', 'add-v2', 'out-add')" class="w-full bg-yellow-600 py-2 rounded-lg font-bold hover:bg-yellow-500 transition">Addieren</button>
            <div id="out-add" class="mt-3 text-center text-yellow-400 font-bold"></div>
        </div>

        <div class="glass p-5 rounded-2xl border-l-4 border-l-orange-500">
            <div class="flex justify-between mb-3"><h2 class="font-bold">Subtract</h2><span class="endpoint-tag">/subtract</span></div>
            <div class="flex gap-2 mb-3">
                <input id="sub-v1" type="number" class="w-full bg-slate-900 border border-slate-700 rounded p-2 text-sm" placeholder="A">
                <input id="sub-v2" type="number" class="w-full bg-slate-900 border border-slate-700 rounded p-2 text-sm" placeholder="B">
            </div>
            <button onclick="callMath('subtract', 'sub-v1', 'sub-v2', 'out-sub')" class="w-full bg-orange-600 py-2 rounded-lg font-bold hover:bg-orange-500 transition">Subtrahieren</button>
            <div id="out-sub" class="mt-3 text-center text-orange-400 font-bold"></div>
        </div>

        <div class="glass p-5 rounded-2xl border-l-4 border-l-red-500">
            <div class="flex justify-between mb-3"><h2 class="font-bold">Multiply</h2><span class="endpoint-tag">/multiply</span></div>
            <div class="flex gap-2 mb-3">
                <input id="mul-v1" type="number" class="w-full bg-slate-900 border border-slate-700 rounded p-2 text-sm" placeholder="A">
                <input id="mul-v2" type="number" class="w-full bg-slate-900 border border-slate-700 rounded p-2 text-sm" placeholder="B">
            </div>
            <button onclick="callMath('multiply', 'mul-v1', 'mul-v2', 'out-mul')" class="w-full bg-red-600 py-2 rounded-lg font-bold hover:bg-red-500 transition">Multiplizieren</button>
            <div id="out-mul" class="mt-3 text-center text-red-400 font-bold"></div>
        </div>

        <div class="glass p-5 rounded-2xl border-l-4 border-l-pink-500">
            <div class="flex justify-between mb-3"><h2 class="font-bold">Division</h2><span class="endpoint-tag">/devision</span></div>
            <div class="flex gap-2 mb-3">
                <input id="div-v1" type="number" class="w-full bg-slate-900 border border-slate-700 rounded p-2 text-sm" placeholder="A">
                <input id="div-v2" type="number" class="w-full bg-slate-900 border border-slate-700 rounded p-2 text-sm" placeholder="B">
            </div>
            <button onclick="callMath('devision', 'div-v1', 'div-v2', 'out-div')" class="w-full bg-pink-600 py-2 rounded-lg font-bold hover:bg-pink-500 transition">Dividieren</button>
            <div id="out-div" class="mt-3 text-center text-pink-400 font-bold"></div>
        </div>
    </div>

    <h3 class="text-slate-500 uppercase text-xs font-bold mb-4 tracking-widest">AI & Language Services</h3>
    <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
        
        <div class="glass p-6 rounded-2xl border-t-2 border-t-blue-500">
            <div class="flex justify-between mb-4"><h2 class="font-bold text-blue-400">Summarization</h2><span class="endpoint-tag">/summary</span></div>
            <textarea id="in-summary" class="w-full bg-slate-900 border border-slate-700 rounded-lg p-2 text-sm mb-3 focus:border-blue-500 outline-none" rows="3" placeholder="Text zum Kürzen..."></textarea>
            <button onclick="callApi('summary', 'in-summary', 'out-summary')" class="w-full bg-blue-600 py-2 rounded-lg font-bold hover:bg-blue-500 transition">Zusammenfassen</button>
            <div id="out-summary" class="mt-4 p-3 bg-slate-900/50 rounded text-sm min-h-[40px] text-slate-300"></div>
        </div>

        <div class="glass p-6 rounded-2xl border-t-2 border-t-emerald-500">
            <div class="flex justify-between mb-4"><h2 class="font-bold text-emerald-400">Text Generator</h2><span class="endpoint-tag">/text-generator</span></div>
            <input id="in-gen" type="text" class="w-full bg-slate-900 border border-slate-700 rounded-lg p-2 text-sm mb-3 focus:border-emerald-500 outline-none" placeholder="Anfang des Satzes...">
            <button onclick="callApi('text-generator', 'in-gen', 'out-gen')" class="w-full bg-emerald-600 py-2 rounded-lg font-bold hover:bg-emerald-500 transition">Generieren</button>
            <div id="out-gen" class="mt-4 p-3 bg-slate-900/50 rounded text-sm text-emerald-200 italic"></div>
        </div>

        <div class="glass p-6 rounded-2xl border-t-2 border-t-purple-500">
            <div class="flex justify-between mb-4"><h2 class="font-bold text-purple-400">Topic Classifier</h2><span class="endpoint-tag">/text-classifier</span></div>
            <input id="in-class" type="text" class="w-full bg-slate-900 border border-slate-700 rounded-lg p-2 text-sm mb-3 focus:border-purple-500 outline-none" placeholder="Thema analysieren...">
            <button onclick="callApi('text-classifier', 'in-class', 'out-class')" class="w-full bg-purple-600 py-2 rounded-lg font-bold hover:bg-purple-500 transition">Klassifizieren</button>
            <pre id="out-class" class="mt-4 p-3 bg-slate-900/50 rounded text-[10px] text-purple-300 overflow-auto max-h-24"></pre>
        </div>

        <div class="glass p-6 rounded-2xl border-t-2 border-t-teal-500">
            <div class="flex justify-between mb-4"><h2 class="font-bold text-teal-400">Text to Speech</h2><span class="endpoint-tag">/t2s</span></div>
            <input id="in-t2s" type="text" class="w-full bg-slate-900 border border-slate-700 rounded-lg p-2 text-sm mb-3 focus:border-teal-500 outline-none" placeholder="Vorzulesender Text...">
            <button onclick="callApi('t2s', 'in-t2s', 'out-t2s')" class="w-full bg-teal-600 py-2 rounded-lg font-bold hover:bg-teal-500 transition">Sprechen</button>
            <div id="out-t2s" class="mt-4"></div>
        </div>

        <div class="glass p-6 rounded-2xl border-t-2 border-t-indigo-500">
            <div class="flex justify-between mb-4"><h2 class="font-bold text-indigo-400">Image Classifier</h2><span class="endpoint-tag">/image-classifier</span></div>
            <input type="file" id="in-img" class="text-[10px] mb-3 block w-full text-slate-400">
            <button onclick="uploadImg()" class="w-full bg-indigo-600 py-2 rounded-lg font-bold hover:bg-indigo-500 transition">Analysieren</button>
            <pre id="out-img" class="mt-4 p-2 bg-slate-900/50 rounded text-[10px] text-indigo-300 max-h-24 overflow-auto"></pre>
        </div>
    </div>
</div>

<script>
    const BASE_URL = 'http://localhost:8080';

    async function callApi(endpoint, inputId, outId) {
        const out = document.getElementById(outId);
        const input = document.getElementById(inputId);
        if (!input.value) return out.innerText = "⚠️ Input fehlt";
        
        out.innerText = "⏳...";
        try {
            const res = await fetch(`${BASE_URL}/${endpoint}`, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({ text: input.value })
            });
            const data = await res.json();
            let display = data.summary || data.classification || data.generated_text || data.result || data.message || data;
            out.innerText = typeof display === 'object' ? JSON.stringify(display, null, 2) : display;
            
            if(endpoint === 't2s' && data.audio) {
                out.innerHTML = `<audio controls class="w-full mt-2 h-8"><source src="data:audio/wav;base64,${data.audio}" type="audio/wav"></audio>`;
            }
        } catch (e) { out.innerText = "❌ Error: " + e.message; }
    }

    async function callMath(endpoint, id1, id2, outId) {
        const v1 = document.getElementById(id1).value;
        const v2 = document.getElementById(id2).value;
        const out = document.getElementById(outId);
        if (v1 === "" || v2 === "") return out.innerText = "⚠️ Werte!";
        
        out.innerText = "🔢...";
        try {
            const res = await fetch(`${BASE_URL}/${endpoint}`, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({ value1: v1, value2: v2 })
            });
            const data = await res.json();
            out.innerText = data.result !== undefined ? `Resultat: ${data.result}` : "Fehler";
        } catch (e) { out.innerText = "❌ Fehler"; }
    }

    function uploadImg() {
        const file = document.getElementById('in-img').files[0];
        const out = document.getElementById('out-img');
        if (!file) return out.innerText = "⚠️ Bild wählen";

        const reader = new FileReader();
        reader.onloadend = async () => {
            out.innerText = "🔍 Analysiere...";
            try {
                const res = await fetch(`${BASE_URL}/image-classifier`, {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({ image: reader.result })
                });
                const data = await res.json();
                out.innerText = JSON.stringify(data.predictions || data, null, 2);
            } catch (e) { out.innerText = "❌ " + e.message; }
        };
        reader.readAsDataURL(file);
    }
</script>
</body>
</html>

        """
        return HTMLResponse(content=html_content)

    except Exception as e:
        return HTMLResponse({"error": str(e)}, status_code=400)