# Run Models on Your Own Hardware

Every AI feature in Alexandria talks to a model over HTTP, so none of them needs a cloud provider. This guide shows how to run each kind of model yourself: a vision model for handwriting, Whisper for transcription, and an embedding model for search by meaning. Then you point Alexandria at them in Settings. Your notes stay on your network, and the bill is electricity.

## What Can Run Locally

| Job | What to run | API Alexandria uses |
|---|---|---|
| Handwriting recognition | A vision model in vLLM, Ollama or another OpenAI-compatible server | OpenAI Chat Completions (`/v1/chat/completions`) |
| Minutes summaries and note actions | Any text model in vLLM, Ollama or another OpenAI-compatible server | OpenAI Chat Completions |
| Transcription | The Whisper server in `deploy/whisper/` | `/v1/audio/transcriptions` |
| Search by meaning | Ollama, either the bundled Compose service or your own (`deploy/ollama-embed/`) | Ollama `/api/embed` |

You can run all of these on one GPU machine (called **gpu-host** below), keep some hosted, or use the bundled CPU embedding service and nothing else.

## What You Need First

- A running Alexandria server ([Install Alexandria Server](install.md)).
- For vision and summary models, realistically an NVIDIA GPU with enough memory for the model you pick. Transcription and embeddings run on a CPU too, just more slowly.
- On gpu-host: Python 3 (for Whisper), the NVIDIA driver, and systemd if you use the unit files as they are.
- A network path from the Alexandria server to gpu-host, ideally a private LAN.

## Embeddings: the Bundled Ollama Service

The simplest option runs Ollama inside your Compose deployment, on the server's CPU. It has no host ports and no GPU; only the Alexandria containers can reach it.

1. In `deploy/compose/`, start it and download the model:

   ```sh
   ./setup-ollama.sh
   ```

   It defaults to `nomic-embed-text:v1.5`; pass another model name as the argument if you want. It doesn't change any settings or start any indexing.
2. Add `ALEXANDRIA_OLLAMA_ENABLED=true` to `.env`, so installs, upgrades and backups include the service.
3. Optionally limit it in `.env` with `ALEXANDRIA_OLLAMA_CPUS` (default `1.0`) and `ALEXANDRIA_OLLAMA_MEMORY` (default `2g`). `ALEXANDRIA_OLLAMA_KEEP_ALIVE` (default `5m`) sets how long the model stays loaded after use.
4. In Settings, use the base URL `http://ollama:11434` (see [Enter the Endpoints](#enter-the-endpoints) below).

It works, but CPU embedding is slow: the first index of a large library can take hours on a core or two. A GPU does the same build in minutes.

## Embeddings: Ollama on a GPU Machine

`deploy/ollama-embed/ollama-embed.service` is a systemd unit for a dedicated embedding Ollama on gpu-host.

1. Install Ollama on gpu-host. The unit expects a release unpacked under `/opt/ollama/<version>/`; if you installed it elsewhere, change `ExecStart`.
2. Copy the unit to `/etc/systemd/system/` and edit it:
   - set `User=` and `Group=` to the account that should run it;
   - set `OLLAMA_MODELS` to where models should live;
   - set `CUDA_VISIBLE_DEVICES` to your GPU's UUID (see [GPU Pinning](#gpu-pinning)), or delete that line on a single-GPU or CPU-only machine;
   - remove the vLLM unit from `After=` if you don't have one.
3. Start it and pull the model:

   ```sh
   sudo systemctl daemon-reload
   sudo systemctl enable --now ollama-embed
   ollama pull nomic-embed-text:v1.5
   ```

The unit listens on port 11434 on every interface and keeps the model loaded (`OLLAMA_KEEP_ALIVE=-1`), so searches don't wait for it to load. Ollama has no authentication; read [Security](#security).

## Transcription: the Whisper Server

`deploy/whisper/` contains a small OpenAI-compatible speech-to-text server built on faster-whisper: `server.py`, a pinned `requirements.txt` and `whisper.service`. It serves `POST /v1/audio/transcriptions`, `GET /v1/models` and an unauthenticated `GET /health`, handles one request at a time, filters out silence, and accepts uploads up to 64 MiB.

1. Copy the three files to gpu-host, for example to `/opt/whisper/`.
2. Create a virtual environment and install the pinned packages:

   ```sh
   python3 -m venv /opt/whisper/env
   /opt/whisper/env/bin/pip install --only-binary=:all: -r /opt/whisper/requirements.txt
   ```

   Keep `av` at the pinned version. The faster-whisper release in the file doesn't work with newer PyAV.
3. Create `/etc/whisper/whisper.env`, readable only by root, with a random key:

   ```sh
   WHISPER_API_KEY=a-long-random-string
   ```

   The server requires that key as a bearer token. Keep it in this file, never on a command line.
4. Edit `whisper.service`: `User=`, `Group=`, the paths, and the device settings below. Then install it:

   ```sh
   sudo cp whisper.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable --now whisper
   ```

   The first start downloads the model into `WHISPER_MODEL_DIR`, which takes a few minutes.
5. Check it: `curl http://gpu-host:8090/health` should return `{"status":"ok"}`.

| Variable | Default | Meaning |
|---|---|---|
| `WHISPER_DEVICE` | `cpu` | `cuda` for a GPU. The shipped unit sets `cuda`. |
| `WHISPER_COMPUTE_TYPE` | `int8` | `int8` on a CPU; the unit uses `int8_float16` on a GPU. |
| `WHISPER_CPU_THREADS` | `8` | CPU threads. |
| `WHISPER_MODEL` | `deepdml/faster-whisper-large-v3-turbo-ct2` | The model to load. |
| `WHISPER_SERVED_NAME` | `whisper-large-v3-turbo` | The model name clients must ask for. |
| `WHISPER_MODEL_DIR` | (download cache) | Where model files are stored. |
| `WHISPER_MAX_BYTES` | 64 MiB | Largest accepted upload. |
| `WHISPER_API_KEY` | (none) | When set, required as `Authorization: Bearer …`. |

**For a GPU**, the unit also sets `LD_LIBRARY_PATH` to the cuBLAS and cuDNN libraries that `requirements.txt` installs into the virtual environment. Fix the `python3.12` part of that path if your Python version differs. **For a CPU**, set `WHISPER_DEVICE=cpu` and `WHISPER_COMPUTE_TYPE=int8`, and remove the `CUDA_VISIBLE_DEVICES` and `LD_LIBRARY_PATH` lines; you can also drop the two `nvidia-` packages from `requirements.txt`.

On a modern 8-thread CPU, expect transcription to take roughly a third of the recording's length. On a GPU, it takes a small fraction of that.

## Vision and Summary Models: vLLM or Ollama

Alexandria works with any server that speaks OpenAI Chat Completions. Two common choices:

- **vLLM**: serve a vision-capable model with something like `vllm serve <model> --host 0.0.0.0 --port 8000 --api-key <key>`. vLLM's `--api-key` makes it require that key, which you then enter in Settings. A reasoning model that thinks before answering is slower at plain transcription; Alexandria's **vLLM only: disable thinking** option turns that off per request.
- **Ollama**: pull a vision model and use Ollama's OpenAI-compatible API at `http://gpu-host:11434`. Ollama has no key, so leave **API key** empty.

One model can do both jobs if it handles images and text well. You can also point the summary model at a smaller, faster text model than the vision one.

Large models and Whisper or Ollama can share a GPU, but vLLM reserves most of a card's memory by default. Lower vLLM's `--gpu-memory-utilization` (and `--max-model-len`, if needed) to leave room for anything else on that card.

## GPU Pinning

On a machine with several GPUs, give each service its own card. That way they don't fight over memory.

- Pin with `CUDA_VISIBLE_DEVICES`, **by UUID**. CUDA's default device order doesn't always match `nvidia-smi`'s, so index numbers can point at the wrong card. List the UUIDs with:

  ```sh
  nvidia-smi --query-gpu=index,uuid --format=csv
  ```

  and replace the `GPU-REPLACE-WITH-…` placeholders in the unit files.
- Some Ollama versions also find GPUs through Vulkan, which ignores `CUDA_VISIBLE_DEVICES`. The shipped unit sets `OLLAMA_VULKAN=false` to keep Ollama on the card you chose.
- After a change, `nvidia-smi` shows which process sits on which card.

## Security

- **Keep model servers off the internet.** Bind them to a private LAN, and firewall their ports (11434, 8090, 8000 or whatever you chose) so only the Alexandria server can reach them.
- **Use keys where the server supports them.** The Whisper server and vLLM can both require a bearer key. Enter it only in Alexandria's Settings, where it's stored encrypted.
- **Ollama has no authentication**, and Alexandria sends no key to the embedding endpoint. The firewall is its only protection. The bundled Compose Ollama has no host port, so only the application network can reach it.
- Keep key files such as `/etc/whisper/whisper.env` readable only by root.

## Enter the Endpoints

Open **Settings → AI Tools and Recognition** ([Set Up AI Models](ai-models.md) explains every field):

| Card | API format | Base URL | Model |
|---|---|---|---|
| **Provider** (Vision Model) | **OpenAI Chat Completions** | `http://gpu-host:8000` (vLLM) or `http://gpu-host:11434` (Ollama) | your vision model's name |
| **Audio Note Minutes** → Transcription | (fixed) | `http://gpu-host:8090` | `whisper-large-v3-turbo` |
| **Audio Note Minutes** → Summary Model | **OpenAI Chat Completions** | as for vision | your text model's name |
| **Search by Meaning** | (Ollama) | `http://ollama:11434` (bundled) or `http://gpu-host:11434` | `nomic-embed-text:v1.5` |

Never include `/v1` in a base URL. Test each card before saving it. For search, then go to **Settings → Search** and choose **Enable and build index**, or **Build replacement index** if you've switched from another embedding endpoint. The old index keeps working until the new one is ready.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| A connection test times out | A firewall, or the service is bound to `127.0.0.1`. | From the Alexandria host, try `curl http://gpu-host:8090/health` (Whisper) or `curl http://gpu-host:11434/api/tags` (Ollama). Open the port to that host only. |
| Whisper test fails with 401 | The key in Settings doesn't match `WHISPER_API_KEY`. | Re-enter it with **Replace with entered key**. |
| Whisper says the model *is not served here* | The **Model** in Settings doesn't match `WHISPER_SERVED_NAME`. | Use `whisper-large-v3-turbo`, or whatever you set. |
| Whisper fails to start with a cuBLAS or cuDNN error | `LD_LIBRARY_PATH` doesn't point at the virtual environment's NVIDIA libraries. | Fix the path (check the Python version), or run on the CPU. |
| Whisper fails after a package upgrade | A newer PyAV broke faster-whisper. | Reinstall the pinned `requirements.txt`. |
| Settings → Search shows **Model missing** | The embedding model isn't pulled on that Ollama. | Run `ollama pull nomic-embed-text:v1.5` there (or `./setup-ollama.sh` for the bundled one). |
| **Provider** shows **Not configured** | No embedding URL and model are saved. | Save them on Settings → AI Tools and Recognition → **Search by Meaning**. |
| **Provider** shows **Unavailable** with the bundled service | The Ollama container isn't running. | Run `./setup-ollama.sh`, and set `ALEXANDRIA_OLLAMA_ENABLED=true` so it starts with everything else. |
| The index build crawls | Embeddings are running on a small CPU. | Expected. Move embedding to a GPU and build a replacement index. |
| A service lands on the wrong GPU, or runs out of memory | Pinned by index instead of UUID, Ollama is using Vulkan, or vLLM took the whole card. | Pin by UUID, keep `OLLAMA_VULKAN=false`, and lower vLLM's memory share. |
| Recognition is slow or returns its reasoning instead of text | A reasoning model is thinking first. | Tick **vLLM only: disable thinking**, or choose a non-reasoning vision model. |
