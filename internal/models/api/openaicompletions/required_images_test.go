package openaicompletions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/catalog"
	"github.com/stretchr/testify/require"
)

func TestRequiredImagesNeverRetryWithoutImages(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "chat", true: "stream"}[stream], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				messages := body["messages"].([]any)
				content := messages[0].(map[string]any)["content"].([]any)
				require.Len(t, content, 6)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"model does not support image input","type":"invalid_request_error"}}`))
			}))
			defer server.Close()

			client := New(Config{
				Endpoint: api.Endpoint{BaseURL: server.URL, Model: "m"},
				Settings: catalog.DefaultOpenAICompletions(),
			})
			ctx := api.WithRequiredImages(context.Background())
			messages := []api.Message{{
				Role: "user", Content: "describe",
				Images: []string{
					"data:image/png;base64,eA==", "data:image/png;base64,eA==",
					"data:image/png;base64,eA==", "data:image/png;base64,eA==",
					"data:image/png;base64,eA==",
				},
			}}
			if stream {
				_, err := client.ChatStream(ctx, messages, &api.Options{})
				require.Error(t, err)
			} else {
				_, err := client.Chat(ctx, messages, &api.Options{})
				require.Error(t, err)
			}
			require.Equal(t, int32(1), calls.Load())
		})
	}
}
