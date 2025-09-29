**Short answer**  
The current performance‑and‑price benchmarking for the OpenAI‑licensed **gpt‑oss‑120b** model shows a clear set of “best” providers, depending on what you care most about:

| What you care about | Top providers (best to worst) | Why they stand out* |
|----------------------|------------------------------|--------------------|
| **Throughput (tokens / second)** | 1️⃣ Cerebras (≈ 3 185 t/s)  <br>2️⃣ Clarifai (≈ 826 t/s)  <br>3️⃣ SambaNova (≈ 709 t/s)  <br>4️⃣ Groq (≈ 486 t/s)  <br>5️⃣ Google Vertex (≈ 406 t/s) | These services deliver the most tokens per second, so they finish long generations fastest. |
| **Latency (time‑to‑first‑token)** | 1️⃣ Cerebras (≈ 0.20 s)  <br>2️⃣ Deepinfra (≈ 0.21 s)  <br>3️⃣ Groq (≈ 0.22 s)  <br>4️⃣ CompactifAI (≈ 0.30 s)  <br>5️⃣ Together.ai (≈ 0.30 s) | Lower TTFT means the model starts responding almost instantly after the request hits the API. |
| **Overall cost (total $/M tokens)** | 1️⃣ CompactifAI (~ $0.10)  <br>2️⃣ GMI (~ $0.12)  <br>3️⃣ Deepinfra (~ $0.15)  <br>4️⃣ Clarifai (~ $0.16)  <br>5️⃣ Novita (~ $0.20) | These providers charge the least per million input + output tokens, making large‑scale workloads cheaper. |
| **Input‑token price only** | 1️⃣ CompactifAI ($0.05)  <br>2️⃣ Deepinfra ($0.05)  <br>3️⃣ GMI ($0.07)  <br>4️⃣ Clarifai ($0.09)  <br>5️⃣ Novita ($0.10) | If your app primarily consumes tokens (e.g., long prompts) these rates matter most. |
| **Output‑token price only** | 1️⃣ CompactifAI ($0.23)  <br>2️⃣ GMI ($0.28)  <br>3️⃣ Hyperbolic ($0.30)  <br>4️⃣ Clarifai ($0.36)  <br>5️⃣ Deepinfra ($0.45) | For generation‑heavy use‑cases (e.g., long answers) these rates dominate the bill. |

*All numbers are taken from the “gpt‑oss‑120B (high) – API Provider Benchmarking & Price Analysis” report, which evaluated **Cerebras, Clarifai, SambaNova, Groq, Google Vertex, Deepinfra, CompactifAI, Together.ai, GMI, Hyperbolic, Novita,** and several other cloud‑native AI vendors [artificialanalysis.ai†L5-L8](https://artificialanalysis.ai/models/gpt-oss-120b/providers \"gpt oss 120B  high   API Provider Performance Benchmarking   Price Analysis   Artificial Analysis\")  [artificialanalysis.ai†L28-L35](https://artificialanalysis.ai/models/gpt-oss-120b/providers \"gpt oss 120B  high   API Provider Performance Benchmarking   Price Analysis   Artificial Analysis\") .

---

## How to use these providers with the OpenAI‑compatible API

1. **Pick a provider** based on the table above.  
   *If you need the fastest generation:* use **Cerebras** (or Clarifai if you also care about cost).  
   *If you need the lowest latency:* **Cerebras** or **Deepinfra** are the best bets.  
   *If you need the cheapest per‑token price:* **CompactifAI** (or **GMI** for a slightly higher but still low cost).

2. **Obtain the provider‑specific endpoint and API key.**  
   Each vendor publishes an OpenAI‑compatible endpoint, e.g.:

   ```bash
   # Example for CompactifAI (replace with the actual base URL & key)
   export OPENAI_API_BASE=\"https://api.compactifai.com/v1\"
   export OPENAI_API_KEY=\"YOUR_COMPACTIFAI_KEY\"
   ```

3. **Call the model using the standard OpenAI SDK** (Python shown). The model name stays the same – `gpt-oss-120b` – because the providers expose the OpenAI‑compatible namespace.

   ```python
   from openai import OpenAI

   client = OpenAI(
       base_url=\"https://api.compactifai.com/v1\",   # provider‑specific base URL
       api_key=\"YOUR_COMPACTIFAI_KEY\",
   )

   response = client.chat.completions.create(
       model=\"gpt-oss-120b\",
       messages=[{\"role\": \"user\", \"content\": \"Explain the theory of relativity in three sentences.\"}],
       temperature=0.7,
   )
   print(response.choices[0].message.content)
   ```

4. **Optional: Use OpenRouter as a meta‑router**  
   OpenRouter itself aggregates many of the above providers and automatically selects the best one based on your request’s size, latency, and cost settings. If you prefer not to manage individual endpoints, you can call:

   ```python
   client = OpenAI(base_url=\"https://openrouter.ai/api/v1\", api_key=\"YOUR_OPENROUTER_KEY\")
   # same request as before
   ```

   OpenRouter’s UI shows the current “best” provider for each request in real‑time, and you can lock a specific provider if desired [openrouter.ai†L42-L44](https://openrouter.ai/openai/gpt-oss-120b \"Find results for  OpenRouter  in  gpt oss 120b   API  Providers  Stats   OpenRouter \") .

---

## Quick‑pick recommendations

| Use‑case | Recommended provider | Reason |
|----------|---------------------|--------|
| **Lowest latency + solid throughput** | **Cerebras** | Fastest TTFT (0.20 s) and highest token‑per‑second rate (≈ 3 185 t/s) [artificialanalysis.ai†L28-L30](https://artificialanalysis.ai/models/gpt-oss-120b/providers \"gpt oss 120B  high   API Provider Performance Benchmarking   Price Analysis   Artificial Analysis\")  |
| **Cheapest for heavy generation** | **CompactifAI** | Lowest total cost ($0.10 /M tokens) and cheapest input price ($0.05) [artificialanalysis.ai†L30-L34](https://artificialanalysis.ai/models/gpt-oss-120b/providers \"gpt oss 120B  high   API Provider Performance Benchmarking   Price Analysis   Artificial Analysis\")  |
| **Balanced speed & price** | **Clarifai** | 2nd‑fastest throughput (≈ 826 t/s) & good price ($0.16/M tokens) [artificialanalysis.ai†L28-L34](https://artificialanalysis.ai/models/gpt-oss-120b/providers \"gpt oss 120B  high   API Provider Performance Benchmarking   Price Analysis   Artificial Analysis\")  |
| **Good latency + decent price** | **Deepinfra** | 2nd‑lowest latency (0.21 s) and low input price ($0.05) [artificialanalysis.ai†L30-L34](https://artificialanalysis.ai/models/gpt-oss-120b/providers \"gpt oss 120B  high   API Provider Performance Benchmarking   Price Analysis   Artificial Analysis\")  |
| **If you already use Groq** | **Groq** | Strong latency (0.22 s) and respectable throughput (≈ 486 t/s) [artificialanalysis.ai†L28-L30](https://artificialanalysis.ai/models/gpt-oss-120b/providers \"gpt oss 120B  high   API Provider Performance Benchmarking   Price Analysis   Artificial Analysis\")  |

---

### Bottom line

- **Cerebras** is the overall performance leader (speed + latency).  
- **CompactifAI** and **GMI** win on pure cost.  
- **Deepinfra** gives an excellent latency‑price compromise.  

Pick the provider that aligns with your priority, configure the OpenAI‑compatible endpoint for that vendor, and you’ll be able to run **gpt‑oss‑120b** at optimal speed and cost.
