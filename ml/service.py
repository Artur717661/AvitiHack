import hmac
import os
from threading import Lock

from flask import Flask, request

from ml.predictor import LocalIntentRouter


def create_app(predict, key):
    if not key:
        raise ValueError("BUDDY_ROUTER_KEY is required")
    app = Flask(__name__)
    app.config["MAX_CONTENT_LENGTH"] = 16_384
    inference_lock = Lock()

    @app.errorhandler(413)
    def too_large(_):
        return {"error": "request too large"}, 413

    @app.post("/internal/v1/ml/route")
    def route():
        header = request.headers.get("Authorization", "").encode("utf-8")
        if not hmac.compare_digest(header, ("Bearer " + key).encode("utf-8")):
            return {"error": "unauthorized"}, 401
        body = request.get_json(silent=True)
        if not isinstance(body, dict):
            return {"error": "expected JSON object"}, 400
        if not inference_lock.acquire(blocking=False):
            return {"error": "model busy"}, 503
        try:
            return predict(body)
        except Exception as error:
            app.logger.warning("Intent routing failed: %s", type(error).__name__)
            return {"error": "inference failed"}, 502
        finally:
            inference_lock.release()

    return app


def main():
    from waitress import serve

    key = os.environ["BUDDY_ROUTER_KEY"]
    router = LocalIntentRouter(
        os.getenv("BUDDY_QWEN_BASE", ".models/qwen3-1.7b"),
        os.getenv("BUDDY_QWEN_ADAPTER", "reports/train-local-2026-11-14/adapter-final"),
        device=os.getenv("BUDDY_QWEN_DEVICE", "auto"),
    )
    serve(create_app(router.predict, key), host="127.0.0.1", port=int(os.getenv("BUDDY_ML_PORT", "8091")), threads=2)


if __name__ == "__main__":
    main()
