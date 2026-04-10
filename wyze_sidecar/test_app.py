import unittest
from datetime import datetime, timezone

import wyze_sidecar.app as app
from wyze_sidecar.app import build_login_kwargs, normalize_scale_record


class FakeRecord:
    def __init__(self, **kwargs):
        self.__dict__.update(kwargs)


class SidecarHelpersTest(unittest.TestCase):
    def test_build_login_kwargs_only_includes_supported_fields(self):
        def login(email, password, api_key=None):
            return None

        kwargs = build_login_kwargs(
            login,
            email="athlete@example.com",
            password="secret",
            key_id="kid",
            api_key="api",
            totp_key="totp",
        )

        self.assertEqual(
            kwargs,
            {
                "email": "athlete@example.com",
                "password": "secret",
                "api_key": "api",
            },
        )

    def test_normalize_scale_record_maps_needed_fields(self):
        rec = FakeRecord(
            id="abc123",
            measure_ts=1712560462000,
            device_mac="device-1",
            _weight=77.4,
            body_fat=18.2,
            muscle=36.8,
            body_water=55.1,
            bmr=1684,
        )

        out = normalize_scale_record(rec)

        self.assertEqual(out["external_id"], "wyze:scale_record:abc123")
        self.assertEqual(out["device_id"], "device-1")
        self.assertEqual(out["weight_kg"], 77.4)
        self.assertEqual(out["body_fat_pct"], 18.2)
        self.assertEqual(out["muscle_mass_kg"], 36.8)
        self.assertEqual(out["body_water_pct"], 55.1)
        self.assertEqual(out["bmr_kcal"], 1684.0)

    def test_normalize_scale_record_falls_back_to_stable_hash(self):
        rec = FakeRecord(
            measured_time=datetime(2026, 4, 8, 7, 14, 22, tzinfo=timezone.utc),
            weight=77.4,
        )

        out1 = normalize_scale_record(rec)
        out2 = normalize_scale_record(rec)

        self.assertTrue(out1["external_id"].startswith("wyze:scale_record:"))
        self.assertEqual(out1["external_id"], out2["external_id"])

    def test_query_scale_records_filters_requested_window(self):
        original_client_symbol = None

        class FakeScales:
            def get_records(self, start_time, end_time):
                return [
                    FakeRecord(id="old", measure_ts=datetime(2026, 3, 31, 23, 0, tzinfo=timezone.utc), _weight=70),
                    FakeRecord(id="in", measure_ts=datetime(2026, 4, 2, 12, 0, tzinfo=timezone.utc), _weight=71),
                    FakeRecord(id="new", measure_ts=datetime(2026, 4, 10, 0, 0, tzinfo=timezone.utc), _weight=72),
                ]

        class FakeClient:
            def __init__(self, *args, **kwargs):
                self.scales = FakeScales()

            def login(self, **kwargs):
                return {}

        import sys

        module = sys.modules.get("wyze_sdk")
        if module is None:
            import types

            module = types.SimpleNamespace(Client=FakeClient)
            sys.modules["wyze_sdk"] = module
            injected = True
        else:
            original_client_symbol = getattr(module, "Client", None)
            setattr(module, "Client", FakeClient)
            injected = False

        try:
            app.os.environ["WYZE_EMAIL"] = "athlete@example.com"
            app.os.environ["WYZE_PASSWORD"] = "secret"
            app.os.environ["WYZE_KEY_ID"] = "kid"
            app.os.environ["WYZE_API_KEY"] = "api"
            records = app.query_scale_records(
                datetime(2026, 4, 1, 0, 0, tzinfo=timezone.utc),
                datetime(2026, 4, 9, 23, 59, 59, tzinfo=timezone.utc),
            )
        finally:
            if injected:
                del sys.modules["wyze_sdk"]
            else:
                setattr(module, "Client", original_client_symbol)

        self.assertEqual(len(records), 1)
        self.assertEqual(records[0]["external_id"], "wyze:scale_record:in")


if __name__ == "__main__":
    unittest.main()
