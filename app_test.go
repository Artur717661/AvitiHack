package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fixture(t *testing.T) (*app, http.Handler) {
	t.Helper()
	driver, dsn := "sqlite", filepath.Join(t.TempDir(), "test.sqlite3")
	if pg := os.Getenv("BUDDY_TEST_POSTGRES"); pg != "" {
		admin, err := sql.Open("postgres", pg)
		if err != nil {
			t.Fatal(err)
		}
		schema := "test_" + randomID()
		if _, err := admin.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
			admin.Close()
			t.Fatal(err)
		}
		t.Cleanup(func() {
			defer admin.Close()
			if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
				t.Error(err)
			}
		})
		target, err := url.Parse(pg)
		if err != nil {
			t.Fatal(err)
		}
		query := target.Query()
		query.Set("search_path", schema)
		target.RawQuery = query.Encode()
		driver, dsn = "postgres", target.String()
	}
	store, err := openStore(t.Context(), driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.db.Close() })
	a := &app{store: store, corporate: CorporateMock{}, router: train(loadExamples()), sessionKey: "test-key", modelName: "local-classifier"}
	return a, a.routes(nil, nil)
}

func call(handler http.Handler, method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func loginAs(t *testing.T, h http.Handler, id string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"user_id": id, "password": accounts[id].password})
	res := call(h, "POST", "/api/demo/login", string(body), nil)
	check(t, res, 200)
	return res.Result().Cookies()[0]
}

func check(t *testing.T, res *httptest.ResponseRecorder, code int) {
	t.Helper()
	if res.Code != code {
		t.Fatalf("want %d, got %d: %s", code, res.Code, res.Body.String())
	}
}

func decode[T any](t *testing.T, res *httptest.ResponseRecorder) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err, res.Body.String())
	}
	return result
}

const draftBody = `{"resource_id":"team_space","reason":"Нужны инструкции"}`
const confirmBody = `{"version":1}`

func TestAlphaWorkflow(t *testing.T) {
	_, h := fixture(t)
	check(t, call(h, "GET", "/api/health", "", nil), 200)
	check(t, call(h, "GET", "/api/session", "", nil), 401)
	check(t, call(h, "POST", "/api/demo/login", `{"user_id":"x","password":"x"}`, nil), 401)
	cookie := loginAs(t, h, "demo-intern-1")
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal(cookie)
	}
	check(t, call(h, "GET", "/api/session", "", cookie), 200)
	check(t, call(h, "GET", "/api/drafts/current", "", cookie), 200)
	ask := func(message string) ChatReply {
		raw, _ := json.Marshal(map[string]string{"message": message})
		res := call(h, "POST", "/api/chat", string(raw), cookie)
		check(t, res, 200)
		return decode[ChatReply](t, res)
	}
	if res := ask("Как получить доступ к вики?"); res.Source != "demo_access_alpha_v1" {
		t.Fatal(res)
	}
	check(t, call(h, "GET", "/api/knowledge/demo_access_alpha_v1", "", cookie), 200)
	check(t, call(h, "GET", "/api/knowledge/demo_intro_alpha_v1", "", cookie), 200)
	check(t, call(h, "GET", "/api/knowledge/other", "", cookie), 404)
	if res := ask("Подготовь заявку на доступ"); !res.NeedsReason {
		t.Fatal(res)
	}
	if res := ask("Что с моей заявкой?"); res.Draft != nil || res.Intent != "request_status" {
		t.Fatal(res)
	}
	created := call(h, "POST", "/api/drafts", draftBody, cookie)
	check(t, created, 201)
	d := decode[Draft](t, created)
	path := "/api/drafts/" + d.ID
	if d.ID == "" || d.Status != awaiting || d.URL != "/drafts/"+d.ID || d.Version != 1 || d.RequestID != nil {
		t.Fatal(d)
	}
	d2 := decode[Draft](t, call(h, "POST", "/api/drafts", draftBody, cookie))
	if d2.ID != d.ID {
		t.Fatal(d2)
	}
	if res := ask("да"); res.Intent != "clarify" || res.Draft.RequestID != nil {
		t.Fatal(res)
	}
	if res := ask("Подготовь заявку на доступ"); res.NeedsReason || res.Draft.ID != d.ID {
		t.Fatal(res)
	}
	check(t, call(h, "POST", path+"/confirm", `{"version":2}`, cookie), 409)
	check(t, call(h, "POST", path+"/confirm", `{"version":0}`, cookie), 400)
	confirmed := call(h, "POST", path+"/confirm", confirmBody, cookie)
	check(t, confirmed, 200)
	result := decode[Draft](t, confirmed)
	if result.Status != pending || result.RequestID == nil {
		t.Fatal(result)
	}
	for _, response := range []*httptest.ResponseRecorder{
		call(h, "GET", path, "", cookie), call(h, "GET", "/api/drafts/current", "", cookie), call(h, "POST", path+"/confirm", confirmBody, cookie),
	} {
		check(t, response, 200)
		copy := decode[Draft](t, response)
		if copy.RequestID == nil || *copy.RequestID != *result.RequestID {
			t.Fatal(copy)
		}
	}
	if res := ask("Что дальше?"); res.Intent != "next_step" || res.Draft.RequestID == nil || !strings.Contains(res.Text, *result.RequestID) {
		t.Fatal(res)
	}
	check(t, call(h, "POST", "/api/demo/logout", "{}", cookie), 200)
}

func TestPermissionsAndInputs(t *testing.T) {
	a, h := fixture(t)
	alpha, beta, another := loginAs(t, h, "demo-intern-1"), loginAs(t, h, "demo-intern-2"), loginAs(t, h, "demo-intern-3")
	expired := &http.Cookie{Name: "buddy_session", Value: a.sessionValue("demo-intern-1", time.Now().Add(-time.Hour))}
	check(t, call(h, "GET", "/api/session", "", expired), 401)
	check(t, call(h, "GET", "/api/session", "", &http.Cookie{Name: "buddy_session", Value: "unknown.0.invalid"}), 401)
	d := decode[Draft](t, call(h, "POST", "/api/drafts", draftBody, alpha))
	path := "/api/drafts/" + d.ID
	for _, route := range []struct{ method, path, body string }{
		{"GET", path, ""}, {"GET", "/api/knowledge/demo_access_alpha_v1", ""}, {"GET", "/api/drafts/current", ""},
		{"POST", "/api/chat", `{"message":"Как получить доступ?"}`}, {"POST", "/api/drafts", draftBody}, {"POST", path + "/confirm", confirmBody},
	} {
		check(t, call(h, route.method, route.path, route.body, beta), 403)
	}
	check(t, call(h, "GET", path, "", another), 404)
	check(t, call(h, "POST", path+"/confirm", confirmBody, another), 404)
	check(t, call(h, "POST", path+"/confirm", confirmBody, nil), 401)
	check(t, call(h, "POST", "/api/drafts", `{"resource_id":"vpn","reason":"test"}`, alpha), 403)
	check(t, call(h, "POST", "/api/drafts", `{"resource_id":"team_space","reason":" "}`, alpha), 400)
	check(t, call(h, "POST", "/api/chat", `{"message":""}`, alpha), 400)
	for _, body := range []string{"{}", "{", confirmBody + "{}", `{"version":true}`, `{"version":1,"role":"admin"}`, "  "} {
		check(t, call(h, "POST", path+"/confirm", body, alpha), 400)
	}
	check(t, call(h, "POST", path+"/confirm", "", alpha), 415)
	check(t, call(h, "POST", path+"/confirm", `{"version":1,"extra":"`+strings.Repeat("x", 17_000)+`"}`, alpha), 413)
	req := httptest.NewRequest("POST", path+"/confirm", strings.NewReader(confirmBody))
	req.Header.Set("Origin", "https://elsewhere.invalid")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	check(t, res, 403)
	check(t, call(h, "GET", "/api/missing", "", alpha), 404)
}

type failedCorporate struct{}

func (failedCorporate) Create(ctx context.Context, tx *sql.Tx, id string) (string, string, error) {
	if _, _, err := (CorporateMock{}).Create(ctx, tx, id); err != nil {
		return "", "", err
	}
	return "", "", errors.New("simulated fault after create")
}

type failedRouter struct{}

func (failedRouter) Route(context.Context, Actor, string, string) (string, error) {
	return "", errors.New("model unavailable")
}

func TestRollbackAndConcurrentRetry(t *testing.T) {
	a, h := fixture(t)
	cookie := loginAs(t, h, "demo-intern-1")
	d := decode[Draft](t, call(h, "POST", "/api/drafts", draftBody, cookie))
	path := "/api/drafts/" + d.ID
	a.corporate = failedCorporate{}
	check(t, call(h, "POST", path+"/confirm", confirmBody, cookie), 503)
	if got := decode[Draft](t, call(h, "GET", path, "", cookie)); got.Status != awaiting || got.RequestID != nil {
		t.Fatal(got)
	}
	var count int
	if err := a.store.db.QueryRow("SELECT COUNT(*) FROM buddy_corporate_requests").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	a.corporate = CorporateMock{}
	var replies [2]*httptest.ResponseRecorder
	var wg sync.WaitGroup
	for i := range replies {
		wg.Go(func() { replies[i] = call(h, "POST", path+"/confirm", confirmBody, cookie) })
	}
	wg.Wait()
	check(t, replies[0], 200)
	check(t, replies[1], 200)
	if !bytes.Equal(replies[0].Body.Bytes(), replies[1].Body.Bytes()) {
		t.Fatal("different confirmations")
	}
	if err := a.store.db.QueryRow("SELECT COUNT(*) FROM buddy_corporate_requests").Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	a.router = failedRouter{}
	check(t, call(h, "POST", "/api/chat", `{"message":"Как получить доступ?"}`, cookie), 503)
}

type transportWithToken struct{ token string }

func (transport transportWithToken) RoundTrip(r *http.Request) (*http.Response, error) {
	request := r.Clone(r.Context())
	request.Header.Set("Authorization", "Bearer "+transport.token)
	return http.DefaultTransport.RoundTrip(request)
}

func TestMCPUsesSameDraft(t *testing.T) {
	a, _ := fixture(t)
	api := a.routes(a.mcpHandler("test-mcp", "http://public.example"), nil)
	web := httptest.NewServer(api)
	t.Cleanup(web.Close)
	res, err := http.Get(web.URL + "/mcp")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatal(res.StatusCode)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: web.URL + "/mcp", HTTPClient: &http.Client{Transport: transportWithToken{"test-mcp"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil || len(tools.Tools) != 3 {
		t.Fatal(tools, err)
	}
	for _, tool := range []struct {
		name string
		args map[string]string
	}{
		{"ask", map[string]string{"message": "Как получить доступ?"}},
		{"request_status", map[string]string{}},
		{"prepare_draft", map[string]string{"reason": "Нужна документация"}},
	} {
		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tool.name, Arguments: tool.args})
		if err != nil || result.IsError {
			t.Fatal(result, err)
		}
		if tool.name == "prepare_draft" {
			draft := result.StructuredContent.(map[string]any)
			if !strings.HasPrefix(draft["draft_url"].(string), "http://public.example/drafts/") {
				t.Fatal(draft)
			}
		}
	}
	cookie := loginAs(t, api, "demo-intern-1")
	d := decode[Draft](t, call(api, "GET", "/api/drafts/current", "", cookie))
	confirmed := decode[Draft](t, call(api, "POST", "/api/drafts/"+d.ID+"/confirm", confirmBody, cookie))
	got, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "request_status", Arguments: struct{}{}})
	if err != nil || got.IsError {
		t.Fatal(got, err)
	}
	jsonResult, _ := json.Marshal(got.StructuredContent)
	if confirmed.RequestID == nil || !bytes.Contains(jsonResult, []byte(*confirmed.RequestID)) {
		t.Fatal(string(jsonResult))
	}
}
