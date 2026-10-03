import unittest

from integrations.harbor.route import (
    GEMINI,
    OPENROUTER,
    SETTINGS,
    VLLM,
    RouteError,
    build_settings,
    release_asset,
    resolve_route,
)


class ResolveRouteTest(unittest.TestCase):
    def test_openrouter_keeps_vendor(self):
        provider, model = resolve_route("openrouter/deepseek/deepseek-v4-flash", None)
        self.assertEqual(provider, OPENROUTER)
        self.assertEqual(model, "deepseek/deepseek-v4-flash")

    def test_gemini(self):
        provider, model = resolve_route("gemini/gemini-2.5-flash", None)
        self.assertEqual(provider, GEMINI)
        self.assertEqual(model, "gemini-2.5-flash")

    def test_base_url_overrides_prefix(self):
        provider, model = resolve_route(
            "Qwen/Qwen3-8B", "http://host.docker.internal:8000/v1"
        )
        self.assertEqual(provider, VLLM)
        self.assertEqual(model, "Qwen/Qwen3-8B")

    def test_vllm_prefix_stripped_when_base_url_set(self):
        _, model = resolve_route("vllm/Qwen/Qwen3-8B", "http://127.0.0.1:8000/v1")
        self.assertEqual(model, "Qwen/Qwen3-8B")

    def test_unknown_model_is_an_error(self):
        with self.assertRaises(RouteError):
            resolve_route("deepseek-chat", None)

    def test_openrouter_without_vendor_is_an_error(self):
        with self.assertRaises(RouteError):
            resolve_route("openrouter/deepseek-chat", None)


class SettingsTest(unittest.TestCase):
    def test_benchmark_defaults_and_optional_key(self):
        settings = build_settings(
            VLLM,
            "qwen",
            base_url="http://host:8000/v1",
            temperature=0,
            context_limit=32768,
        )
        self.assertEqual(settings["sagittarius"]["maxToolRounds"], 0)
        self.assertEqual(settings["sagittarius"]["sessions"]["autoTitle"], "off")
        self.assertFalse(settings["sagittarius"]["update"]["autoCheck"])
        definition = settings["providers"]["custom"]["vllm"]
        self.assertNotIn("apiKeyEnvVar", definition)
        self.assertEqual(definition["baseUrl"], "http://host:8000/v1")
        self.assertEqual(
            settings["providers"]["vllm"]["models"]["qwen"]["temperature"], 0
        )

    def test_settings_path_matches_the_home_layout(self):
        self.assertTrue(SETTINGS.endswith("/.sagittarius/settings.json"))

    def test_release_asset_strips_v_and_maps_arch(self):
        url, name = release_asset("v0.21.0", "x86_64")
        self.assertEqual(name, "sagittarius_0.21.0_linux_amd64.tar.gz")
        self.assertTrue(url.endswith("/v0.21.0/" + name))


if __name__ == "__main__":
    unittest.main()
