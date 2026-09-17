package session

import (
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBusinessReferencesCannotFallBackToQuickAnswerOrMissingAgent(t *testing.T) {
	for _, kind := range []string{"asset", "alarm"} {
		for _, mode := range []qaMode{qaModeNormal, qaModeAgent} {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			req := &qaRequestContext{c: c, mentionedItems: types.MentionedItems{{Type: kind, ID: "opaque-reference"}}}
			if mode == qaModeNormal {
				req.customAgent = &types.CustomAgent{}
			}
			(&Handler{}).executeQA(req, mode, false)
			require.Len(t, c.Errors, 1)
			require.Contains(t, c.Errors[0].Error(), "context_unavailable")
		}
	}
}
