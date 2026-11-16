"""Download only public Qwen3 data/weights into the project, never remote code."""

from __future__ import annotations

import json
import hashlib
import os
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
os.environ["HF_HOME"] = str(ROOT / ".cache" / "huggingface")
os.environ["HF_HUB_DISABLE_XET"] = "1"


def main() -> None:
    from huggingface_hub import HfApi, hf_hub_url
    import requests

    repo = "Qwen/Qwen3-1.7B"
    revision = "70d244cc86ccca08cf5af4e1e306ecf908b1ad5e"
    info = HfApi(token=False).model_info(repo, revision=revision, files_metadata=True)
    if info.sha != revision:
        raise RuntimeError("The model metadata does not match the pinned revision")
    if not revision:
        raise RuntimeError("The public model revision could not be resolved")
    target = ROOT / ".models" / "qwen3-1.7b"
    print(json.dumps({"repo": repo, "revision": revision, "target": str(target)}), flush=True)
    target.mkdir(parents=True, exist_ok=True)
    allowed = {"config.json", "generation_config.json", "tokenizer.json",
               "tokenizer_config.json", "special_tokens_map.json", "vocab.json",
               "merges.txt", "tokenizer.model", "model.safetensors.index.json", "README.md", "LICENSE"}
    for item in info.siblings:
        name = item.rfilename
        if "/" in name or (name not in allowed and not name.endswith(".safetensors")):
            continue
        destination = target / name
        expected_hash = item.lfs.sha256 if item.lfs else None
        if destination.exists() and destination.stat().st_size == item.size:
            with destination.open("rb") as handle:
                if not expected_hash or hashlib.file_digest(handle, "sha256").hexdigest() == expected_hash:
                    continue
        partial = destination.with_suffix(destination.suffix + ".download")
        digest = hashlib.sha256()
        received = partial.stat().st_size if partial.exists() else 0
        if received and received >= item.size:
            with partial.open("rb") as handle:
                valid = received == item.size and (not expected_hash or hashlib.file_digest(handle, "sha256").hexdigest() == expected_hash)
            if valid:
                partial.replace(destination)
                continue
            received = 0
        if received:
            with partial.open("rb") as handle:
                for chunk in iter(lambda: handle.read(4 * 1024 * 1024), b""):
                    digest.update(chunk)
        headers = {"Range": f"bytes={received}-"} if received else {}
        print(f"Downloading {name}: {item.size} bytes", flush=True)
        # Public, revision-pinned official URL; no user credentials or remote code.
        with requests.get(hf_hub_url(repo, name, revision=revision), headers=headers, stream=True, timeout=(15, 45)) as response:
            response.raise_for_status()
            if response.status_code == 206:
                expected_range = f"bytes {received}-{item.size - 1}/{item.size}"
                if response.headers.get("Content-Range") != expected_range:
                    raise RuntimeError(f"Unexpected byte range for {name}")
            else:
                digest = hashlib.sha256()
                received = 0
            next_progress = received + 256 * 1024 * 1024
            with partial.open("ab" if received else "wb") as handle:
                for chunk in response.iter_content(chunk_size=4 * 1024 * 1024):
                    if not chunk:
                        continue
                    handle.write(chunk)
                    digest.update(chunk)
                    received += len(chunk)
                    if received >= next_progress:
                        print(f"{name}: {received // (1024 * 1024)} MiB", flush=True)
                        next_progress += 256 * 1024 * 1024
        if received != item.size:
            raise RuntimeError(f"Incomplete file {name}; rerun to continue the download")
        if expected_hash and digest.hexdigest() != expected_hash:
            partial.unlink()
            raise RuntimeError(f"SHA-256 verification failed for {name}; rerun to download it again")
        partial.replace(destination)
    manifest = {
        "repo_id": repo,
        "revision": revision,
        "local_path": str(target.relative_to(ROOT)),
        "trust_remote_code": False,
        "files": [
            {"name": str(p.relative_to(target)), "size_bytes": p.stat().st_size}
            for p in sorted(target.rglob("*"))
            if p.is_file() and ".cache" not in p.relative_to(target).parts
        ],
    }
    reports = ROOT / "reports"
    reports.mkdir(exist_ok=True)
    (reports / "base-model-manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8"
    )
    print("Model files downloaded. No inference or training was performed.", flush=True)


if __name__ == "__main__":
    main()
