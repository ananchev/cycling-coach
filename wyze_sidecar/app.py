import hashlib
import inspect
import json
import os
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, HTTPServer


def build_login_kwargs(login_fn, email, password, key_id=None, api_key=None, totp_key=None):
    params = inspect.signature(login_fn).parameters
    kwargs = {}
    if "email" in params:
        kwargs["email"] = email
    if "username" in params:
        kwargs["username"] = email
    if "password" in params:
        kwargs["password"] = password
    if key_id and "key_id" in params:
        kwargs["key_id"] = key_id
    if api_key and "api_key" in params:
        kwargs["api_key"] = api_key
    if totp_key and "totp_key" in params:
        kwargs["totp_key"] = totp_key
    return kwargs


def _coerce_float(value):
    if value is None:
        return None
    return float(value)


def _coerce_datetime(value):
    if value is None:
        return None
    if isinstance(value, datetime):
        return value.astimezone(timezone.utc)
    if isinstance(value, (int, float)):
        # Handle both seconds and milliseconds.
        if value > 10_000_000_000:
            value = value / 1000.0
        return datetime.fromtimestamp(value, tz=timezone.utc)
    if isinstance(value, str):
        text = value.strip()
        if text.endswith("Z"):
            text = text[:-1] + "+00:00"
        return datetime.fromisoformat(text).astimezone(timezone.utc)
    raise ValueError(f"unsupported datetime value: {value!r}")


def _record_external_id(record, measured_at):
    for name in ("record_id", "id", "unique_id", "measurement_id"):
        value = getattr(record, name, None)
        if value not in (None, ""):
            return f"wyze:scale_record:{value}"

    fingerprint = json.dumps(
        {
            "measured_at": measured_at.isoformat(),
            "weight": _coerce_float(getattr(record, "weight", None)),
            "body_fat": _coerce_float(getattr(record, "body_fat", None)),
            "muscle": _coerce_float(getattr(record, "muscle", None)),
            "body_water": _coerce_float(getattr(record, "body_water", None)),
            "bmr": _coerce_float(getattr(record, "bmr", None)),
        },
        sort_keys=True,
    ).encode("utf-8")
    digest = hashlib.sha1(fingerprint).hexdigest()
    return f"wyze:scale_record:{digest}"


def normalize_scale_record(record):
    measured_at = _coerce_datetime(
        getattr(record, "measure_ts", None)
        or getattr(record, "measure_time", None)
        or getattr(record, "measured_time", None)
        or getattr(record, "timestamp", None)
        or getattr(record, "ts", None)
        or getattr(record, "created_at", None)
    )
    if measured_at is None:
        raise ValueError("scale record missing measured timestamp")

    device_id = (
        getattr(record, "device_mac", None)
        or getattr(record, "device_id", None)
        or getattr(record, "mac", None)
    )

    weight_kg = _coerce_float(
        getattr(record, "_weight", None)
        or getattr(record, "weight_kg", None)
        or getattr(record, "weight", None)
    )

    return {
        "external_id": _record_external_id(record, measured_at),
        "measured_at": measured_at.isoformat().replace("+00:00", "Z"),
        "device_id": device_id,
        "weight_kg": weight_kg,
        "body_fat_pct": _coerce_float(getattr(record, "body_fat", None)),
        "muscle_mass_kg": _coerce_float(getattr(record, "muscle", None)),
        "body_water_pct": _coerce_float(getattr(record, "body_water", None)),
        "bmr_kcal": _coerce_float(getattr(record, "bmr", None)),
        "raw_source": "wyze",
    }


def query_scale_records(from_dt, to_dt):
    from wyze_sdk import Client

    email = os.environ["WYZE_EMAIL"]
    password = os.environ["WYZE_PASSWORD"]
    key_id = os.getenv("WYZE_KEY_ID")
    api_key = os.getenv("WYZE_API_KEY")
    totp_key = os.getenv("WYZE_TOTP_KEY")

    try:
        client = Client()
    except TypeError:
        ctor_kwargs = {}
        ctor_params = inspect.signature(Client).parameters
        if "email" in ctor_params:
            ctor_kwargs["email"] = email
        if "password" in ctor_params:
            ctor_kwargs["password"] = password
        if key_id and "key_id" in ctor_params:
            ctor_kwargs["key_id"] = key_id
        if api_key and "api_key" in ctor_params:
            ctor_kwargs["api_key"] = api_key
        if totp_key and "totp_key" in ctor_params:
            ctor_kwargs["totp_key"] = totp_key
        client = Client(**ctor_kwargs)

    login_fn = getattr(client, "login", None)
    if callable(login_fn):
        login_fn(**build_login_kwargs(login_fn, email, password, key_id, api_key, totp_key))

    records = client.scales.get_records(start_time=from_dt, end_time=to_dt)
    normalized = []
    for record in records:
        item = normalize_scale_record(record)
        measured_at = _coerce_datetime(item["measured_at"])
        if measured_at < from_dt or measured_at > to_dt:
            continue
        normalized.append(item)
    return normalized


class Handler(BaseHTTPRequestHandler):
    def _write_json(self, status_code, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status_code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health":
            self._write_json(200, {"status": "ok"})
            return
        self._write_json(404, {"error": {"code": "not_found", "message": "Not found"}})

    def do_POST(self):
        if self.path != "/v1/scale-records/query":
            self._write_json(404, {"error": {"code": "not_found", "message": "Not found"}})
            return

        try:
            length = int(self.headers.get("Content-Length", "0"))
            payload = json.loads(self.rfile.read(length) or b"{}")
            from_dt = _coerce_datetime(payload.get("from"))
            to_dt = _coerce_datetime(payload.get("to"))
            if from_dt is None or to_dt is None:
                raise ValueError("from and to are required")
            records = query_scale_records(from_dt, to_dt)
            self._write_json(
                200,
                {
                    "records": records,
                    "meta": {
                        "from": from_dt.isoformat().replace("+00:00", "Z"),
                        "to": to_dt.isoformat().replace("+00:00", "Z"),
                        "count": len(records),
                    },
                },
            )
        except ValueError as exc:
            self._write_json(400, {"error": {"code": "invalid_time_range", "message": str(exc)}})
        except Exception as exc:  # noqa: BLE001
            self._write_json(502, {"error": {"code": "wyze_unavailable", "message": str(exc)}})


def main():
    port = int(os.getenv("PORT", "8090"))
    server = HTTPServer(("0.0.0.0", port), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
