package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPRouterContract(t *testing.T) {
	for _, tc := range []struct {
		response string
		status   int
		good     bool
	}{
		{`{"intent":"next_step"}`, 200, true},
		{`{"intent":"next_step","request_id":"invented"}`, 200, false},
		{`{"intent":"submit"}`, 200, false},
		{`{"intent":"next_step"}{}`, 200, false},
		{strings.Repeat(" ", 4097), 200, false},
		{`{}`, 503, false},
	} {
		t.Run(fmt.Sprintf("%d/%d", tc.status, len(tc.response)), func(t *testing.T) {
			actor := accounts["demo-intern-1"].actor
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Message string `json:"message"`
					State   string `json:"workflow_state"`
					Actor   Actor  `json:"actor_context"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.Header.Get("Authorization") != "Bearer test-key" || body.Message != "Что дальше?" || body.State != pending || body.Actor != actor {
					t.Error(body)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.response)
			}))
			defer service.Close()
			intent, err := (HTTPRouter{service.URL, "test-key"}).Route(t.Context(), actor, "Что дальше?", pending)
			if tc.good && (err != nil || intent != "next_step") || !tc.good && err == nil {
				t.Fatal(intent, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (HTTPRouter{URL: "http://127.0.0.1:1"}).Route(ctx, Actor{}, "msg", "not_started"); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := (HTTPRouter{URL: ":///"}).Route(t.Context(), Actor{}, "msg", "not_started"); err == nil {
		t.Fatal("invalid URL accepted")
	}
}

func TestLocalClassifierQuality(t *testing.T) {
	data := loadExamples()
	model := train(data)
	for _, s := range []string{"dev", "test"} {
		correct, total := 0, 0
		for _, ex := range data {
			if ex.Split != s {
				continue
			}
			total++
			if model.classify(ex.Message, ex.State) == ex.Target.Intent {
				correct++
			}
		}
		if total == 0 || (s == "dev" && correct*100 < total*65) {
			t.Fatal(s, correct, total)
		}
		t.Logf("Synthetic %s: %d/%d (%.1f%%)", s, correct, total, float64(correct)*100/float64(total))
	}
	for input, expected := range map[string]string{
		"...": "clarify", "??а??": "clarify", "Что дальше?": "next_step", "да": "clarify", "проверь статус": "request_status",
	} {
		if actual := model.classify(input, awaiting); actual != expected {
			t.Errorf("%q: %s", input, actual)
		}
	}
}

func TestPersistenceAndStaticRoutes(t *testing.T) {
	temp := t.TempDir()
	if err := os.WriteFile(filepath.Join(temp, "index.html"), []byte("AI-buddy interface"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(temp, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(temp, "assets", "app.css"), []byte("body{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "")
	t.Setenv("BUDDY_SQLITE", filepath.Join(temp, "session.sqlite3"))
	t.Setenv("BUDDY_WEB_DIR", temp)
	t.Setenv("BUDDY_SESSION_KEY", "persistent-key")
	t.Setenv("BUDDY_ROUTER_URL", "")
	server, closeStore, err := buildServer()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/drafts/example", "/assets/app.css"} {
		check(t, call(server.Handler, "GET", path, "", nil), 200)
	}
	check(t, call(server.Handler, "GET", "/file.out", "", nil), 404)
	cookie := loginAs(t, server.Handler, "demo-intern-1")
	d := decode[Draft](t, call(server.Handler, "POST", "/api/drafts", draftBody, cookie))
	confirmed := decode[Draft](t, call(server.Handler, "POST", "/api/drafts/"+d.ID+"/confirm", confirmBody, cookie))
	closeStore()
	t.Setenv("BUDDY_ROUTER_URL", "http://127.0.0.1:8091/internal/v1/ml/route")
	restarted, closeAgain, err := buildServer()
	if err != nil {
		t.Fatal(err)
	}
	defer closeAgain()
	if !strings.Contains(call(restarted.Handler, "GET", "/api/health", "", nil).Body.String(), "qwen-router") {
		t.Fatal("mode not reported")
	}
	result := decode[Draft](t, call(restarted.Handler, "GET", "/api/drafts/current", "", cookie))
	if confirmed.RequestID == nil || result.RequestID == nil || *confirmed.RequestID != *result.RequestID {
		t.Fatal(result)
	}
}

func TestStartupRequiresBuiltFrontend(t *testing.T) {
	t.Setenv("BUDDY_WEB_DIR", t.TempDir())
	server, closeStore, err := buildServer()
	if err == nil {
		closeStore()
		t.Fatalf("started without frontend: %v", server)
	}
	if !strings.Contains(err.Error(), "pnpm --dir web build") {
		t.Fatal(err)
	}
}
