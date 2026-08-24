package config

import "testing"

func webSettings(cfg *SagittariusWebConfig) *Settings {
	return &Settings{Sagittarius: &SagittariusSettings{Web: cfg}}
}

func TestWebMaxFetchBytes(t *testing.T) {
	zero, negative, custom := 0, -10, 4096

	for _, tc := range []struct {
		name        string
		global      *Settings
		project     *Settings
		directFetch bool
		want        int
	}{
		{name: "unset uses the default", want: DefaultMaxFetchBytes},
		{
			name:        "unset in direct mode uses the experimental default",
			directFetch: true,
			want:        DefaultMaxExperimentalFetchBytes,
		},
		{
			name:   "explicit global value wins over the default",
			global: webSettings(&SagittariusWebConfig{MaxFetchBytes: &custom}),
			want:   custom,
		},
		{
			name:    "project wins over global",
			global:  webSettings(&SagittariusWebConfig{MaxFetchBytes: &zero}),
			project: webSettings(&SagittariusWebConfig{MaxFetchBytes: &custom}),
			want:    custom,
		},
		{
			// A zero or negative cap would mean "download nothing", which is never
			// what a user intends by writing it into settings.
			name:   "zero falls through to the default",
			global: webSettings(&SagittariusWebConfig{MaxFetchBytes: &zero}),
			want:   DefaultMaxFetchBytes,
		},
		{
			name:   "negative falls through to the default",
			global: webSettings(&SagittariusWebConfig{MaxFetchBytes: &negative}),
			want:   DefaultMaxFetchBytes,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WebMaxFetchBytes(tc.global, tc.project, tc.directFetch); got != tc.want {
				t.Errorf("WebMaxFetchBytes() = %d; want %d", got, tc.want)
			}
		})
	}
}

func TestWebDirectFetch(t *testing.T) {
	on, off := true, false

	if WebDirectFetch(nil, nil) {
		t.Error("directWebFetch should default to false")
	}
	if !WebDirectFetch(webSettings(&SagittariusWebConfig{DirectWebFetch: &on}), nil) {
		t.Error("an explicit global true should be honored")
	}
	if WebDirectFetch(
		webSettings(&SagittariusWebConfig{DirectWebFetch: &on}),
		webSettings(&SagittariusWebConfig{DirectWebFetch: &off}),
	) {
		t.Error("project should win over global")
	}
}

func TestWebSearchEnabled(t *testing.T) {
	on, off := true, false

	if !WebSearchEnabled(nil, nil) {
		t.Error("search should default to true when unset")
	}
	if !WebSearchEnabled(webSettings(&SagittariusWebConfig{SearchEnabled: &on}), nil) {
		t.Error("an explicit true should be honored")
	}
	if WebSearchEnabled(webSettings(&SagittariusWebConfig{SearchEnabled: &off}), nil) {
		t.Error("an explicit false should be honored")
	}
	if WebSearchEnabled(
		webSettings(&SagittariusWebConfig{SearchEnabled: &on}),
		webSettings(&SagittariusWebConfig{SearchEnabled: &off}),
	) {
		t.Error("project should win over global")
	}
}

func TestWebUtilityModel(t *testing.T) {
	if got := WebUtilityModel(nil, nil); got != "" {
		t.Errorf("unset utilityModel = %q; want empty so the provider picks its default", got)
	}
	global := webSettings(&SagittariusWebConfig{UtilityModel: "gemini-2.5-flash"})
	if got := WebUtilityModel(global, nil); got != "gemini-2.5-flash" {
		t.Errorf("global utilityModel = %q; want gemini-2.5-flash", got)
	}
	project := webSettings(&SagittariusWebConfig{UtilityModel: "gemini-2.5-pro"})
	if got := WebUtilityModel(global, project); got != "gemini-2.5-pro" {
		t.Errorf("project utilityModel = %q; want gemini-2.5-pro", got)
	}
}
