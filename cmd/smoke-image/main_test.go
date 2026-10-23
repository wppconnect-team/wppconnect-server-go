package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestRestartRefreshesPublishedPort(t *testing.T) {
	paths := make(map[string]any)
	for i := range 50 {
		paths[string(rune('A'+i))] = map[string]any{}
	}
	receiver := func(w http.ResponseWriter, r *http.Request) {
		var data any
		switch r.URL.Path {
		case "/readyz":
			return
		case "/api/smoke/smoke-secret/generate-token":
			data = map[string]string{"token": "test-token"}
		case "/openapi.json":
			data = map[string]any{"openapi": "3.0.3", "paths": paths}
		case "/api/smoke/status-session":
			data = map[string]string{"status": "CLOSED"}
		case "/api/smoke/close-session":
			data = map[string]string{"status": "success"}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(data)
	}
	first := httptest.NewServer(http.HandlerFunc(receiver))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(receiver))
	defer second.Close()
	portLookups := 0
	restarted := false
	fake := func(args ...string) (string, error) {
		switch args[0] {
		case "port":
			portLookups++
			target := first.URL
			if restarted {
				target = second.URL
			}
			u, _ := url.Parse(target)
			return u.Host, nil
		case "inspect":
			if args[2] == "{{.State.Health.Status}}" {
				return "healthy", nil
			}
			return "0", nil
		case "start":
			restarted = true
			first.Close()
		case "exec":
			if args[1] == "-e" {
				return "", errors.New("unreachable healthcheck port")
			}
			if args[2] == "cat" {
				return `{"name":"smoke","restore":false}`, nil
			}
		}
		return "", nil
	}
	if err := runWithDocker("test-image", "linux/amd64", fake); err != nil {
		t.Fatal(err)
	}
	if portLookups != 2 {
		t.Fatalf("published port was resolved %d times; want before and after restart", portLookups)
	}
}
