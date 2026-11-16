package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"
)

//go:embed data/intent_dataset.json data/knowledge/*.md
var demoData embed.FS

var classes = []string{"explain_access", "request_access", "request_status", "next_step", "clarify", "out_of_scope"}
var words = regexp.MustCompile(`[\p{L}\p{N}]+`)

type example struct {
	Split   string `json:"split"`
	Message string `json:"message"`
	State   string `json:"workflow_state"`
	Target  struct {
		Intent string `json:"intent"`
	} `json:"target"`
}

type prediction struct {
	Intent string `json:"intent"`
}

type classifier struct {
	counts     map[string]map[string]int
	totals     map[string]int
	vocabulary map[string]bool
}

func features(message, state string) []string {
	var result []string
	for _, token := range words.FindAllString(strings.ToLower(message), -1) {
		result = append(result, token)
		letters := []rune("^" + token + "$")
		for i := 0; i+3 <= len(letters); i++ {
			result = append(result, "#"+string(letters[i:i+3]))
		}
	}
	return append(result, "state:"+state)
}

func loadExamples() []example {
	raw, err := demoData.ReadFile("data/intent_dataset.json")
	if err != nil {
		panic(err)
	}
	var data struct {
		Records []example `json:"records"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		panic(err)
	}
	return data.Records
}

func train(examples []example) *classifier {
	model := &classifier{map[string]map[string]int{}, map[string]int{}, map[string]bool{}}
	for _, label := range classes {
		model.counts[label] = map[string]int{}
	}
	for _, ex := range examples {
		if ex.Split != "train" {
			continue
		}
		label := ex.Target.Intent
		for _, feature := range features(ex.Message, ex.State) {
			model.counts[label][feature]++
			model.totals[label]++
			model.vocabulary[feature] = true
		}
	}
	return model
}

func (c *classifier) classify(message, state string) string {
	tokens := words.FindAllString(strings.ToLower(message), -1)
	normalized := strings.Join(tokens, " ")
	switch normalized {
	case "", "да", "нет", "ок", "ага", "подтверждаю", "отправь", "отправляй":
		return "clarify"
	case "как получить доступ", "как получить доступ к вики", "как получить доступ к пространству команды":
		return "explain_access"
	case "подготовь заявку", "подготовь заявку на доступ", "подготовь черновик":
		return "request_access"
	case "какой статус моей заявки", "что с моей заявкой", "проверь статус":
		return "request_status"
	case "что дальше", "что делать дальше":
		return "next_step"
	}
	items := features(message, state)
	if len(items) < 4 {
		return "clarify"
	}
	label, top, second := "clarify", math.Inf(-1), math.Inf(-1)
	for _, intent := range classes {
		score := 0.0
		for _, item := range items {
			if !c.vocabulary[item] {
				continue
			}
			score += math.Log(float64(c.counts[intent][item]+1) / float64(c.totals[intent]+len(c.vocabulary)))
		}
		if score > top {
			label, top, second = intent, score, top
		} else if score > second {
			second = score
		}
	}
	if top-second < 1.5 {
		return "clarify"
	}
	return label
}

type IntentRouter interface {
	Route(context.Context, Actor, string, string) (string, error)
}

func (c *classifier) Route(_ context.Context, _ Actor, message, state string) (string, error) {
	return c.classify(message, state), nil
}

type HTTPRouter struct {
	URL string
	Key string
}

func (r HTTPRouter) Route(ctx context.Context, actor Actor, message, state string) (string, error) {
	input := struct {
		Message string `json:"message"`
		State   string `json:"workflow_state"`
		Actor   Actor  `json:"actor_context"`
	}{message, state, actor}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(input); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.URL, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if r.Key != "" {
		req.Header.Set("Authorization", "Bearer "+r.Key)
	}
	client := http.Client{Timeout: 18 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("router returned HTTP %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return "", errors.New("invalid router response length")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var reply prediction
	if err := decoder.Decode(&reply); err != nil {
		return "", err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return "", errors.New("extra router data")
	}
	for _, intent := range classes {
		if intent == reply.Intent {
			return intent, nil
		}
	}
	return "", errors.New("unexpected intent")
}
