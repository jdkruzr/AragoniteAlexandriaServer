# Local models for Audio Note Minutes

2026-10-13. Audio Note Minutes on the home deployment use two local services on
**gpu-host** (`192.0.2.20`, 2× RTX 3090, Ryzen 7 3700X). Alexandria
(`192.0.2.10`) reaches both over the LAN.

| Service | Unit | Address | Model |
|---|---|---|---|
| Summary (existing) | `vllm-qwen38.service` | `http://192.0.2.20:8080` | `qwen3.8-27b` (OpenAI format, thinking off by default) |
| Transcription | `whisper.service` | `http://192.0.2.20:8090` | `whisper-large-v3-turbo` |

Both require a bearer key. vLLM's is in `/etc/vllm-qwen38/vllm-qwen38.env`, and
Whisper's is `WHISPER_API_KEY` in `/etc/whisper/whisper.env` (root, 0600). Enter
them only in Alexandria's Settings → Recognition → Audio Note Minutes, and never on a
command line.

## Whisper service

`deploy/whisper/` holds the server (`server.py`, about 80 lines of FastAPI over
faster-whisper), pinned `requirements.txt` and the systemd unit. It serves
`POST /v1/audio/transcriptions` (`json`, `verbose_json` with segments, or
`text`), `GET /v1/models` and an unauthenticated `GET /health`. It runs requests
one at a time with VAD filtering, and accepts uploads up to 64 MiB.

It runs on the **CPU** (int8, 8 threads), so vLLM keeps both GPUs untouched. The
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

To move transcription to a GPU later, set `WHISPER_DEVICE=cuda` and
`WHISPER_COMPUTE_TYPE=int8_float16` with `CUDA_VISIBLE_DEVICES`, after freeing
about 1.5 GB on that card.

Rollback: `sudo systemctl disable --now whisper`. Nothing else on gpu-host was
changed.
