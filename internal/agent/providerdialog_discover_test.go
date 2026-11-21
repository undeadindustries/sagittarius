package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/undeadindustries/sagittarius/internal/config"
)

func TestProviderDialogDiscoverModelsSurfacesErrors(t *testing.T) {
	t.Run("surfaces HTTP 401 message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
		}))
		t.Cleanup(srv.Close)

		app, _ := scopeTestApp(t)
		if err := app.persistGlobal(func(s *config.Settings) error {
			s.Providers.Custom[scopeTestProvider] = config.CustomProviderDefinition{
				DisplayName: "Compat",
				BaseURL:     srv.URL + "/v1/chat/completions",
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		d := &providerDialogDeps{app: app}
		_, err := d.DiscoverModels(t.Context(), scopeTestProvider)
		if err == nil {
			t.Fatal("expected error for HTTP 401")
		}
		if !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), "invalid x-api-key") {
			t.Fatalf("err = %q, want HTTP 401 and invalid x-api-key", err.Error())
		}
	})

	t.Run("hints workspace id when key is not scoped to a workspace", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"This API key is not scoped to a workspace"}}`))
		}))
		t.Cleanup(srv.Close)

		app, _ := scopeTestApp(t)
		if err := app.persistGlobal(func(s *config.Settings) error {
			s.Providers.Custom[scopeTestProvider] = config.CustomProviderDefinition{
				DisplayName: "Compat",
				BaseURL:     srv.URL + "/v1/chat/completions",
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		d := &providerDialogDeps{app: app}
		_, err := d.DiscoverModels(t.Context(), scopeTestProvider)
		if err == nil {
			t.Fatal("expected error for HTTP 400")
		}
		if !strings.Contains(err.Error(), "wrkspc_") {
			t.Fatalf("err = %q, want workspace id hint", err.Error())
		}
	})
}
