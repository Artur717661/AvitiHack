package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Actor struct {
	ID   string `json:"user_id"`
	Team string `json:"team_id"`
	Role string `json:"role"`
}

type account struct {
	actor    Actor
	password string
}

var accounts = map[string]account{
	"demo-intern-1": {Actor{"demo-intern-1", "alpha", "intern"}, "alpha-demo-1"},
	"demo-intern-2": {Actor{"demo-intern-2", "beta", "intern"}, "beta-demo-2"},
	"demo-intern-3": {Actor{"demo-intern-3", "alpha", "intern"}, "alpha-demo-3"},
}

type app struct {
	store        *Store
	corporate    CorporateService
	router       IntentRouter
	modelName    string
	sessionKey   string
	secureCookie bool
}

type problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("write JSON: %v", err)
	}
}

func fail(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]problem{"error": {code, message}})
}

var errAccess = errors.New("access denied")
var errReason = errors.New("invalid reason")
var errMessage = errors.New("invalid message")
var errModel = errors.New("model unavailable")

func respondError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errAccess):
		fail(w, 403, "access_denied", "Вики Alpha доступна для запроса стажёрам Alpha. Обратись к наставнику своей команды.")
	case errors.Is(err, errNotFound):
		fail(w, 404, "draft_not_found", "Черновик недоступен по этой ссылке.")
	case errors.Is(err, errStale):
		fail(w, 409, "stale_draft", "Открой актуальную версию черновика.")
	case errors.Is(err, errReason):
		fail(w, 400, "invalid_reason", "Укажи причину длиной от 1 до 400 символов.")
	case errors.Is(err, errMessage):
		fail(w, 400, "invalid_message", "Напиши сообщение длиной от 1 до 500 символов.")
	case errors.Is(err, errCorporate):
		fail(w, 503, "corporate_unavailable", "Сервис заявок временно недоступен. Попробуй подтвердить ещё раз.")
	case errors.Is(err, errModel):
		fail(w, 503, "model_unavailable", "Помощник временно недоступен. Повтори вопрос или воспользуйся формой черновика.")
	default:
		log.Printf("request failed: %v", err)
		fail(w, 500, "internal_error", "Не удалось выполнить запрос. Попробуй ещё раз.")
	}
}

func readJSON(w http.ResponseWriter, r *http.Request, input any) bool {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		fail(w, 415, "content_type", "Передай данные в JSON.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16_384))
	decoder.DisallowUnknownFields()
	var raw json.RawMessage
	err := decoder.Decode(&raw)
	if err == nil {
		err = decoder.Decode(new(any))
		if err == io.EOF && len(raw) > 0 && raw[0] == '{' {
			fields := json.NewDecoder(strings.NewReader(string(raw)))
			fields.DisallowUnknownFields()
			err = fields.Decode(input)
			if err == nil {
				return true
			}
		}
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(w, 413, "payload_too_large", "Запрос превышает допустимый размер.")
		return false
	}
	fail(w, 400, "invalid_json", "Проверь поля запроса.")
	return false
}

func (a *app) sign(value string) string {
	sum := hmac.New(sha256.New, []byte(a.sessionKey))
	sum.Write([]byte(value))
	return hex.EncodeToString(sum.Sum(nil))
}

func (a *app) sessionValue(id string, expires time.Time) string {
	value := id + "." + strconv.FormatInt(expires.Unix(), 10)
	return value + "." + a.sign(value)
}

func (a *app) actorFor(r *http.Request) (Actor, bool) {
	cookie, err := r.Cookie("buddy_session")
	if err != nil {
		return Actor{}, false
	}
	content, signature, ok := strings.Cut(cookie.Value, ".")
	expiry, signature, ok2 := strings.Cut(signature, ".")
	if !ok || !ok2 || !hmac.Equal([]byte(signature), []byte(a.sign(content+"."+expiry))) {
		return Actor{}, false
	}
	expires, err := strconv.ParseInt(expiry, 10, 64)
	if err != nil || expires <= time.Now().Unix() {
		return Actor{}, false
	}
	user, ok := accounts[content]
	return user.actor, ok
}

func allowAlpha(actor Actor) error {
	if actor.Team != "alpha" || actor.Role != "intern" {
		return errAccess
	}
	return nil
}

func (a *app) protected(w http.ResponseWriter, r *http.Request) (Actor, bool) {
	actor, ok := a.actorFor(r)
	if !ok {
		fail(w, 401, "login_required", "Войди под тестовой учёткой.")
		return Actor{}, false
	}
	if err := allowAlpha(actor); err != nil {
		respondError(w, err)
		return Actor{}, false
	}
	return actor, true
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID       string `json:"user_id"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	user, ok := accounts[input.ID]
	if !ok || !hmac.Equal([]byte(input.Password), []byte(user.password)) {
		fail(w, 401, "invalid_credentials", "Проверь тестовый логин и пароль.")
		return
	}
	expires := time.Now().Add(12 * time.Hour)
	http.SetCookie(w, &http.Cookie{Name: "buddy_session", Value: a.sessionValue(user.actor.ID, expires), Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: a.secureCookie, Expires: expires})
	writeJSON(w, 200, user.actor)
}

func (a *app) prepare(ctx context.Context, actor Actor, reason string) (*Draft, error) {
	if err := allowAlpha(actor); err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 400 {
		return nil, errReason
	}
	return a.store.prepare(ctx, actor.ID, reason)
}

type ChatReply struct {
	Intent      string `json:"intent"`
	Text        string `json:"text"`
	Source      string `json:"source,omitempty"`
	Draft       *Draft `json:"draft"`
	NeedsReason bool   `json:"needs_reason,omitempty"`
}

func instruction(name string) string {
	raw, err := demoData.ReadFile("data/knowledge/" + name + ".md")
	if err != nil {
		return ""
	}
	_, body, ok := strings.Cut(string(raw), "\n---\n")
	if ok {
		return strings.TrimSpace(body)
	}
	return string(raw)
}

func (a *app) ask(ctx context.Context, actor Actor, message string) (ChatReply, error) {
	if err := allowAlpha(actor); err != nil {
		return ChatReply{}, err
	}
	message = strings.TrimSpace(message)
	if message == "" || utf8.RuneCountInString(message) > 500 {
		return ChatReply{}, errMessage
	}
	draft, err := a.store.current(ctx, actor.ID)
	if err != nil && !errors.Is(err, errNotFound) {
		return ChatReply{}, err
	}
	state := "not_started"
	if draft != nil {
		state = draft.Status
	}
	intent, err := a.router.Route(ctx, actor, message, state)
	if err != nil {
		return ChatReply{}, errModel
	}
	reply := ChatReply{Intent: intent, Draft: draft}
	switch intent {
	case "explain_access":
		reply.Text = "Чтобы запросить доступ к вики Alpha, подготовь черновик, укажи причину и подтверди отправку на странице. Заявку рассмотрит тестовый наставник."
		reply.Source = "demo_access_alpha_v1"
	case "request_access":
		reply.Text = "Укажи, зачем тебе доступ к вики. Я подготовлю черновик для проверки."
		reply.NeedsReason = draft == nil
		if draft != nil {
			reply.Text = "Твой черновик уже сохранён. " + draft.NextStep
		}
	case "request_status", "next_step":
		reply.Text = "У тебя ещё нет заявки. Следующий шаг - подготовить черновик."
		if draft != nil {
			reply.Text = draft.NextStep
			if draft.RequestID != nil {
				reply.Text = *draft.RequestID + " на рассмотрении. " + reply.Text
			}
		}
	case "out_of_scope":
		reply.Text = "Могу помочь с доступом к вики Alpha. По другим инструментам обратись к наставнику."
	default:
		reply.Intent = "clarify"
		reply.Text = "Уточни, что нужно: инструкция, черновик заявки или её статус."
		if draft != nil && draft.Status == awaiting {
			reply.Text = "Заявка ждёт твоего подтверждения на странице черновика."
		}
	}
	return reply, nil
}

func (a *app) routes(mcp http.Handler, static http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "model": a.modelName})
	})
	mux.HandleFunc("POST /api/demo/login", a.login)
	mux.HandleFunc("POST /api/demo/logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "buddy_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secureCookie, SameSite: http.SameSiteStrictMode})
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/session", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := a.actorFor(r)
		if !ok {
			fail(w, 401, "login_required", "Войди под тестовой учёткой.")
			return
		}
		writeJSON(w, 200, actor)
	})
	mux.HandleFunc("GET /api/knowledge/{source}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.protected(w, r); !ok {
			return
		}
		name := map[string]string{"demo_access_alpha_v1": "demo-access-alpha", "demo_intro_alpha_v1": "demo-intro-alpha"}[r.PathValue("source")]
		if name == "" {
			fail(w, 404, "source_not_found", "Инструкция недоступна.")
			return
		}
		writeJSON(w, 200, map[string]string{"text": instruction(name)})
	})
	mux.HandleFunc("POST /api/chat", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := a.protected(w, r)
		if !ok {
			return
		}
		var input struct {
			Message string `json:"message"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		reply, err := a.ask(r.Context(), actor, input.Message)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, reply)
	})
	mux.HandleFunc("POST /api/drafts", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := a.protected(w, r)
		if !ok {
			return
		}
		var input struct {
			Resource string `json:"resource_id"`
			Reason   string `json:"reason"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		if input.Resource != resourceID {
			respondError(w, errAccess)
			return
		}
		draft, err := a.prepare(r.Context(), actor, input.Reason)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 201, draft)
	})
	mux.HandleFunc("GET /api/drafts/current", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := a.protected(w, r)
		if !ok {
			return
		}
		draft, err := a.store.current(r.Context(), actor.ID)
		if err != nil && !errors.Is(err, errNotFound) {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, draft)
	})
	mux.HandleFunc("GET /api/drafts/{id}", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := a.protected(w, r)
		if !ok {
			return
		}
		draft, err := a.store.get(r.Context(), actor.ID, r.PathValue("id"))
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, draft)
	})
	mux.HandleFunc("POST /api/drafts/{id}/confirm", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := a.protected(w, r)
		if !ok {
			return
		}
		var input struct {
			Version int `json:"version"`
		}
		if !readJSON(w, r, &input) {
			return
		}
		if input.Version < 1 {
			fail(w, 400, "invalid_version", "Укажи версию черновика.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		draft, err := a.store.confirm(ctx, a.corporate, actor.ID, r.PathValue("id"), input.Version)
		if err != nil {
			respondError(w, err)
			return
		}
		writeJSON(w, 200, draft)
	})
	if mcp != nil {
		mux.Handle("/mcp", mcp)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		fail(w, 404, "not_found", "Такой операции нет.")
	})
	if static != nil {
		mux.Handle("/", static)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method == http.MethodPost {
			origin := r.Header.Get("Origin")
			parsed, err := url.Parse(origin)
			if origin != "" && (err != nil || parsed.Host != r.Host || (parsed.Scheme != "http" && parsed.Scheme != "https")) {
				fail(w, 403, "origin_denied", "Открой страницу AI-buddy и повтори запрос.")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
