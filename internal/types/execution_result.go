package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
)

type MessageExecutionResult struct {
	Status string          `json:"status"`
	Error  *ExecutionError `json:"error,omitempty"`
}

// ExecutionError is safe to return to the browser and store in history.
// Provider bodies, URLs, prompts and credentials never enter this object.
type ExecutionError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	RequestID string `json:"request_id,omitempty"`
}

func ClassifyExecutionError(raw, requestID string) *ExecutionError {
	msg := strings.ToLower(raw)
	e := &ExecutionError{Code: "diagnosis_failed", Message: "诊断未完成，请重试；已生成内容已保留。", Retryable: true, RequestID: requestID}
	switch {
	case strings.Contains(msg, "message_save_failed"), strings.Contains(msg, "execution result could not be saved"):
		e.Code, e.Message = "message_save_failed", "诊断结果保存失败，请重试；当前页面仍保留已生成内容。"
	case strings.Contains(msg, "rate_limited"), strings.Contains(msg, "429"), strings.Contains(msg, "rate limit"), strings.Contains(msg, "quota"):
		e.Code, e.Message = "rate_limited", "模型请求受限或额度不足，请稍后重试或切换模型。"
	case strings.Contains(msg, "model_unauthorized"), strings.Contains(msg, "401"), strings.Contains(msg, "403"), strings.Contains(msg, "authentication"), strings.Contains(msg, "api key"):
		e.Code, e.Message, e.Retryable = "model_unauthorized", "模型接入认证失败，请检查配置或切换模型。", false
	case strings.Contains(msg, "context_length_exceeded"), strings.Contains(msg, "context length"), strings.Contains(msg, "context_length"), strings.Contains(msg, "maximum context"), strings.Contains(msg, "too many tokens"):
		e.Code, e.Message, e.Retryable = "context_length_exceeded", "输入或历史内容超出模型限制，请缩短输入或新建会话。", false
	case strings.Contains(msg, "model_timeout"), strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "timed out"):
		e.Code, e.Message = "model_timeout", "模型响应超时，请重试或切换模型。"
	case strings.Contains(msg, "model_unavailable"), strings.Contains(msg, "chat model"), strings.Contains(msg, "model_id"), strings.Contains(msg, "model not found"):
		e.Code, e.Message, e.Retryable = "model_unavailable", "所选模型不可用，请检查配置或切换模型。", false
	case strings.Contains(msg, "model_protocol_error"), strings.Contains(msg, "reasoning_content"), strings.Contains(msg, "400"), strings.Contains(msg, "invalid request"), strings.Contains(msg, "unsupported"):
		e.Code, e.Message, e.Retryable = "model_protocol_error", "模型接口不兼容当前请求，请检查接入配置或切换模型。", false
	case strings.Contains(msg, "stream_interrupted"), strings.Contains(msg, "eof"), strings.Contains(msg, "stream interrupted"):
		e.Code, e.Message = "stream_interrupted", "模型响应意外中断，请重试；已生成内容已保留。"
	case strings.Contains(msg, "upstream_unavailable"), strings.Contains(msg, "502"), strings.Contains(msg, "503"), strings.Contains(msg, "connection refused"):
		e.Code, e.Message = "upstream_unavailable", "模型服务暂时不可用，请稍后重试或切换模型。"
	}
	return e
}

func (r *MessageExecutionResult) Value() (driver.Value, error) {
	if r == nil {
		return nil, nil
	}
	return json.Marshal(r)
}

func (r *MessageExecutionResult) Scan(value interface{}) error {
	if value == nil {
		*r = MessageExecutionResult{}
		return nil
	}
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("invalid execution result database type %T", value)
	}
	return json.Unmarshal(data, r)
}
