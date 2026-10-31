package updatecheck

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	for _, tt := range []struct {
		name, current, body, want string
		status                    int
		wantErr                   bool
	}{
		{name: "newer", current: "0.9.0", body: `[{"tag_name":"v0.10.0"}]`, want: "v0.10.0"},
		{name: "major", current: "v0.99.99", body: `[{"tag_name":"v1.0.0"}]`, want: "v1.0.0"},
		{name: "patch", current: "v0.10.9", body: `[{"tag_name":"v0.10.10"}]`, want: "v0.10.10"},
		{name: "same", current: "v0.10.0", body: `[{"tag_name":"v0.10.0"}]`},
		{name: "older", current: "v1.0.0", body: `[{"tag_name":"v0.10.0"}]`},
		{name: "highest stable core", current: "v0.9.0", body: `[
			{"tag_name":"asana-sessions/v9.0.0"},
			{"tag_name":"v3.0.0","draft":true},
			{"tag_name":"v2.0.0","prerelease":true},
			{"tag_name":"v2.0.0-rc.1"},
			{"tag_name":"v0.09.1"},
			{"tag_name":"v0.11.0"},
			{"tag_name":"v0.10.0"}
		]`, want: "v0.11.0"},
		{name: "empty", current: "v1.0.0", body: `[]`},
		{name: "bad json", current: "v1.0.0", body: `{`, wantErr: true},
		{name: "rate limit", current: "v1.0.0", status: http.StatusForbidden, body: `{}`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("per_page") != "100" || r.URL.Query().Get("page") != "1" {
					t.Errorf("unexpected query: %s", r.URL.RawQuery)
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("Accept") != "application/vnd.github+json" {
					t.Errorf("unexpected headers: %v", r.Header)
				}
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()
			got, err := check(context.Background(), srv.Client(), srv.URL, tt.current)
			if got != tt.want || (err != nil) != tt.wantErr {
				t.Fatalf("check = %q, %v; want %q, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestCheckSkipsDevelopmentBuilds(t *testing.T) {
	for _, current := range []string{"", "dev", "(devel)", "v1.0.0-rc.1", "v1.0.0-1-gabcdef", "v01.0.0"} {
		got, err := check(context.Background(), nil, "invalid", current)
		if got != "" || err != nil {
			t.Errorf("check(%q) = %q, %v", current, got, err)
		}
	}
}

func TestCheckPaginatesPastCompanionReleases(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, "["+strings.Repeat(`{"tag_name":"asana-sessions/v1.0.0"},`, 99)+`{"tag_name":"v0.10.0"}]`)
		} else {
			fmt.Fprint(w, `[{"tag_name":"v0.11.0"}]`)
		}
	}))
	defer srv.Close()
	got, err := check(context.Background(), srv.Client(), srv.URL, "v0.9.0")
	if got != "v0.11.0" || err != nil || requests != 2 {
		t.Fatalf("check = %q, %v; requests = %d", got, err, requests)
	}
}

func TestCheckCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := check(ctx, http.DefaultClient, "https://example.invalid", "v1.0.0"); err == nil {
		t.Fatal("cancelled check succeeded")
	}
}

func TestCheckRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"tag_name":"v2.0.0","body":"`+strings.Repeat("x", 4<<20)+`"}]`)
	}))
	defer srv.Close()
	if got, err := check(context.Background(), srv.Client(), srv.URL, "v1.0.0"); got != "" || err == nil {
		t.Fatalf("oversized check = %q, %v", got, err)
	}
}
