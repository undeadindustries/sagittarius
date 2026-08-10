package agent

import (
	"context"
	"strings"

	"github.com/undeadindustries/sagittarius/internal/provider"
)

// IntentVerdict represents the result of the conversational read-only lock classifier.
type IntentVerdict int

const (
	IntentNeutral IntentVerdict = iota
	IntentLock
	IntentUnlock
)

// classifyReadOnlyIntent evaluates the user input to determine if it is requesting
// a read-only lock ("don't change anything yet") or lifting one ("go ahead").
func classifyReadOnlyIntent(ctx context.Context, input string, aux provider.ContentGenerator) IntentVerdict {
	lower := strings.ToLower(strings.TrimSpace(input))

	// Very simple heuristic to skip quoting (e.g. `what happens if I say "go ahead"?`)
	// We'll just strip out anything between quotes before matching, as a fast guard against false positives.
	stripped := stripQuotes(lower)

	// 1. Check for explicit unlock phrases FIRST.
	unlocks := []string{
		"go ahead", "do it", "apply it", "make the change", "yes, proceed", "proceed",
	}
	for _, phrase := range unlocks {
		if strings.Contains(stripped, phrase) {
			// Basic negation guard: "don't go ahead" -> Neutral
			if strings.Contains(stripped, "don't "+phrase) || strings.Contains(stripped, "do not "+phrase) {
				continue
			}
			return IntentUnlock
		}
	}

	// 2. Check for explicit lock phrases.
	locks := []string{
		"don't change anything", "do not change anything", "read only", "read-only",
		"just discuss", "just look", "no changes yet", "don't modify", "do not modify",
	}
	for _, phrase := range locks {
		if strings.Contains(stripped, phrase) {
			return IntentLock
		}
	}

	// 3. Fallback to aux-generator for ambiguous cases?
	// The plan says: "aux-generator classifier only on ambiguous state-changing messages"
	// But what defines an "ambiguous state-changing message"?
	// If it doesn't clearly match unlock/lock, it's Neutral.
	// Actually, wait. "A deterministic phrase table handles the common cases at zero cost; the aux generator... is consulted only when the phrase table is uncertain and the verdict would change gate state."
	// How do we know it is uncertain? If it's short and contains "change", "write", "edit", etc?
	// It's probably easier to just use the LLM if we want to be "EXTREMELY smart" as the prompt said.
	// Let's implement the aux generator call.

	if aux == nil {
		return IntentNeutral
	}

	// We only want to call the LLM if it seems remotely related to permissions or actions.
	// If the input is just "hello" or "what is this code", it's neutral.
	// We look for words like: change, write, edit, proceed, apply, touch, modify.
	keywords := []string{"change", "write", "edit", "proceed", "apply", "touch", "modify"}
	hasKeyword := false
	for _, kw := range keywords {
		if strings.Contains(stripped, kw) {
			hasKeyword = true
			break
		}
	}
	if !hasKeyword {
		return IntentNeutral
	}

	sysPrompt := "Classify the user's intent regarding whether you (the AI agent) are allowed to modify files or run mutating commands.\n" +
		"Respond with EXACTLY ONE WORD from this list:\n" +
		"LOCK - The user is forbidding you from making changes (e.g., 'just look', 'don't edit anything').\n" +
		"UNLOCK - The user is granting permission to make changes (e.g., 'go ahead', 'do it', 'apply the fix').\n" +
		"NEUTRAL - The user is just asking a question, making a statement, or the intent is unrelated to permissions.\n" +
		"If unsure, respond NEUTRAL."

	req := &provider.GenerateRequest{
		SystemInstruction: sysPrompt,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Parts: []provider.Part{{Text: input}}},
		},
		Temperature: new(float64),
	}

	stream, err := aux.GenerateContentStream(ctx, req)
	if err != nil {
		return IntentNeutral
	}

	var b strings.Builder
	for chunk := range stream {
		if chunk.Error != nil {
			return IntentNeutral
		}
		b.WriteString(chunk.TextDelta)
	}

	result := strings.TrimSpace(b.String())
	if strings.Contains(result, "LOCK") {
		return IntentLock
	}
	if strings.Contains(result, "UNLOCK") {
		return IntentUnlock
	}

	return IntentNeutral
}

func stripQuotes(s string) string {
	var b strings.Builder
	inDouble := false
	inSingle := false
	inBacktick := false

	for _, c := range s {
		switch c {
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
				continue
			}
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
				continue
			}
		case '`':
			if !inDouble && !inSingle {
				inBacktick = !inBacktick
				continue
			}
		}
		if !inDouble && !inSingle && !inBacktick {
			b.WriteRune(c)
		}
	}
	return b.String()
}
