package contextmgmt

import (
	"bytes"
	"encoding/json"
)

// jsContent mirrors the JS { role, parts } shape so contentCharCount produces
// the same byte length as the fork's JSON.stringify(content).
type jsContent struct {
	Role  string   `json:"role"`
	Parts []jsPart `json:"parts"`
}

type jsPart struct {
	Text             string  `json:"text,omitempty"`
	FunctionCall     *jsCall `json:"functionCall,omitempty"`
	FunctionResponse *jsResp `json:"functionResponse,omitempty"`
}

type jsCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type jsResp struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// contentCharCount returns the length of a JS-compatible JSON encoding of the
// message, used by FindCompressSplitPoint to weight history entries the same
// way the fork does. HTML escaping is disabled to match JSON.stringify output.
func contentCharCount(content Message) int {
	jc := jsContent{Role: string(content.Role)}
	jc.Parts = make([]jsPart, 0, len(content.Parts))
	for i := range content.Parts {
		jc.Parts = append(jc.Parts, toJSPart(content.Parts[i]))
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(jc); err != nil {
		return 0
	}
	// json.Encoder appends a trailing newline; exclude it from the count.
	return buf.Len() - 1
}

func toJSPart(part Part) jsPart {
	switch {
	case part.FunctionCall != nil:
		args := part.FunctionCall.Args
		if args == nil {
			args = map[string]any{}
		}
		return jsPart{FunctionCall: &jsCall{
			ID:   part.FunctionCall.ID,
			Name: part.FunctionCall.Name,
			Args: args,
		}}
	case part.FunctionResponse != nil:
		resp := part.FunctionResponse.Response
		if resp == nil {
			resp = map[string]any{}
		}
		return jsPart{FunctionResponse: &jsResp{
			Name:     part.FunctionResponse.Name,
			Response: resp,
		}}
	default:
		return jsPart{Text: part.Text}
	}
}
