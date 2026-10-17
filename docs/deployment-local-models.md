# Local models for Audio Note Minutes and Search

2026-10-13. Audio Note Minutes on the home deployment use two local services on
**gpu-host** (`192.0.2.20`, 2× RTX 3090, Ryzen 7 3700X). Alexandria
(`192.0.2.10`) reaches both over the LAN.

| Service | Unit | Address | Model |
|---|---|---|---|
| Summary (existing) | `vllm-qwen38.service` | `http://192.0.2.20:8080` | `qwen3.8-27b` (OpenAI format, thinking off by default) |
| Transcription | `whisper.service` | `http://192.0.2.20:8090` | `whisper-large-v3-turbo` |
| Search embedding | `ollama-embed.service` | `http://192.0.2.20:11434` | `nomic-embed-text:v1.5` |

vLLM and Whisper require a bearer key; Ollama has none. It is open on the LAN by
the owner's choice (isolated home LAN), and Alexandria's embedding client sends
no key. vLLM's is in `/etc/vllm-qwen38/vllm-qwen38.env`, and
Whisper's is `WHISPER_API_KEY` in `/etc/whisper/whisper.env` (root, 0600). Enter
them only in Alexandria's Settings → Recognition → Audio Note Minutes, and never on a
command line.

## Whisper service

`deploy/whisper/` holds the server (`server.py`, about 80 lines of FastAPI over
faster-whisper), pinned `requirements.txt` and the systemd unit. It serves
`POST /v1/audio/transcriptions` (`json`, `verbose_json` with segments, or
`text`), `GET /v1/models` and an unauthenticated `GET /health`. It runs requests
one at a time with VAD filtering, and accepts uploads up to 64 MiB.

It ran on the **CPU** (int8, 8 threads) until 2026-10-17, so vLLM kept both GPUs untouched. The
KV cache only just fits one 262k-token request (1.16×), so taking GPU memory for
Whisper would have meant shrinking vLLM's context. Measured on 2026-10-13, a
185.4 s, 32 kbps BOOX recording took 57.9 s (0.31× real time). The model uses
about 2 GB of RAM.

Install or update:

```sh
scp deploy/whisper/{server.py,requirements.txt,whisper.service} ops@gpu-host:/opt/whisper/scratch/
ssh ops@gpu-host '
  python3 -m venv /opt/whisper/env   # first time only
  /opt/whisper/env/bin/pip install --only-binary=:all: -r /opt/whisper/scratch/requirements.txt
  cp /opt/whisper/scratch/server.py /opt/whisper/server.py
  sudo cp /opt/whisper/scratch/whisper.service /etc/systemd/system/
  sudo systemctl daemon-reload && sudo systemctl restart whisper'
```

Create `/etc/whisper/whisper.env` with a random `WHISPER_API_KEY=` before the
first start. `av` is pinned to 14.2.0: faster-whisper 1.2.x passes an argument
that newer PyAV releases removed.

Transcription moved to GPU0 on 2026-10-17; see GPU Layout below.

Rollback: `sudo systemctl disable --now whisper`. Nothing else on gpu-host was
changed.

## GPU Layout (2026-10-17)

Before this, Whisper ran on the CPU and search embedding ran in the app VM's
bundled Ollama container on 2 vCPUs. The owner wants Alexandria to depend only
on API endpoints, not on models running on the app host's CPU.

| GPU (nvidia-smi index) | vLLM (TP=2) | Also |
|---|---|---|
| GPU0 `GPU-aaaaaaaa…` | 20.0 GB | Whisper, 1.2 GB |
| GPU1 `GPU-bbbbbbbb…` | 20.0 GB | Ollama embedding, 0.6 GB |

- **vLLM:** `--gpu-memory-utilization 0.92→0.83` and `--max-model-len
  262144→131072`.
  - Previous unit: `vllm-qwen38.service.bak.20261017`.
  - The restart took 4½ minutes, and it answered at 131,072 context
    afterwards.
- **Whisper:** `WHISPER_DEVICE=cuda`, `int8_float16`, plus NVIDIA's cuBLAS 12
  and cuDNN 9.10.2 wheels in the venv via `LD_LIBRARY_PATH`.
  - The package list before the change is in
    `/opt/whisper/pip-freeze.before-gpu-20261017.txt`; the previous unit is
    `whisper.service.bak.20261017`.
  - The model loads in 2.8 s, and a 20 s clip transcribes in 0.6 s, against
    0.31× real time on the CPU.
- **Ollama:** 0.40.3, the same version as the VM's bundled container.
  - The `ollama-linux-amd64.tar.zst` release asset was checked against the
    release's `sha256sum.txt` and installed under `/opt/ollama/0.40.3`.
  - Models are in `/opt/ollama/models`, and the unit is
    `deploy/ollama-embed/ollama-embed.service`.
  - The model ID matches the VM's (`0a109f422b47`). Embeddings of the same
    text agree with the VM's to cosine 1.000000.
  - Ollama 0.40 also enumerates GPUs through Vulkan, which ignores
    `CUDA_VISIBLE_DEVICES`, and picked GPU0. `OLLAMA_VULKAN=false` keeps it on
    CUDA.
- **Pin by UUID.** Both units pin their card by UUID, because CUDA's default
  device order need not match nvidia-smi's.
- **Switching Alexandria.** Settings → Search, embedding URL
  `http://192.0.2.20:11434`. Generations are keyed by endpoint and model,
  so the new URL starts a replacement index build. The current index serves
  search until the new one is ready.
- **Replacement index (2026-10-17 02:58 UTC).** The owner queued it after
  saving the gpu-host URL. It embedded all 1,633 records (1,646 vectors) in about
  70 s, roughly 1,400 records a minute, with no failures. The first build on
  the VM's CPU ran at about 12 records a minute (2026-10-14, on 2 vCPUs shared
  with the worker): about 120× faster. The VM-built index is retired.
- **Rollback.**
  - `sudo systemctl disable --now ollama-embed`
  - restore the two `.bak.20261017` units, then `daemon-reload` and restart
  - point Settings back at `http://ollama:11434`

## GPU power cap

2026-10-14: both 3090s are capped at **200 W** (stock 350 W) by
`gpu-power-cap.service` on gpu-host (`nvidia-smi -pm 1`, `nvidia-smi -pl 200`, runs
at boot). The owner lowered it from 250 W because the room ran hot. Earlier
versions of the unit are kept beside it as `.bak-250w` and `.bak-150w`. To change it, edit
the `-pl` value, then `systemctl daemon-reload` and
`systemctl restart gpu-power-cap.service`.
