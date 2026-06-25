package tools

import (
	"context"
	"strings"
	"testing"
)

func TestWebSearchTool_Schema(t *testing.T) {
	tool := &webSearchTool{}
	decl := tool.Declaration()
	if decl.Name != GoogleWebSearchToolName {
		t.Errorf("expected %q, got %q", GoogleWebSearchToolName, decl.Name)
	}
	if len(decl.Parameters["required"].([]string)) != 1 || decl.Parameters["required"].([]string)[0] != ParamQuery {
		t.Error("expected 'query' to be required")
	}
}

func TestWebFetchTool_Schema(t *testing.T) {
	t.Run("default mode", func(t *testing.T) {
		tool := newWebFetchTool(nil, false, 1024)
		decl := tool.Declaration()
		if len(decl.Parameters["required"].([]string)) != 1 || decl.Parameters["required"].([]string)[0] != ParamPrompt {
			t.Error("expected 'prompt' to be required in default mode")
		}
	})

	t.Run("direct mode", func(t *testing.T) {
		tool := newWebFetchTool(nil, true, 1024)
		decl := tool.Declaration()
		if len(decl.Parameters["required"].([]string)) != 1 || decl.Parameters["required"].([]string)[0] != ParamURL {
			t.Error("expected 'url' to be required in direct mode")
		}
	})
}

func TestWebFetchTool_DirectExecutionValidation(t *testing.T) {
	tool := newWebFetchTool(nil, true, 1024)
	_, err := tool.Execute(context.Background(), map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "requires a non-empty string parameter \"url\"") {
		t.Errorf("expected validation error, got: %v", err)
	}

	result, err := tool.Execute(context.Background(), map[string]interface{}{ParamURL: "not-a-url"})
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}

	// Expect results to indicate no valid URLs found.
	// Since we wrap in map[string]interface{}{"results": ...}, check that.
	if rStr, ok := result["results"].(string); !ok || !strings.Contains(rStr, "No valid URLs") {
		t.Errorf("expected No valid URLs error inside results, got %v", result)
	}
}
