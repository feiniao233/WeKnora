package chat

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequiredImagesNeverRetryWithoutImages(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "chat", true: "stream"}[stream], func(t *testing.T) {
			var calls atomic.Int32
			c, _ := protocolChat(t, "openai", func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				messages := body["messages"].([]any)
				content := messages[0].(map[string]any)["content"].([]any)
				require.Len(t, content, 6) // all five images plus the user text
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"message":"model does not support image input","type":"invalid_request_error"}}`))
			})
			ctx := WithRequiredImages(t.Context())
			messages := []Message{{Role: "user", Content: "describe", Images: []string{"data:image/png;base64,eA==", "data:image/png;base64,eA==", "data:image/png;base64,eA==", "data:image/png;base64,eA==", "data:image/png;base64,eA=="}}}
			if stream {
				_, err := c.ChatStream(ctx, messages, &ChatOptions{})
				require.Error(t, err)
			} else {
				_, err := c.Chat(ctx, messages, &ChatOptions{})
				require.Error(t, err)
			}
			require.Equal(t, int32(1), calls.Load())
		})
	}
}
