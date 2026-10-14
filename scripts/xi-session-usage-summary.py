#!/usr/bin/env python3
"""Summarize local session usage without emitting message or tool contents."""

import argparse
import collections
from datetime import datetime
import json
import math
from pathlib import Path
import re
import statistics


def instant(value):
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if result.tzinfo is None:
        raise ValueError("timestamps must include a timezone")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("log", type=Path)
    parser.add_argument("--start", required=True, type=instant)
    parser.add_argument("--end", required=True, type=instant)
    parser.add_argument("--turn-id", action="append", default=[])
    args = parser.parse_args()
    if args.end <= args.start:
        parser.error("end must be after start; range is [start, end)")
    wanted = set(args.turn_id)
    responses = {}
    duplicates = conflicts = missing_ids = malformed = compactions = outputs = truncated = 0
    current_turn = None
    call_names = {}
    output_sizes = []
    marker = re.compile(r"Warning: truncated output|Output (?:is )?truncated|\d[\d,]* tokens truncated", re.I)
    with args.log.open() as source:
        for line_number, line in enumerate(source, 1):
            try:
                record = json.loads(line)
            except json.JSONDecodeError:
                malformed += 1
                continue
            payload = record.get("payload", {})
            if not isinstance(payload, dict):
                continue
            kind = record.get("type")
            if kind == "response_item" and payload.get("type") in ("function_call", "custom_tool_call"):
                call_names[payload.get("call_id")] = payload.get("name")
            if kind == "turn_context" or (kind == "event_msg" and payload.get("type") == "task_started"):
                current_turn = payload.get("turn_id") or current_turn
            try:
                timestamp = instant(record["timestamp"])
            except (KeyError, TypeError, ValueError):
                continue
            if not args.start <= timestamp < args.end:
                continue
            turn = payload.get("turn_id") or current_turn
            if wanted and turn not in wanted:
                continue
            if kind == "token_usage_record":
                identity = payload.get("response_id")
                if not identity:
                    missing_ids += 1
                    continue
                usage = payload.get("usage", {})
                if identity in responses:
                    duplicates += 1
                    conflicts += responses[identity][0] != usage
                    continue
                responses[identity] = (usage, timestamp)
            elif kind == "compacted":
                compactions += 1
            elif kind == "response_item" and payload.get("type") in ("function_call_output", "custom_tool_call_output"):
                outputs += 1
                value = payload.get("output", "")
                text = value if isinstance(value, str) else json.dumps(value)
                truncated += bool(marker.search(text))
                output_sizes.append((len(text.encode("utf-8")), line_number, call_names.get(payload.get("call_id"))))
    totals = collections.Counter()
    for usage, _ in responses.values():
        totals.update({key: value for key, value in usage.items() if isinstance(value, (int, float))})
    inputs = sorted(usage["input_tokens"] for usage, _ in responses.values())
    timestamps = [stamp for _, stamp in responses.values()]
    sizes = sorted(size for size, _, _ in output_sizes)
    result = {
        "source": str(args.log.resolve()),
        "window_inclusive_start": args.start.isoformat(),
        "window_exclusive_end": args.end.isoformat(),
        "turn_ids": args.turn_id,
        "responses": len(responses),
        "input_median": statistics.median(inputs) if inputs else None,
        "input_p90_nearest_rank": inputs[math.ceil(len(inputs) * .9) - 1] if inputs else None,
        "totals": dict(totals),
        "input_cache_hit_percent": 100 * totals["cached_input_tokens"] / totals["input_tokens"] if totals["input_tokens"] else None,
        "compactions": compactions,
        "tool_outputs": outputs,
        "outputs_with_explicit_truncation_markers": truncated,
        "tool_output_utf8_bytes": {
            "total": sum(sizes),
            "median": statistics.median(sizes) if sizes else None,
            "p90_nearest_rank": sizes[math.ceil(len(sizes) * .9) - 1] if sizes else None,
            "largest_five": [
                {"bytes": size, "source_line": line, "tool": tool}
                for size, line, tool in sorted(output_sizes, reverse=True)[:5]
            ],
        },
        "response_timestamp_span_seconds": (max(timestamps) - min(timestamps)).total_seconds() if timestamps else None,
        "duplicate_response_records": duplicates,
        "conflicting_duplicate_records": conflicts,
        "missing_response_ids": missing_ids,
        "malformed_json_lines_in_source": malformed,
        "limits": "Root session only; no manager usage or billing estimate. Reasoning is part of output. Timestamp span includes waiting and is not active compute time. Tool bytes measure recorded output text, not model tokens or untruncated source size. Truncation markers can be quoted in tool content. First duplicate retained; conflicts invalidate exact totals.",
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
