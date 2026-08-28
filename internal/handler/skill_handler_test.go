package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	agentskills "github.com/Tencent/WeKnora/internal/agent/skills"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

type stubSkillService struct {
	interfaces.SkillService
}

func TestGetSkillReturnsTextResources(t *testing.T) {
	skillsDir := t.TempDir()
	skillDir := filepath.Join(skillsDir, "rca-diagnosis")
	for name, content := range map[string][]byte{
		"SKILL.md":              []byte("---\nname: rca-diagnosis\ndescription: RCA\n---\nRead the runbook."),
		"references/runbook.md": []byte("# Runbook"),
		"scripts/check.py":      []byte("print('ok')"),
		"assets/icon.png":       {0x89, 'P', 'N', 'G', 0},
	} {
		path := filepath.Join(skillDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WEKNORA_SKILLS_DIR", skillsDir)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler())
	router.GET("/skills/:name", NewSkillHandler(service.NewSkillService()).GetSkill)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/skills/rca-diagnosis", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data struct {
			Resources []struct {
				Name     string `json:"name"`
				Content  string `json:"content"`
				IsScript bool   `json:"is_script"`
			} `json:"resources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Resources) != 2 {
		t.Fatalf("resources=%+v", body.Data.Resources)
	}
	if got := body.Data.Resources[0]; got.Name != "references/runbook.md" || got.Content != "# Runbook" || got.IsScript {
		t.Fatalf("reference=%+v", got)
	}
	if got := body.Data.Resources[1]; got.Name != "scripts/check.py" || got.Content != "print('ok')" || !got.IsScript {
		t.Fatalf("script=%+v", got)
	}
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
