import unittest
from concurrent.futures import ThreadPoolExecutor
from threading import Event

from ml.router_contract import build_model_messages, parse_model_response, render_prompt
from ml.service import create_app


INPUT = {
    "message": "Что дальше?",
    "workflow_state": "pending_approval",
    "actor_context": {"user_id": "demo-intern-1", "team_id": "alpha", "role": "intern"},
}
ROUTE = "/internal/v1/ml/route"
HEADERS = {"Authorization": "Bearer test"}


class ModelServiceTests(unittest.TestCase):
    def test_prompt_projects_only_message_and_state(self):
        messages = build_model_messages(INPUT)
        self.assertIn("Что дальше?", messages[1]["content"])
        self.assertIn("pending_approval", messages[1]["content"])
        self.assertNotIn("demo-intern-1", messages[1]["content"])

        class Tokenizer:
            def apply_chat_template(self, prompt, **options):
                self.options = options
                return prompt[1]["content"]

        tokenizer = Tokenizer()
        self.assertEqual(render_prompt(tokenizer, INPUT), messages[1]["content"])
        self.assertFalse(tokenizer.options["enable_thinking"])
        self.assertEqual(parse_model_response('{"intent":"next_step"}'), {"intent": "next_step"})
        for answer in (None, '{"intent":"next_step","intent":"clarify"}', '{"intent":"submit"}'):
            with self.subTest(answer=answer), self.assertRaises(Exception):
                parse_model_response(answer)

    def test_authorization_payload_and_prediction(self):
        with self.assertRaises(ValueError):
            create_app(lambda body: body, "")

        def predict(body):
            self.assertEqual(body, INPUT)
            return {"intent": "next_step"}

        client = create_app(predict, "test").test_client()
        self.assertEqual(client.post(ROUTE, json=INPUT).status_code, 401)
        self.assertEqual(client.post(ROUTE, json=[], headers=HEADERS).status_code, 400)
        self.assertEqual(client.post(ROUTE, json={"long": "x" * 20000}, headers=HEADERS).status_code, 413)
        self.assertEqual(client.post(ROUTE, json=INPUT, headers=HEADERS).json, {"intent": "next_step"})

    def test_model_failure_and_retry(self):
        times = 0

        def predict(_):
            nonlocal times
            times += 1
            if times == 1:
                raise ValueError("failed")
            return {"intent": "next_step"}

        client = create_app(predict, "test").test_client()
        self.assertEqual(client.post(ROUTE, json=INPUT, headers=HEADERS).status_code, 502)
        self.assertEqual(client.post(ROUTE, json=INPUT, headers=HEADERS).status_code, 200)

    def test_concurrent_inference_is_bounded(self):
        active, release = Event(), Event()

        def predict(_):
            active.set()
            self.assertTrue(release.wait(2))
            return {"intent": "next_step"}

        app = create_app(predict, "test")
        with ThreadPoolExecutor(max_workers=1) as pool:
            call = pool.submit(app.test_client().post, ROUTE, json=INPUT, headers=HEADERS)
            try:
                self.assertTrue(active.wait(2))
                self.assertEqual(app.test_client().post(ROUTE, json=INPUT, headers=HEADERS).status_code, 503)
            finally:
                release.set()
            self.assertEqual(call.result().status_code, 200)
