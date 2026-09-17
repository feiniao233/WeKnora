package session

import (
	"net/http/httptest"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestExplicitSkillCannotFallBackToQuickAnswerOrMissingAgent(t *testing.T) {
	for _, mode := range []qaMode{qaModeNormal, qaModeAgent} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := &qaRequestContext{c: c, skillNames: []string{"selected"}}
		if mode == qaModeNormal {
			req.customAgent = &types.CustomAgent{}
		}
		// No service dependencies: rejection must occur before any persistence or model work.
		(&Handler{}).executeQA(req, mode, false)
		require.Len(t, c.Errors, 1)
		require.Contains(t, c.Errors[0].Error(), "skill_unavailable")
	}
}
