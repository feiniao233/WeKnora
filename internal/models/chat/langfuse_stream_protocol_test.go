package chat

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/tracing/langfuse"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type protocolStubChat struct {
	stream func(context.Context) <-chan types.StreamResponse
}

func (*protocolStubChat) GetModelName() string { return "test-model" }
func (*protocolStubChat) GetModelID() string   { return "test-model-id" }
func (*protocolStubChat) Chat(context.Context, []Message, *ChatOptions) (*types.ChatResponse, error) {
	return nil, nil
}
func (s *protocolStubChat) ChatStream(ctx context.Context, _ []Message, _ *ChatOptions) (<-chan types.StreamResponse, error) {
	return s.stream(ctx), nil
}

func TestLangfuseStreamTerminalAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []types.StreamResponse
		cancel bool
		failed bool
	}{
		{"thinking done is not terminal", []types.StreamResponse{{ResponseType: types.ResponseTypeThinking, Content: "private reasoning"}, {ResponseType: types.ResponseTypeThinking, Done: true}}, false, true},
		{"stream error", []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: "partial"}, {ResponseType: types.ResponseTypeError, Content: "model_timeout", Done: true}}, false, true},
		{"complete", []types.StreamResponse{{ResponseType: types.ResponseTypeThinking, Done: true}, {ResponseType: types.ResponseTypeAnswer, Content: "answer", Done: true}}, false, false},
		{"cancel blocked output", nil, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var spans []*tracepb.Span
			collectorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				var request collector.ExportTraceServiceRequest
				require.NoError(t, proto.Unmarshal(raw, &request))
				mu.Lock()
				for _, resource := range request.ResourceSpans {
					for _, scope := range resource.ScopeSpans {
						spans = append(spans, scope.Spans...)
					}
				}
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-protobuf")
			}))
			defer collectorServer.Close()
			mgr, err := langfuse.Init(langfuse.Config{Enabled: true, Host: collectorServer.URL, PublicKey: "test-public", SecretKey: "test-secret", SampleRate: 1, FlushAt: 1, FlushInterval: time.Millisecond, QueueSize: 32, RequestTimeout: time.Second})
			require.NoError(t, err)
			defer langfuse.Init(langfuse.Config{Enabled: false})
			producerDone := make(chan struct{})
			firstAccepted := make(chan struct{})
			stub := &protocolStubChat{stream: func(ctx context.Context) <-chan types.StreamResponse {
				ch := make(chan types.StreamResponse)
				go func() {
					defer close(ch)
					defer close(producerDone)
					if tc.cancel {
						if sendStreamResponse(ctx, ch, types.StreamResponse{ResponseType: types.ResponseTypeAnswer, Content: "pending"}) {
							close(firstAccepted)
						}
						<-ctx.Done()
						return
					}
					for _, event := range tc.events {
						if !sendStreamResponse(ctx, ch, event) {
							return
						}
					}
				}()
				return ch
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wrapped := &langfuseChat{inner: stub}
			stream, err := wrapped.ChatStream(ctx, []Message{{Role: "user", Content: "private prompt"}}, nil)
			require.NoError(t, err)
			if tc.cancel {
				select {
				case <-firstAccepted:
				case <-time.After(time.Second):
					t.Fatal("wrapper did not receive first chunk")
				}
				cancel()
			}
			drained := make(chan struct{})
			go func() {
				for range stream {
				}
				close(drained)
			}()
			select {
			case <-drained:
			case <-time.After(time.Second):
				t.Fatal("wrapper remained blocked")
			}
			select {
			case <-producerDone:
			case <-time.After(time.Second):
				t.Fatal("inner producer remained blocked")
			}
			deadline, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			require.NoError(t, mgr.Shutdown(deadline))
			mu.Lock()
			defer mu.Unlock()
			var generation *tracepb.Span
			for _, span := range spans {
				if span.Name == "chat.completion.stream" {
					for _, attr := range span.Attributes {
						if attr.Key == "langfuse.observation.type" && attr.Value.GetStringValue() == "generation" {
							generation = span
						}
					}
				}
			}
			require.NotNil(t, generation)
			if tc.failed {
				require.Equal(t, tracepb.Status_STATUS_CODE_ERROR, generation.Status.GetCode())
			} else {
				require.NotEqual(t, tracepb.Status_STATUS_CODE_ERROR, generation.Status.GetCode())
			}
		})
	}
}
