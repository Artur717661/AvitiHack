package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func staticHandler(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/drafts/") {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			files.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

func buildServer() (*http.Server, func(), error) {
	addr := env("BUDDY_ADDR", "127.0.0.1:8080")
	webDir := env("BUDDY_WEB_DIR", "web/dist")
	index, err := os.Stat(filepath.Join(webDir, "index.html"))
	if err != nil || !index.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("фронтенд не собран в %s: выполни pnpm --dir web build", webDir)
	}
	dsn, driver := os.Getenv("DATABASE_URL"), "postgres"
	if dsn == "" {
		dsn, driver = env("BUDDY_SQLITE", "buddy.sqlite3"), "sqlite"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := openStore(ctx, driver, dsn)
	if err != nil {
		return nil, nil, err
	}
	var router IntentRouter = train(loadExamples())
	modelName := "local-classifier"
	if endpoint := os.Getenv("BUDDY_ROUTER_URL"); endpoint != "" {
		router = HTTPRouter{URL: endpoint, Key: os.Getenv("BUDDY_ROUTER_KEY")}
		modelName = "qwen-router"
	}
	publicURL := env("BUDDY_PUBLIC_URL", "http://127.0.0.1:8080")
	a := &app{store: store, corporate: CorporateMock{}, router: router, modelName: modelName,
		sessionKey: env("BUDDY_SESSION_KEY", randomID()), secureCookie: strings.HasPrefix(publicURL, "https://")}
	server := &http.Server{Addr: addr, Handler: a.routes(a.mcpHandler(os.Getenv("BUDDY_MCP_TOKEN"), publicURL), staticHandler(webDir)),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 12 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	return server, func() { store.db.Close() }, nil
}

func main() {
	server, closeStore, err := buildServer()
	if err != nil {
		log.Fatal(err)
	}
	defer closeStore()
	log.Printf("AI-buddy слушает %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
