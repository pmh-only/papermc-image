package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatestVersion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{"newest first", `{"26.1":["26.1.2","26.1.1"],"1.21":["1.21.11"]}`, "26.1.2"},
		{"snapshot", `{"3.4":["3.4.0-SNAPSHOT","3.4.0"]}`, "3.4.0-SNAPSHOT"},
		{"empty group", `{"26.1":[],"1.21":["1.21.11"]}`, "1.21.11"},
		{"no versions", `{}`, ""},
		{"wrong shape", `[]`, ""},
		{"missing", `null`, ""},
		{"invalid entry", `{"1.21":[42]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := latestVersion(json.RawMessage(tc.input))
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("latestVersion = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestFetchJSON(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		wantError bool
	}{
		{"build", http.StatusOK, `{"id":69,"channel":"STABLE","downloads":{"server:default":{"name":"paper.jar","url":"https://example.com/paper.jar"}}}`, false},
		{"API error", http.StatusNotFound, `{"ok":false,"message":"Unknown project"}`, true},
		{"invalid JSON", http.StatusOK, `not json`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("User-Agent") != userAgent {
					t.Errorf("missing identifying User-Agent")
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			var build buildResponse
			err := fetchJSON(server.URL, &build)
			if (err != nil) != tc.wantError {
				t.Fatalf("fetchJSON error = %v", err)
			}
			if !tc.wantError && (build.ID != 69 || build.Downloads["server:default"].Name != "paper.jar" || build.Downloads["server:default"].URL != "https://example.com/paper.jar") {
				t.Fatalf("unexpected build: %+v", build)
			}
		})
	}
}
