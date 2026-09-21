package chat

import "context"

import "github.com/Tencent/WeKnora/internal/models/api"

// WithRequiredImages prevents provider compatibility retries from silently
// dropping explicitly supplied user images. Tool-result images retain their
// existing best-effort behavior unless the turn also requires user images.
func WithRequiredImages(ctx context.Context) context.Context {
	return api.WithRequiredImages(ctx)
}
