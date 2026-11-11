package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/code-corhuila/multi-tour-worker-cash-close/internal/config"
)

func env(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(k string) string { return m[k] }
}

var required = []string{
	"SERVICE_TOKEN", "secret-token-value",
	"CASH_REPORTING_BASE_URL", "http://cash-reporting-api:8085/",
}

func with(extra ...string) []string { return append(append([]string{}, required...), extra...) }

// AC-WK-08.9: the declared defaults, and the shared runtime variables travel with them.
func TestLoadClose_applies_the_declared_defaults(t *testing.T) {
	got, err := config.LoadClose(env(required...))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Interval != 15*time.Minute || got.HealthAddr != ":8080" || got.Runtime.BatchSize != 100 {
		t.Errorf("defaults = %+v", got)
	}
	if got.CashReportingURL != "http://cash-reporting-api:8085" {
		t.Errorf("url = %q, the trailing slash should go", got.CashReportingURL)
	}
}

func TestLoadClose_reads_its_own_variables(t *testing.T) {
	got, err := config.LoadClose(env(with("CLOSE_INTERVAL", "1m", "HEALTH_ADDR", ":9090")...))

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Interval != time.Minute || got.HealthAddr != ":9090" {
		t.Errorf("got %+v", got)
	}
}

// AC-WK-08.9: a missing or malformed variable stops the worker with its name, never its value.
func TestLoadClose_names_the_bad_variable_and_never_prints_a_value(t *testing.T) {
	cases := map[string][]string{
		"SERVICE_TOKEN":           {"CASH_REPORTING_BASE_URL", "http://cash-reporting-api:8085"},
		"CASH_REPORTING_BASE_URL": {"SERVICE_TOKEN", "secret-token-value"},
		"CLOSE_INTERVAL":          with("CLOSE_INTERVAL", "soon"),
	}
	for name, pairs := range cases {
		_, err := config.LoadClose(env(pairs...))

		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: error = %v, want one that names it", name, err)
		}
		if err != nil && strings.Contains(err.Error(), "secret-token-value") {
			t.Errorf("%s: the error prints the token", name)
		}
	}
}

func TestLoadClose_refuses_a_base_url_with_more_than_scheme_host_and_port(t *testing.T) {
	for _, bad := range []string{"cash-reporting-api:8085", "ftp://x:1", "http://u:p@x:1", "http://x:1/api", "http://x:1?a=b", ""} {
		if _, err := config.LoadClose(env("SERVICE_TOKEN", "t", "CASH_REPORTING_BASE_URL", bad)); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
