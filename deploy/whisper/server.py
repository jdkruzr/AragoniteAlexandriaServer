"""Minimal OpenAI-compatible speech-to-text server for Alexandria note minutes.

Serves POST /v1/audio/transcriptions (multipart: file, model, language, prompt,
response_format json|verbose_json|text) and GET /v1/models with one
faster-whisper model. Requests are serialized; set WHISPER_API_KEY to require a
bearer token. See docs/deployment-local-models.md.
"""

import hmac
import os
import tempfile
import threading

from fastapi import Depends, FastAPI, File, Form, Header, HTTPException, UploadFile
from fastapi.responses import PlainTextResponse
from faster_whisper import WhisperModel

MODEL = os.environ.get("WHISPER_MODEL", "deepdml/faster-whisper-large-v3-turbo-ct2")
NAME = os.environ.get("WHISPER_SERVED_NAME", "whisper-large-v3-turbo")
DEVICE = os.environ.get("WHISPER_DEVICE", "cpu")
COMPUTE = os.environ.get("WHISPER_COMPUTE_TYPE", "int8")
THREADS = int(os.environ.get("WHISPER_CPU_THREADS", "8"))
MAX_BYTES = int(os.environ.get("WHISPER_MAX_BYTES", str(64 << 20)))
KEY = os.environ.get("WHISPER_API_KEY", "")

model = WhisperModel(MODEL, device=DEVICE, compute_type=COMPUTE, cpu_threads=THREADS,
                     download_root=os.environ.get("WHISPER_MODEL_DIR"))
lock = threading.Lock()
app = FastAPI(docs_url=None, redoc_url=None, openapi_url=None)


def authorize(authorization: str = Header(default="")):
    if KEY and not hmac.compare_digest(authorization, "Bearer " + KEY):
        raise HTTPException(401, "invalid API key")


@app.get("/health")
def health():
    return {"status": "ok"}


@app.get("/v1/models", dependencies=[Depends(authorize)])
def models():
    return {"object": "list", "data": [{"id": NAME, "object": "model", "owned_by": "local"}]}


@app.post("/v1/audio/transcriptions", dependencies=[Depends(authorize)])
def transcribe(file: UploadFile = File(...), model_name: str = Form("", alias="model"),
               language: str = Form(""), prompt: str = Form(""),
               response_format: str = Form("json"), temperature: float = Form(0.0)):
    if model_name and model_name != NAME:
        raise HTTPException(404, f"model {model_name!r} is not served here")
    if response_format not in ("json", "verbose_json", "text"):
        raise HTTPException(400, "response_format must be json, verbose_json or text")
    with tempfile.NamedTemporaryFile(suffix=os.path.splitext(file.filename or "")[1]) as tmp:
        size = 0
        while chunk := file.file.read(1 << 20):
            size += len(chunk)
            if size > MAX_BYTES:
                raise HTTPException(413, "audio is too large")
            tmp.write(chunk)
        tmp.flush()
        with lock:
            try:
                segments, info = model.transcribe(
                    tmp.name, language=language or None, initial_prompt=prompt or None,
                    temperature=temperature, vad_filter=True)
                segments = list(segments)
            except Exception as exc:  # undecodable audio is the caller's problem
                raise HTTPException(400, f"audio could not be decoded: {type(exc).__name__}")
    text = " ".join(s.text.strip() for s in segments).strip()
    if response_format == "text":
        return PlainTextResponse(text)
    if response_format == "json":
        return {"text": text}
    return {
        "task": "transcribe", "language": info.language, "duration": info.duration, "text": text,
        "segments": [{"id": i, "start": round(s.start, 2), "end": round(s.end, 2), "text": s.text.strip()}
                     for i, s in enumerate(segments)],
    }
