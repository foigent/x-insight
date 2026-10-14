#!/usr/bin/env python3
"""Classify a bounded private LiteLLM traceback into closed diagnostic enums.

The caller keeps the source log private. This program emits only three bounded
enums and never emits traceback text, paths, functions, line numbers, exception
messages, or exception arguments.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path


MAX_LOG_BYTES = 512 * 1024
MAX_LOG_LINES = 200
PINNED_LITELLM_SOURCE_COMMIT = "052b5a2169d8d3082e1d66e69f200a72b0c1e274"
EXCEPTION_ENUMS = {
    "AttributeError",
    "TypeError",
    "KeyError",
    "LocalProtocolError",
    "PydanticSerializationError",
    "RuntimeError",
    "UnicodeEncodeError",
    "ValueError",
    "ValidationError",
    "CancelledError",
}
PROVIDER_ANCHORS = (
    "litellm/llms/custom_httpx/",
    "litellm/llms/openai/chat/",
)
UPSTREAM_TRANSPORT_ANCHORS = (
    "httpx/",
    "httpcore/",
)
FRAME_STAGE_PATTERNS = (
    (
        "model_conversion",
        (
            "litellm/types/utils.py",
            "litellm/utils.py",
            "litellm/litellm_core_utils/core_helpers.py",
            "litellm/litellm_core_utils/streaming_handler.py",
        ),
    ),
    (
        "post_call_hook",
        (
            "litellm/proxy/proxy_logging.py",
            "litellm/litellm_core_utils/litellm_logging.py",
            "litellm/proxy/spend_tracking/",
            "litellm/integrations/",
        ),
    ),
    (
        "response_serialization",
        (
            "fastapi/encoders.py",
            "starlette/responses.py",
        ),
    ),
    (
        "proxy_response_pipeline",
        (
            "litellm/proxy/proxy_server.py",
            "litellm/proxy/common_request_processing.py",
        ),
    ),
)
ASGI_TRANSPORT_ANCHORS = (
    "uvicorn/protocols/http/h11_impl.py",
    "uvicorn/protocols/http/httptools_impl.py",
    "starlette/middleware/base.py",
    "starlette/middleware/errors.py",
    "starlette/middleware/exceptions.py",
)
ASYNC_RUNTIME_ANCHORS = (
    "anyio/",
    "asyncio/",
)
DOCKER_TIMESTAMP = re.compile(
    r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z\s"
)
TRACEBACK_START = "Traceback (most recent call last):"
FRAME_PATTERN = re.compile(
    r'^\s*File "([^"\r\n]+)", line ([1-9][0-9]*), '
    r"in ([A-Za-z0-9_.<>]+)\s*$"
)
EXCEPTION_PATTERN = re.compile(
    r"^(?:[A-Za-z_][A-Za-z0-9_.]*\.)?"
    r"([A-Za-z_][A-Za-z0-9_]*(?:Error|Exception))(?::|$)"
)


def _unavailable() -> tuple[str, str, str]:
    return "unavailable", "unavailable", "unavailable"


def _strip_docker_timestamp(line: str) -> str:
    return DOCKER_TIMESTAMP.sub("", line, count=1)


def _allowlisted_relative_path(path: str) -> str | None:
    normalized = path.replace("\\", "/")
    for marker in (
        "/site-packages/",
        "/dist-packages/",
        "/app/",
    ):
        if marker in normalized:
            return normalized.rsplit(marker, 1)[1]
    if normalized.startswith(
        (
            "litellm/",
            "fastapi/",
            "h11/",
            "starlette/",
            "uvicorn/",
            "httpx/",
            "httpcore/",
            "anyio/",
            "asyncio/",
        )
    ):
        return normalized
    asyncio_marker = "/asyncio/"
    if "/lib/python" in normalized and asyncio_marker in normalized:
        return "asyncio/" + normalized.rsplit(asyncio_marker, 1)[1]
    return None


def _matches_anchor(path: str, anchor: str) -> bool:
    if anchor.endswith("/"):
        return path.startswith(anchor)
    return path == anchor


def _matches_any(path: str, anchors: tuple[str, ...]) -> bool:
    return any(_matches_anchor(path, anchor) for anchor in anchors)


def _classify_stage(frames: list[tuple[str, int, str]]) -> str:
    relative_paths = [
        relative
        for path, _line_number, _function_name in frames
        if (relative := _allowlisted_relative_path(path)) is not None
    ]
    provider_context = any(
        _matches_any(path, PROVIDER_ANCHORS) for path in relative_paths
    )
    upstream_transport_context = any(
        _matches_any(path, UPSTREAM_TRANSPORT_ANCHORS)
        for path in relative_paths
    )
    if provider_context:
        if upstream_transport_context:
            return "upstream_transport"
        return "upstream_decode"

    for path in reversed(relative_paths):
        for stage, patterns in FRAME_STAGE_PATTERNS:
            if _matches_any(path, patterns):
                return stage
    if any(
        _matches_any(path, ASGI_TRANSPORT_ANCHORS)
        for path in relative_paths
    ):
        return "asgi_transport"
    if any(
        _matches_any(path, ASYNC_RUNTIME_ANCHORS)
        for path in relative_paths
    ):
        return "async_runtime"
    return "unavailable"


def _classify_serialization_substage(
    frames: list[tuple[str, int, str]],
) -> str:
    relative_frames = [
        (relative, line_number, function_name)
        for path, line_number, function_name in frames
        if (relative := _allowlisted_relative_path(path)) is not None
    ]
    for path, line_number, function_name in reversed(relative_frames):
        if path == "h11/_headers.py" and function_name == "normalize_and_validate":
            return "h11_header_validation"
        if path == "h11/_writers.py" and function_name in {
            "send_data",
            "send_eom",
        }:
            return "h11_content_length_body"
        if path == "starlette/responses.py" and function_name == "init_headers":
            return "starlette_header_init"
        if path == "starlette/responses.py" and function_name == "render":
            return "starlette_render"
        if path == "starlette/responses.py" and function_name == "__call__":
            # The pinned Starlette 1.3.1 Response.__call__ has exactly one
            # response-start send on line 166 and one response-body send on
            # line 167. The line is consumed only as a closed classifier input
            # and is never emitted.
            if line_number == 166:
                return "starlette_send_start"
            if line_number == 167:
                return "starlette_send_body"
        if path == "fastapi/encoders.py" and function_name == "jsonable_encoder":
            return "fastapi_jsonable_encoder"
    return "unavailable"


def _classify_exception(lines: list[str]) -> str:
    for line in reversed(lines):
        match = EXCEPTION_PATTERN.match(line.strip())
        if match is None:
            continue
        exception_name = match.group(1)
        if exception_name in EXCEPTION_ENUMS:
            return exception_name
        return "Other"
    return "unavailable"


def classify_log_text(
    text: str,
    source_commit: str = PINNED_LITELLM_SOURCE_COMMIT,
) -> tuple[str, str, str]:
    if source_commit != PINNED_LITELLM_SOURCE_COMMIT:
        return _unavailable()
    try:
        encoded = text.encode("utf-8")
    except UnicodeEncodeError:
        return _unavailable()
    if not encoded or len(encoded) > MAX_LOG_BYTES:
        return _unavailable()

    raw_lines = text.splitlines()
    if not raw_lines or len(raw_lines) > MAX_LOG_LINES:
        return _unavailable()
    lines = [_strip_docker_timestamp(line) for line in raw_lines]
    traceback_indexes = [
        index
        for index, line in enumerate(lines)
        if line.strip() == TRACEBACK_START
    ]
    if not traceback_indexes:
        return _unavailable()

    traceback_lines = lines[traceback_indexes[-1] :]
    frames: list[tuple[str, int, str]] = []
    for line in traceback_lines:
        match = FRAME_PATTERN.match(line)
        if match is not None:
            frames.append((match.group(1), int(match.group(2)), match.group(3)))

    exception_class = _classify_exception(traceback_lines)
    failure_stage = _classify_stage(frames)
    serialization_substage = _classify_serialization_substage(frames)
    return exception_class, failure_stage, serialization_substage


def classify_log_path(
    path: Path,
    source_commit: str,
) -> tuple[str, str, str]:
    try:
        if not path.is_file() or path.stat().st_size > MAX_LOG_BYTES:
            return _unavailable()
        text = path.read_text(encoding="utf-8")
    except (OSError, UnicodeError):
        return _unavailable()
    return classify_log_text(text, source_commit)


def main() -> int:
    result = _unavailable()
    try:
        if len(sys.argv) == 3:
            result = classify_log_path(Path(sys.argv[1]), sys.argv[2])
    except Exception:
        result = _unavailable()
    sys.stdout.write(f"{result[0]}|{result[1]}|{result[2]}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
