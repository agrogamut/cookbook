package main

import (
	"strings"
	"testing"
)

func TestFixtureTargetsBeforeConnection(t *testing.T) {
	values := map[string]string{}
	for key, database := range map[string]string{"TEST_DATABASE_URL": "ci_legacy", "CASE_SOURCE_TEST_DATABASE_URL": "ci_cases", "PORTAL_TEST_DATABASE_URL": "ci_portal"} {
		values[key] = "postgres://ci_fixture:local@127.0.0.1:5432/" + database + "?sslmode=disable"
	}
	get := func(key string) string { return values[key] }
	if items, err := targets(get); err != nil || len(items) != 3 {
		t.Fatalf("valid isolated fixtures: %v", err)
	}
	for _, key := range []string{"TEST_DATABASE_URL", "CASE_SOURCE_TEST_DATABASE_URL", "PORTAL_TEST_DATABASE_URL"} {
		t.Run(key, func(t *testing.T) {
			original := values[key]
			defer func() { values[key] = original }()
			for _, value := range []string{"", strings.Replace(original, "127.0.0.1", "remote.example", 1), strings.Replace(original, "ci_fixture", "other", 1), strings.Replace(original, "/ci_", "/other_", 1), strings.Replace(original, "127.0.0.1", "127.0.0.1,localhost", 1)} {
				values[key] = value
				if items, err := targets(get); err == nil || items != nil {
					t.Fatal("invalid target returned a runnable fixture list")
				}
			}
		})
	}
	values["GEMINI_API_KEY"] = "present"
	if _, err := targets(get); err == nil {
		t.Fatal("live credential accepted")
	}
}
