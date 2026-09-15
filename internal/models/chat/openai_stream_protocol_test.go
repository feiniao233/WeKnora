package chat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/provider"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func protocolChat(t *testing.T, providerName string, handler http.HandlerFunc) (*RemoteAPIChat, *httptest.Server) {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := NewRemoteAPIChat(&ChatConfig{ModelName: "protocol-test", ModelID: "protocol-test", BaseURL: server.URL, APIKey: "test-key", Provider: providerName})
	require.NoError(t, err)
	return c, server
}

const protocolPartial = "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"inspect evidence\"}}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\"partial answer\",\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"resolve_alarm\",\"arguments\":\"{\\\"id\\\":\"}}]}}]}\n\n" +
	"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":3,\"total_tokens\":15}}\n\n"
const protocolToolFinish = "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"alarm-1\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n"

func TestOpenAIProtocolCompletionAndFailure(t *testing.T) {
	for _, providerName := range []string{string(provider.ProviderOpenAI), string(provider.ProviderDeepSeek)} {
		for _, tc := range []struct {
			name, suffix string
			failed       bool
		}{
			{"complete with usage tail", protocolToolFinish + "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":12,\"completion_tokens\":8,\"total_tokens\":20}}\n\ndata: [DONE]\n\n", false},
			{"finished without DONE", protocolToolFinish, false},
			{"premature EOF", "", true},
			{"malformed JSON", "data: {broken\n\n", true},
			{"provider error", "data: {\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"quota private-payload\"}}\n\n", true},
			{"failure after finish", protocolToolFinish + "data: {broken\n\n", true},
		} {
			t.Run(providerName+"/"+tc.name, func(t *testing.T) {
				c, _ := protocolChat(t, providerName, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					io.WriteString(w, protocolPartial+tc.suffix)
				})
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				stream, err := c.ChatStream(ctx, []Message{{Role: "user", Content: "test"}}, &ChatOptions{})
				require.NoError(t, err)
				var events []types.StreamResponse
				for event := range stream {
					events = append(events, event)
				}
				require.NotEmpty(t, events)
				last := events[len(events)-1]
				require.True(t, last.Done)
				require.Len(t, last.ToolCalls, 1)
				require.Equal(t, "resolve_alarm", last.ToolCalls[0].Function.Name)
				require.NotNil(t, last.Usage)
				require.Equal(t, 12, last.Usage.PromptTokens)
				var content, reasoning string
				terminals := 0
				for _, event := range events {
					if event.ResponseType == types.ResponseTypeAnswer {
						content += event.Content
						if event.Done {
							terminals++
						}
					}
					if event.ResponseType == types.ResponseTypeThinking {
						reasoning += event.Content
					}
				}
				require.Equal(t, "partial answer", content)
				require.Equal(t, "inspect evidence", reasoning)
				if tc.failed {
					require.Equal(t, types.ResponseTypeError, last.ResponseType)
					require.Equal(t, types.FinishReasonIncomplete, last.FinishReason)
					require.Zero(t, terminals)
				} else {
					require.Equal(t, types.ResponseTypeAnswer, last.ResponseType)
					require.Equal(t, "tool_calls", last.FinishReason)
					require.Equal(t, 1, terminals)
					require.Equal(t, `{"id":"alarm-1"}`, last.ToolCalls[0].Function.Arguments)
				}
				if providerName == string(provider.ProviderDeepSeek) && tc.name == "provider error" {
					require.Contains(t, last.Content, "rate_limited")
					require.NotContains(t, last.Content, "private-payload")
				}
				if tc.name == "complete with usage tail" {
					require.Equal(t, 20, last.Usage.TotalTokens)
				}
			})
		}
	}
}

func TestOpenAIProtocolCancellationClosesBlockedReader(t *testing.T) {
	for _, providerName := range []string{string(provider.ProviderOpenAI), string(provider.ProviderDeepSeek)} {
		t.Run(providerName, func(t *testing.T) {
			finished := make(chan struct{})
			c, _ := protocolChat(t, providerName, func(w http.ResponseWriter, r *http.Request) {
				defer close(finished)
				w.Header().Set("Content-Type", "text/event-stream")
				io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n")
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			})
			ctx, cancel := context.WithCancel(context.Background())
			stream, err := c.ChatStream(ctx, []Message{{Role: "user", Content: "test"}}, &ChatOptions{})
			require.NoError(t, err)
			cancel()
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("provider HTTP request remained blocked")
			}
			select {
			case _, ok := <-stream:
				require.False(t, ok)
			case <-time.After(time.Second):
				t.Fatal("stream did not close after cancellation")
			}
		})
	}
}

func TestRawStreamCancellationUnblocksProducerSend(t *testing.T) {
	c := newTestRemoteChat(t)
	ctx, cancel := context.WithCancel(context.Background())
	output := make(chan types.StreamResponse)
	done := make(chan struct{})
	read := make(chan struct{})
	go func() {
		defer close(done)
		c.processRawHTTPStream(ctx, &http.Response{Body: io.NopCloser(&signalReader{Reader: strings.NewReader(protocolPartial), read: read})}, output, nil)
	}()
	<-read
	// No consumer is attached: cancellation must release a blocked send itself.
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("producer stranded on send")
	}
}

func TestDeepSeekNativeThinkingAndNonstreamReasoning(t *testing.T) {
	for _, thinking := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[thinking], func(t *testing.T) {
			c, _ := protocolChat(t, string(provider.ProviderDeepSeek), func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, map[bool]string{true: "enabled", false: "disabled"}[thinking], body["thinking"].(map[string]any)["type"])
				if thinking {
					require.NotContains(t, body, "tool_choice")
				} else {
					require.Equal(t, "required", body["tool_choice"])
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"answer","reasoning_content":"retained reasoning"},"finish_reason":"stop"}]}`)
			})
			resp, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "test"}}, &ChatOptions{Thinking: &thinking, ToolChoice: "required"})
			require.NoError(t, err)
			require.Equal(t, "retained reasoning", resp.ReasoningContent)
		})
	}
}

type signalReader struct {
	io.Reader
	read chan struct{}
	once sync.Once
}

func (r *signalReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.once.Do(func() { close(r.read) })
	return n, err
}

func TestRawStreamForwarderCancellationWithoutConsumer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan types.StreamResponse)
	forwarded := make(chan struct{})
	cleaned := make(chan struct{})
	_, err := wrapStreamCancel(ctx, input, nil, func() { close(cleaned) })
	require.NoError(t, err)
	go func() { input <- types.StreamResponse{Content: "pending"}; close(forwarded); close(input) }()
	<-forwarded // The forwarder now owns an event that no caller will consume.
	cancel()
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("forwarder blocked despite cancellation")
	}
}
