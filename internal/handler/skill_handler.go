package handler

import (
	"net/http"
	"strings"
	"unicode"

	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
)

// SkillHandler handles skill-related HTTP requests
type SkillHandler struct {
	skillService interfaces.SkillService
}

// NewSkillHandler creates a new skill handler
func NewSkillHandler(skillService interfaces.SkillService) *SkillHandler {
	return &SkillHandler{
		skillService: skillService,
	}
}

// SkillInfoResponse represents the skill info returned to frontend
type SkillInfoResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SkillDetailResponse struct {
	SkillInfoResponse
	Instructions string                  `json:"instructions"`
	Resources    []SkillResourceResponse `json:"resources"`
}

type SkillResourceResponse struct {
	Name     string `json:"name"`
	Content  string `json:"content"`
	IsScript bool   `json:"is_script"`
}

// ListSkills godoc
// @Summary      获取预装Skills列表
// @Description  获取所有预装的Agent Skills元数据
// @Tags         Skills
// @Accept       json
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "Skills列表"
// @Failure      500  {object}  errors.AppError         "服务器错误"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /skills [get]
func (h *SkillHandler) ListSkills(c *gin.Context) {
	ctx := c.Request.Context()

	skillsMetadata, err := h.skillService.ListPreloadedSkills(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to list skills: " + err.Error()))
		return
	}

	// Convert to response format
	var response []SkillInfoResponse
	for _, meta := range skillsMetadata {
		response = append(response, SkillInfoResponse{
			Name:        meta.Name,
			Description: meta.Description,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    response,
		// Skills can be selected before a workspace sandbox is configured; the
		// execution path remains disabled until the agent chooses a config.
		"skills_available": true,
	})
}

// GetSkill returns one preloaded skill's instructions for the management UI.
func (h *SkillHandler) GetSkill(c *gin.Context) {
	ctx := c.Request.Context()
	name := strings.TrimSpace(c.Param("name"))
	if !validSkillName(name) {
		c.Error(errors.NewBadRequestError("Invalid skill name"))
		return
	}
	skill, err := h.skillService.GetSkillByName(ctx, name)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"skill_name": name})
		c.Error(errors.NewNotFoundError("Skill not found"))
		return
	}
	resources := make([]SkillResourceResponse, 0, len(skill.Resources))
	for _, resource := range skill.Resources {
		resources = append(resources, SkillResourceResponse{
			Name: resource.Name, Content: resource.Content, IsScript: resource.IsScript,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": SkillDetailResponse{
			SkillInfoResponse: SkillInfoResponse{Name: skill.Name, Description: skill.Description},
			Instructions:      skill.Instructions,
			Resources:         resources,
		},
	})
}

func validSkillName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if r == '-' || unicode.IsLetter(r) || unicode.IsNumber(r) {
			continue
		}
		return false
	}
	return true
}
