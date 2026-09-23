#!/usr/bin/env python3
"""Run one authorized, low-cost Volcengine TTS -> ASR production check."""

import argparse
import hashlib
import json
import os
import sqlite3
import sys
import tempfile
import time
import uuid
from urllib import error, request


REQUEST_ID_HEADER = "X-Oneapi-Request-Id"


def parse_args():
    parser = argparse.ArgumentParser(
        description=(
            "Validate production sxh-tts and sxh-asr without printing the test token. "
            "This creates real billable requests."
        )
    )
    parser.add_argument("--base-url", required=True, help="Public API base URL")
    parser.add_argument("--db", required=True, help="SQLite database containing the test token")
    parser.add_argument(
        "--log-db",
        help="SQLite log database; defaults to --db",
    )
    parser.add_argument("--token-id", required=True, type=int, help="Dedicated test Token ID")
    parser.add_argument(
        "--text",
        default="上线验证。",
        help="Short non-sensitive TTS text; never use private business content",
    )
    parser.add_argument("--timeout", type=float, default=30.0, help="HTTP timeout in seconds")
    parser.add_argument(
        "--confirm-live-request",
        action="store_true",
        help="Required acknowledgement that two real billable requests will be sent",
    )
    args = parser.parse_args()
    if not args.confirm_live_request:
        parser.error("--confirm-live-request is required")
    if not args.text.strip():
        parser.error("--text must not be empty")
    if len(args.text) > 30:
        parser.error("--text must be at most 30 characters")
    return args


def query_rows(path, sql, params=()):
    last_error = None
    for _ in range(5):
        try:
            connection = sqlite3.connect(path, timeout=10)
            try:
                connection.execute("PRAGMA busy_timeout = 10000")
                return connection.execute(sql, params).fetchall()
            finally:
                connection.close()
        except sqlite3.OperationalError as exc:
            last_error = exc
            if "locked" not in str(exc).lower():
                raise
            time.sleep(1)
    raise last_error


def api_request(base_url, path, method="GET", body=None, headers=None, timeout=30):
    url = base_url.rstrip("/") + path
    req = request.Request(url, data=body, method=method)
    for key, value in (headers or {}).items():
        req.add_header(key, value)
    try:
        response = request.urlopen(req, timeout=timeout)
        try:
            return response.getcode(), response.headers, response.read()
        finally:
            response.close()
    except error.HTTPError as exc:
        exc.read(1024)
        raise RuntimeError("{} returned HTTP {}".format(path, exc.code))


def parse_json(label, payload):
    try:
        return json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        raise RuntimeError("{} did not return valid JSON".format(label))


def multipart_audio(model, audio):
    boundary = "sxh-validation-{}".format(uuid.uuid4().hex)
    marker = boundary.encode("ascii")
    body = bytearray()

    def add_field(name, value):
        body.extend(b"--" + marker + b"\r\n")
        body.extend(
            'Content-Disposition: form-data; name="{}"\r\n\r\n'.format(name).encode(
                "ascii"
            )
        )
        body.extend(value.encode("utf-8"))
        body.extend(b"\r\n")

    add_field("model", model)
    add_field("response_format", "verbose_json")
    add_field("timestamp_granularities[]", "segment")
    body.extend(b"--" + marker + b"\r\n")
    body.extend(
        b'Content-Disposition: form-data; name="file"; filename="validation.mp3"\r\n'
    )
    body.extend(b"Content-Type: audio/mpeg\r\n\r\n")
    body.extend(audio)
    body.extend(b"\r\n--" + marker + b"--\r\n")
    return bytes(body), "multipart/form-data; boundary={}".format(boundary)


def require_request_id(headers, label):
    request_id = headers.get(REQUEST_ID_HEADER)
    if not request_id:
        raise RuntimeError("{} response is missing {}".format(label, REQUEST_ID_HEADER))
    return request_id


def main():
    args = parse_args()
    log_db = args.log_db or args.db
    token_rows = query_rows(
        args.db,
        """
        SELECT key, status, remain_quota, unlimited_quota, used_quota
        FROM tokens
        WHERE id = ?
        """,
        (args.token_id,),
    )
    if len(token_rows) != 1:
        raise RuntimeError("test Token ID was not found")
    token_key, status, before_remain, unlimited, before_used = token_rows[0]
    if status != 1 or not token_key:
        raise RuntimeError("test Token is not enabled")
    if unlimited:
        raise RuntimeError("test Token must have limited quota for deduction validation")
    auth_token = token_key if token_key.startswith("sk-") else "sk-" + token_key
    auth_headers = {"Authorization": "Bearer " + auth_token}

    models_status, _, models_body = api_request(
        args.base_url,
        "/v1/models",
        headers=auth_headers,
        timeout=args.timeout,
    )
    models = parse_json("models", models_body)
    if models_status != 200:
        raise RuntimeError("models returned an unexpected success status")
    model_ids = set(item.get("id") for item in models.get("data", []))
    if not {"sxh-tts", "sxh-asr"}.issubset(model_ids):
        raise RuntimeError("public model list is missing sxh-tts or sxh-asr")

    tts_payload = json.dumps(
        {
            "model": "sxh-tts",
            "input": args.text,
            "voice": "alloy",
            "response_format": "mp3",
        },
        ensure_ascii=False,
    ).encode("utf-8")
    tts_headers = dict(auth_headers)
    tts_headers["Content-Type"] = "application/json"
    tts_status, tts_response_headers, audio = api_request(
        args.base_url,
        "/v1/audio/speech",
        method="POST",
        body=tts_payload,
        headers=tts_headers,
        timeout=args.timeout,
    )
    if tts_status != 200:
        raise RuntimeError("TTS returned an unexpected success status")
    if len(audio) <= 1000:
        raise RuntimeError("TTS response is too small to be valid audio")
    tts_request_id = require_request_id(tts_response_headers, "TTS")

    with tempfile.TemporaryDirectory(prefix="new-api-speech-validation-") as temp_dir:
        audio_path = os.path.join(temp_dir, "validation.mp3")
        with open(audio_path, "wb") as audio_file:
            audio_file.write(audio)
        with open(audio_path, "rb") as audio_file:
            asr_body, content_type = multipart_audio("sxh-asr", audio_file.read())

        asr_headers = dict(auth_headers)
        asr_headers["Content-Type"] = content_type
        asr_status, asr_response_headers, asr_payload = api_request(
            args.base_url,
            "/v1/audio/transcriptions",
            method="POST",
            body=asr_body,
            headers=asr_headers,
            timeout=args.timeout,
        )

    asr = parse_json("ASR", asr_payload)
    if asr_status != 200:
        raise RuntimeError("ASR returned an unexpected success status")
    asr_text = str(asr.get("text", "")).strip()
    if not asr_text:
        raise RuntimeError("ASR response text is empty")
    if float(asr.get("duration") or 0) <= 0:
        raise RuntimeError("ASR response duration is not positive")
    segments = asr.get("segments") or []
    if not segments:
        raise RuntimeError("ASR response has no segments")
    asr_request_id = require_request_id(asr_response_headers, "ASR")
    if asr_request_id == tts_request_id:
        raise RuntimeError("TTS and ASR returned the same request ID")

    log_rows = []
    for _ in range(10):
        log_rows = query_rows(
            log_db,
            """
            SELECT request_id, model_name, quota, prompt_tokens, completion_tokens,
                   use_time, is_stream, channel_id, token_id, content, other
            FROM logs
            WHERE type = 2 AND token_id = ? AND request_id IN (?, ?)
            ORDER BY id
            """,
            (args.token_id, tts_request_id, asr_request_id),
        )
        if len(log_rows) == 2:
            break
        time.sleep(1)
    if len(log_rows) != 2:
        raise RuntimeError("did not find both consume logs by request ID")

    rows_by_request = dict((row[0], row) for row in log_rows)
    expected_models = {
        tts_request_id: "sxh-tts",
        asr_request_id: "sxh-asr",
    }
    log_quota = 0
    for request_id, expected_model in expected_models.items():
        row = rows_by_request.get(request_id)
        if row is None or row[1] != expected_model:
            raise RuntimeError("{} consume log has the wrong model".format(expected_model))
        if row[2] <= 0:
            raise RuntimeError("{} consume log has non-positive quota".format(expected_model))
        if row[7] <= 0:
            raise RuntimeError("{} consume log has no channel".format(expected_model))
        audit_text = row[10] or ""
        if row[9] or args.text in audit_text or asr_text in audit_text:
            raise RuntimeError("{} consume log contains request text".format(expected_model))
        if not audit_text or audit_text == "{}":
            raise RuntimeError("{} consume log is missing audit data".format(expected_model))
        log_quota += row[2]

    after_token = query_rows(
        args.db,
        "SELECT remain_quota, used_quota FROM tokens WHERE id = ?",
        (args.token_id,),
    )[0]
    after_remain, after_used = after_token
    used_delta = after_used - before_used
    remain_delta = before_remain - after_remain
    if used_delta < log_quota:
        raise RuntimeError("Token used quota delta is smaller than consume logs")
    if remain_delta < log_quota:
        raise RuntimeError("Token remaining quota delta is smaller than consume logs")

    print("models_http={}".format(models_status))
    print("tts_http={}".format(tts_status))
    print("tts_audio_bytes={}".format(len(audio)))
    print("tts_audio_sha256={}".format(hashlib.sha256(audio).hexdigest()))
    print("asr_http={}".format(asr_status))
    print("asr_duration={}".format(asr.get("duration")))
    print("asr_segments={}".format(len(segments)))
    print("consume_log_quota={}".format(log_quota))
    print("token_used_delta={}".format(used_delta))
    print("token_remain_delta={}".format(remain_delta))
    token_delta_exact = used_delta == log_quota and remain_delta == log_quota
    print("token_delta_exact={}".format(str(token_delta_exact).lower()))
    print("log_content_private=true")
    print("validation=passed")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print("validation=failed", file=sys.stderr)
        print(str(exc), file=sys.stderr)
        sys.exit(1)
