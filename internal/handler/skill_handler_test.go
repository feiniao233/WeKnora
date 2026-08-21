package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	agentskills "github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type stubSkillService struct {
	interfaces.SkillService
}

func (stubSkillService) GetSkillByName(_ context.Context, name string) (*agentskills.Skill, error) {
	return &agentskills.Skill{Name: name, Description: "RCA", Instructions: "read references/runbook.md"}, nil
}

func TestGetSkill(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	handler := NewSkillHandler(stubSkillService{})
	router.GET("/skills/:name", handler.GetSkill)

	t.Run("returns instructions", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/skills/rca-diagnosis", nil))
		if response.Code != http.StatusOK || response.Body.String() == "" {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("returns unicode skill", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/skills/%E5%BC%95%E7%94%A8%E7%94%9F%E6%88%90%E5%99%A8", nil))
		if response.Code != http.StatusOK || response.Body.String() == "" {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("rejects dotted name", func(t *testing.T) {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/skills/with.dot", nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})
}
