#!/usr/bin/env python3
"""OpenAI-compatible upstream fixture for LiteLLM component CI.

The server intentionally records no request body and emits no credential. It
supports repeatable successful calls with deterministic status/content/usage
and a fresh opaque response ID per call, plus one controlled 400 response.
"""

from __future__ import annotations

import argparse
import json
import threading
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any


MAX_REQUEST_BYTES = 64 * 1024
UPSTREAM_BEARER = "Bearer sk-xinsight-ci-upstream-not-secret"
SUCCESS_PROMPT = "xinsight-privacy-v1-7a91d0-success-prompt"
FAILURE_PROMPT = "xinsight-privacy-v1-7a91d0-failure-prompt"
SUCCESS_RESPONSE = "xinsight-privacy-v1-7a91d0-success-response"
CHAT_COMPLETION_ID_PREFIX = "chatcmpl-xinsight-component-"
CHAT_RESPONSE_STATUSES = (200, 400, 401, 404, 413, 422)
_chat_post_count = 0
_chat_success_response_started_count = 0
_chat_success_response_completed_count = 0
_chat_response_status_counts = {
    status: 0 for status in CHAT_RESPONSE_STATUSES
}
_chat_metrics_lock = threading.Lock()


def _new_chat_completion_id() -> str:
    # Real upstream completions receive a fresh opaque identity per call. Keep
    # every non-identity fixture field deterministic.
    return f"{CHAT_COMPLETION_ID_PREFIX}{uuid.uuid4().hex}"


def _record_chat_post() -> None:
    global _chat_post_count
    with _chat_metrics_lock:
        _chat_post_count += 1


def _record_chat_success_response_started() -> None:
    global _chat_success_response_started_count
    with _chat_metrics_lock:
        _chat_success_response_started_count += 1


def _record_chat_response_completed(status: int) -> None:
    global _chat_success_response_completed_count
    if status not in _chat_response_status_counts:
        return
    with _chat_metrics_lock:
        if status == 200:
            _chat_success_response_completed_count += 1
        _chat_response_status_counts[status] += 1


def _get_chat_metrics() -> dict[str, int]:
    with _chat_metrics_lock:
        metrics = {
            "chat_post_count": _chat_post_count,
            "chat_success_response_started_count": (
                _chat_success_response_started_count
            ),
            "chat_success_response_completed_count": (
                _chat_success_response_completed_count
            ),
        }
        metrics.update(
            {
                f"chat_response_status_{status}_count": (
                    _chat_response_status_counts[status]
                )
                for status in CHAT_RESPONSE_STATUSES
            }
        )
        return metrics


class Handler(BaseHTTPRequestHandler):
    server_version = "xinsight-litellm-ci-upstream/1"
    sys_version = ""

    def version_string(self) -> str:
        # Keep the deterministic fixture identity free of optional whitespace.
        return self.server_version

    def log_message(self, _format: str, *_args: Any) -> None:
        return

    def do_GET(self) -> None:
        if self.path != "/health":
            self._json(404, {"error": {"code": "not_found"}})
            return
        self._json(
            200,
            {
                "status": "ok",
                **_get_chat_metrics(),
            },
        )

    def do_POST(self) -> None:
        if self.path != "/v1/chat/completions":
            self._chat_json(404, {"error": {"code": "not_found"}})
            return
        # This is the only retained request observation. It proves that the
        # observer-denial attempt never reached the private upstream without
        # retaining a credential, request body, prompt, response, or header.
        _record_chat_post()
        if self.headers.get("Authorization") != UPSTREAM_BEARER:
            self._chat_json(401, {"error": {"code": "unauthorized"}})
            return
        try:
            content_length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            self._chat_json(
                400,
                {"error": {"code": "invalid_content_length"}},
            )
            return
        if content_length <= 0 or content_length > MAX_REQUEST_BYTES:
            self._chat_json(
                413,
                {"error": {"code": "request_size_invalid"}},
            )
            return
        try:
            payload = json.loads(self.rfile.read(content_length))
        except (UnicodeDecodeError, json.JSONDecodeError):
            self._chat_json(400, {"error": {"code": "invalid_json"}})
            return
        messages = payload.get("messages")
        if not isinstance(messages, list):
            self._chat_json(400, {"error": {"code": "messages_invalid"}})
            return
        controlled_failure = any(
            isinstance(message, dict)
            and message.get("content") == FAILURE_PROMPT
            for message in messages
        )
        if controlled_failure:
            self._chat_json(
                400,
                {
                    "error": {
                        "message": "deterministic controlled upstream failure",
                        "type": "invalid_request_error",
                        "param": None,
                        "code": "xinsight_controlled_failure",
                    }
                },
            )
            return
        self._chat_json(
            200,
            {
                "id": _new_chat_completion_id(),
                "object": "chat.completion",
                "created": 1785182400,
                "model": "xinsight-ci-upstream-model",
                "choices": [
                    {
                        "index": 0,
                        "message": {
                            "role": "assistant",
                            "content": SUCCESS_RESPONSE,
                        },
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": 3,
                    "completion_tokens": 2,
                    "total_tokens": 5,
                },
            },
        )

    def _chat_json(self, status: int, payload: dict[str, Any]) -> None:
        if status == 200:
            _record_chat_success_response_started()
        self._json(status, payload)
        self.wfile.flush()
        _record_chat_response_completed(status)

    def _json(self, status: int, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, separators=(",", ":"), sort_keys=True).encode(
            "utf-8"
        )
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", default=18081, type=int)
    args = parser.parse_args()
    ThreadingHTTPServer((args.host, args.port), Handler).serve_forever()


if __name__ == "__main__":
    main()
