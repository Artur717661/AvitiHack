"""Verify the seven known handoff requests through a running Qwen HTTP router."""

import json
import os
import time
from pathlib import Path
from urllib.request import Request, urlopen


ROOT = Path(__file__).resolve().parents[1]
cases = json.loads((ROOT / "examples/ml_handoff_alpha.json").read_text(encoding="utf-8"))["cases"]
url = os.environ.get("BUDDY_ROUTER_URL", "http://127.0.0.1:8091/internal/v1/ml/route")
key = os.environ["BUDDY_ROUTER_KEY"]
results = []
for case in cases:
    started = time.perf_counter()
    request = Request(url, data=json.dumps(case["request"], ensure_ascii=False).encode("utf-8"), headers={
        "Content-Type": "application/json",
        "Authorization": f"Bearer {key}",
    })
    try:
        with urlopen(request, timeout=50) as response:
            actual = json.load(response)
    except (OSError, ValueError) as error:
        actual = {"error": str(error)}
    elapsed = round(time.perf_counter() - started, 3)
    results.append({
        "id": case["id"], "expected": case["expected_response"], "actual": actual,
        "match": actual == case["expected_response"], "seconds": elapsed,
    })
report = {"total": len(results), "correct": sum(result["match"] for result in results), "cases": results}
print(json.dumps(report, ensure_ascii=False, indent=2))
if report["correct"] != report["total"]:
    raise SystemExit(1)
