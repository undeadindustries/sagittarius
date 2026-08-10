package prompteval

import (
	"bufio"
	"io"
	"strings"
)

// ParsedLog represents a parsed verbose log.
type ParsedLog struct {
	Turns []Turn
}

type Turn struct {
	UserMessage   string
	ModelResponse string
	ToolCalls     []ToolCall
	ToolResults   []ToolResult
}

type ToolResult struct {
	ID         string
	ResultText string
}

type ToolCall struct {
	Name string
	Args string
	ID   string
}

// ParseVerboseLog parses the verbose log produced by --log-verbose.
// It groups tool calls by user turn.
func ParseVerboseLog(r io.Reader) (*ParsedLog, error) {
	scanner := bufio.NewScanner(r)
	var log ParsedLog
	var currentTurn *Turn
	inUserMessage := false
	inToolResult := false
	inModelResponse := false
	var userMsgBuilder strings.Builder
	var toolResultBuilder strings.Builder
	var modelResponseBuilder strings.Builder
	var currentToolResultID string

	for scanner.Scan() {
		line := scanner.Text()

		// Check for section headers
		if strings.HasPrefix(line, "===== ") && strings.Contains(line, " | ") && strings.HasSuffix(line, " =====") {
			parts := strings.SplitN(line, " | ", 2)
			if len(parts) == 2 {
				kind := strings.TrimSuffix(parts[1], " =====")
				
				// End previous sections
				if inUserMessage && currentTurn != nil {
					currentTurn.UserMessage = strings.TrimSpace(userMsgBuilder.String())
					inUserMessage = false
				}
				if inModelResponse && currentTurn != nil {
					currentTurn.ModelResponse = strings.TrimSpace(modelResponseBuilder.String())
					inModelResponse = false
				}
				if inToolResult && currentTurn != nil {
					currentTurn.ToolResults = append(currentTurn.ToolResults, ToolResult{
						ID:         currentToolResultID,
						ResultText: strings.TrimSpace(toolResultBuilder.String()),
					})
					inToolResult = false
				}

				if kind == "IN  user message" {
					if currentTurn != nil {
						log.Turns = append(log.Turns, *currentTurn)
					}
					currentTurn = &Turn{}
					inUserMessage = true
					userMsgBuilder.Reset()
					continue
				} else if strings.HasPrefix(kind, "IN  model response ") || kind == "IN  model response" {
					if currentTurn == nil {
						currentTurn = &Turn{}
					}
					inModelResponse = true
					modelResponseBuilder.Reset()
					continue
				} else if strings.HasPrefix(kind, "OUT tool_result ") {
					if currentTurn == nil {
						currentTurn = &Turn{}
					}
					inToolResult = true
					currentToolResultID = strings.TrimPrefix(kind, "OUT tool_result ")
					toolResultBuilder.Reset()
					continue
				}
			}
		}

		if inUserMessage {
			userMsgBuilder.WriteString(line + "\n")
			continue
		}
		if inModelResponse {
			modelResponseBuilder.WriteString(line + "\n")
			// We don't continue here because we want to parse tool calls inside the model response
		}
		if inToolResult {
			toolResultBuilder.WriteString(line + "\n")
			continue
		}

		// Parse tool calls in other sections (mostly IN model response)
		if strings.HasPrefix(line, "tool_call: ") {
			if currentTurn == nil {
				// Should not happen in a valid log where user message comes first,
				// but handle it just in case.
				currentTurn = &Turn{}
			}
			// line: tool_call: name(args) id=id
			afterPrefix := strings.TrimPrefix(line, "tool_call: ")
			// find first '('
			parenIdx := strings.Index(afterPrefix, "(")
			if parenIdx != -1 {
				name := afterPrefix[:parenIdx]
				// find last ') id='
				endArgsIdx := strings.LastIndex(afterPrefix, ") id=")
				if endArgsIdx != -1 && endArgsIdx > parenIdx {
					args := afterPrefix[parenIdx+1 : endArgsIdx]
					id := afterPrefix[endArgsIdx+5:]
					currentTurn.ToolCalls = append(currentTurn.ToolCalls, ToolCall{
						Name: name,
						Args: args,
						ID:   id,
					})
				}
			}
		}
	}

	if currentTurn != nil {
		if inUserMessage {
			currentTurn.UserMessage = strings.TrimSpace(userMsgBuilder.String())
		}
		if inModelResponse {
			currentTurn.ModelResponse = strings.TrimSpace(modelResponseBuilder.String())
		}
		if inToolResult {
			currentTurn.ToolResults = append(currentTurn.ToolResults, ToolResult{
				ID:         currentToolResultID,
				ResultText: strings.TrimSpace(toolResultBuilder.String()),
			})
		}
		log.Turns = append(log.Turns, *currentTurn)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &log, nil
}
