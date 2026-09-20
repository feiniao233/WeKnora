package chat

import "context"

type requiredImagesKey struct{}

// WithRequiredImages prevents provider compatibility retries from silently
// dropping explicitly supplied user images. Tool-result images retain their
// existing best-effort behavior unless the turn also requires user images.
func WithRequiredImages(ctx context.Context) context.Context {
	return context.WithValue(ctx, requiredImagesKey{}, true)
}

func requiresImages(ctx context.Context) bool {
	required, _ := ctx.Value(requiredImagesKey{}).(bool)
	return required
}
