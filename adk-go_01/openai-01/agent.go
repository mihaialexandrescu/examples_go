package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/openai/openai-go/v3/option"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/cmd/launcher/full"
	openaimodel "google.golang.org/adk/v2/model/openaimodel"
)

// fixResponsesAssistantContentType works around a bug in
// google.golang.org/adk/v2/model/openaimodel: its request builder stamps every
// message content part with type "input_text", even for assistant (model)
// messages. The OpenAI Responses API — strictly enforced by Azure via the
// litellm proxy — requires assistant message content to use "output_text".
// Without this, any multi-turn conversation fails with a 400 like:
//
//	Invalid value: 'input_text'. Supported values are: 'output_text' and 'refusal'.
//
// This middleware rewrites the outgoing request body, converting "input_text" to
// "output_text" for content parts of any message whose role is "assistant".
func fixResponsesAssistantContentType(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	if req.Body != nil && req.Method == http.MethodPost {
		raw, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		if fixed, changed := rewriteAssistantContentType(raw); changed {
			raw = fixed
		}
		req.Body = io.NopCloser(bytes.NewReader(raw))
		req.ContentLength = int64(len(raw))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(raw)), nil
		}
	}
	return next(req)
}

// rewriteAssistantContentType converts "input_text" content parts to
// "output_text" for assistant-role messages in a Responses API request body.
// It returns the (possibly) rewritten body and whether any change was made. On
// any parse error it returns the original bytes unchanged.
func rewriteAssistantContentType(raw []byte) ([]byte, bool) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw, false
	}
	input, ok := payload["input"].([]any)
	if !ok {
		return raw, false
	}
	changed := false
	for _, item := range input {
		msg, ok := item.(map[string]any)
		if !ok || msg["role"] != "assistant" {
			continue
		}
		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, part := range content {
			p, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if p["type"] == "input_text" {
				p["type"] = "output_text"
				changed = true
			}
		}
	}
	if !changed {
		return raw, false
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return raw, false
	}
	return out, true
}

// logExchangedMessages returns a middleware that logs the raw JSON exchanged
// with the LLM to the given slog logger: the complete request body sent on the
// wire (pretty-printed with every field) and the complete response body received
// (the raw SSE event stream for streaming responses, or pretty-printed JSON for
// non-streaming and error responses). This lets you inspect the exact request
// and response structures and reproduce calls independently (e.g. with curl).
func logExchangedMessages(logger *slog.Logger) option.Middleware {
	return func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		if req.Body != nil && req.Method == http.MethodPost {
			raw, err := io.ReadAll(req.Body)
			_ = req.Body.Close()
			if err != nil {
				return nil, err
			}
			req.Body = io.NopCloser(bytes.NewReader(raw))
			req.ContentLength = int64(len(raw))
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(raw)), nil
			}
			logger.Info("llm --> request",
				"method", req.Method,
				"url", req.URL.String(),
				slog.String(headersLogKey, formatHeaders(req.Header)),
				slog.String(bodyLogKey, prettyJSON(raw)),
			)
		}
		resp, err := next(req)
		if err != nil {
			logger.Error("llm <-- transport error", "err", err)
			return resp, err
		}
		if resp != nil && resp.Body != nil {
			resp.Body = &responseBodyLogger{
				rc:          resp.Body,
				logger:      logger,
				status:      resp.StatusCode,
				contentType: resp.Header.Get("Content-Type"),
				header:      resp.Header,
			}
		}
		return resp, err
	}
}

// responseBodyLogger tees the response body so it can log the full raw body once
// it has been read (or closed), without disturbing streaming consumption.
type responseBodyLogger struct {
	rc          io.ReadCloser
	logger      *slog.Logger
	status      int
	contentType string
	header      http.Header
	buf         bytes.Buffer
	logged      bool
}

func (r *responseBodyLogger) Read(p []byte) (int, error) {
	n, err := r.rc.Read(p)
	if n > 0 {
		r.buf.Write(p[:n])
	}
	if err == io.EOF {
		r.flush()
	}
	return n, err
}

func (r *responseBodyLogger) Close() error {
	r.flush()
	return r.rc.Close()
}

func (r *responseBodyLogger) flush() {
	if r.logged {
		return
	}
	r.logged = true
	body := r.buf.Bytes()
	// Streaming responses are Server-Sent Events (a sequence of `data: {...}`
	// lines); log them verbatim so every event and field is visible. Everything
	// else (non-streaming success and error bodies) is JSON, pretty-printed.
	var rendered string
	if strings.Contains(r.contentType, "text/event-stream") {
		rendered = string(body)
	} else {
		rendered = prettyJSON(body)
	}
	r.logger.Info("llm <-- response",
		"status", r.status,
		"contentType", r.contentType,
		slog.String(headersLogKey, formatHeaders(r.header)),
		slog.String(bodyLogKey, rendered),
	)
}

// Attribute keys whose values are rendered as multi-line labeled blocks after
// the log header (used for HTTP headers and raw request/response JSON).
const (
	headersLogKey = "headers"
	bodyLogKey    = "body"
)

// blockHandler is a minimal slog.Handler that prints a single header line
// (timestamp, level, message, and scalar attributes) followed by the "headers"
// and "body" attributes rendered verbatim on their own lines. Each record is
// prefixed with a newline so multi-line output is never glued onto the agent's
// streamed answer (which is written to stdout without a trailing newline).
type blockHandler struct {
	w     io.Writer
	level slog.Level
}

func (h *blockHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *blockHandler) Handle(_ context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteByte('\n')
	sb.WriteString(r.Time.Format("2006-01-02 15:04:05"))
	sb.WriteByte(' ')
	sb.WriteString(r.Level.String())
	sb.WriteByte(' ')
	sb.WriteString(r.Message)
	type block struct{ key, value string }
	var blocks []block
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == headersLogKey || a.Key == bodyLogKey {
			blocks = append(blocks, block{a.Key, a.Value.String()})
			return true
		}
		sb.WriteByte(' ')
		sb.WriteString(a.Key)
		sb.WriteByte('=')
		sb.WriteString(a.Value.String())
		return true
	})
	sb.WriteByte('\n')
	for _, b := range blocks {
		if b.value == "" {
			continue
		}
		sb.WriteString(b.key)
		sb.WriteString(":\n")
		sb.WriteString(b.value)
		if !strings.HasSuffix(b.value, "\n") {
			sb.WriteByte('\n')
		}
	}
	_, err := io.WriteString(h.w, sb.String())
	return err
}

func (h *blockHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *blockHandler) WithGroup(_ string) slog.Handler      { return h }

// formatHeaders renders HTTP headers as sorted "Name: value" lines, obfuscating
// the values of headers that carry credentials (see isSensitiveHeader).
func formatHeaders(h http.Header) string {
	if len(h) == 0 {
		return ""
	}
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, name := range names {
		value := strings.Join(h[name], ", ")
		if isSensitiveHeader(name) {
			value = obfuscateSecret(value)
		}
		sb.WriteString(name)
		sb.WriteString(": ")
		sb.WriteString(value)
		sb.WriteByte('\n')
	}
	return strings.TrimRight(sb.String(), "\n")
}

// isSensitiveHeader reports whether a header name is known to carry a secret
// (API key, token, cookie, ...) and should have its value obfuscated.
func isSensitiveHeader(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization",
		"api-key", "x-api-key", "openai-api-key",
		"openai-organization", "openai-project",
		"cookie", "set-cookie":
		return true
	}
	return false
}

// obfuscateSecret masks a secret value credit-card style, keeping only the first
// and last 3 characters visible and replacing the middle with asterisks. Values
// of 6 characters or fewer are fully masked.
func obfuscateSecret(s string) string {
	const keep = 3
	if len(s) <= keep*2 {
		return strings.Repeat("*", len(s))
	}
	return s[:keep] + strings.Repeat("*", len(s)-keep*2) + s[len(s)-keep:]
}

// prettyJSON indents a JSON document for readability, returning the input
// unchanged if it is not valid JSON.
func prettyJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return string(raw)
	}
	return buf.String()
}

// messageLoggingEnabled reports whether message logging was requested via the
// OPENAI_LOG_MESSAGES environment variable.
func messageLoggingEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OPENAI_LOG_MESSAGES"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// defaultModel is used when OPENAI_MODEL is unset. gpt-4o-mini is cheap and
// serves the Responses API that this integration targets.
const defaultModel = "gpt-4o-mini"

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if apiKey == "" && baseURL == "" {
		slog.Error("set OPENAI_API_KEY (for OpenAI) or OPENAI_BASE_URL (for an OpenAI-compatible endpoint)")
		os.Exit(1)
	}

	modelName := os.Getenv("OPENAI_MODEL")
	if modelName == "" {
		modelName = defaultModel
	}

	opts := []option.RequestOption{
		option.WithMiddleware(fixResponsesAssistantContentType),
	}
	// Optionally log the messages exchanged with the LLM. Enable with
	// OPENAI_LOG_MESSAGES=1. Logs go to stderr so they stay separate from the
	// agent's answer on stdout (redirect with `2>messages.log` to split them).
	if messageLoggingEnabled() {
		logger := slog.New(&blockHandler{w: os.Stderr, level: slog.LevelInfo})
		opts = append(opts, option.WithMiddleware(logExchangedMessages(logger)))
	}

	model, err := openaimodel.NewModel(ctx, modelName, &openaimodel.ClientConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Options: opts,
	})
	if err != nil {
		slog.Error("failed to create model", "err", err)
		os.Exit(1)
	}

	haikuAgent, err := llmagent.New(llmagent.Config{
		Name:        "hello_haiku_agent",
		Model:       model,
		Description: "When specifically asked to, writes a haiku about the given topic.",
		Instruction: "You are a helpful assistant that, when specifically asked to, responds with a haiku about the given topic.",
		Tools:       nil,
	})
	if err != nil {
		slog.Error("failed to create agent", "err", err)
		os.Exit(1)
	}

	config := &launcher.Config{
		AgentLoader: agent.NewSingleLoader(haikuAgent),
	}

	l := full.NewLauncher()
	if err = l.Execute(ctx, config, os.Args[1:]); err != nil {
		slog.Error("run failed", "err", err, "usage", l.CommandLineSyntax())
		os.Exit(1)
	}
}