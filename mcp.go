package main

import (
	"context"
	"crypto/hmac"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (a *app) mcpHandler(token, publicURL string) http.Handler {
	actor := accounts["demo-intern-1"].actor
	server := mcp.NewServer(&mcp.Implementation{Name: "ai-buddy-alpha", Version: "1.0.0"}, nil)
	toPublicURL := func(draft *Draft) {
		if draft != nil {
			draft.URL = strings.TrimRight(publicURL, "/") + draft.URL
		}
	}
	type askInput struct {
		Message string `json:"message" jsonschema:"Вопрос или просьба стажёра"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "ask", Description: "Объяснить доступ к вики Alpha, узнать статус или следующий шаг."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input askInput) (*mcp.CallToolResult, ChatReply, error) {
			reply, err := a.ask(ctx, actor, input.Message)
			toPublicURL(reply.Draft)
			return nil, reply, err
		})
	type prepareInput struct {
		Reason string `json:"reason" jsonschema:"Причина доступа, указанная стажёром"`
	}
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_draft", Description: "Подготовить черновик по явной просьбе стажёра и вернуть ссылку на страницу подтверждения."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input prepareInput) (*mcp.CallToolResult, *Draft, error) {
			draft, err := a.prepare(ctx, actor, input.Reason)
			toPublicURL(draft)
			return nil, draft, err
		})
	mcp.AddTool(server, &mcp.Tool{Name: "request_status", Description: "Прочитать сохранённый статус заявки текущего стажёра."},
		func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ChatReply, error) {
			reply := ChatReply{Intent: "request_status", Text: "У тебя ещё нет заявки."}
			draft, err := a.store.current(ctx, actor.ID)
			if err == errNotFound {
				return nil, reply, nil
			}
			if err != nil {
				return nil, ChatReply{}, err
			}
			toPublicURL(draft)
			reply.Draft, reply.Text = draft, draft.NextStep
			return nil, reply, nil
		})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" || !hmac.Equal([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			fail(w, 401, "login_required", "Укажи тестовый токен MCP.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 32_768)
		handler.ServeHTTP(w, r)
	})
}
