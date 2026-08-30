package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/undeadindustries/sagittarius/internal/config"
	"github.com/undeadindustries/sagittarius/internal/contextmgmt"
	"github.com/undeadindustries/sagittarius/internal/modes"
	"github.com/undeadindustries/sagittarius/internal/provider"
	"github.com/undeadindustries/sagittarius/internal/session"
	"github.com/undeadindustries/sagittarius/internal/snapshot"
	"github.com/undeadindustries/sagittarius/internal/tools"
	"github.com/undeadindustries/sagittarius/internal/ui"
)

// SubagentGeneratorFunc builds the content generator for a child agent. A
// child never shares the parent's: the OpenAI Responses adapter chains turns
// off its own last response id, and sharing one would splice the child's turns
// into the parent's conversation (AD-058).
type SubagentGeneratorFunc func(ctx context.Context, settings *config.Settings) (provider.ContentGenerator, error)

// subagentSpec is everything that differs between the research and coding
// classes. Everything else about launching a child is shared.
type subagentSpec struct {
	description string
	mode        modes.Mode
	class       config.SubagentClass
	lease       *tools.WriteLease
	charter     string
	snapshotter *snapshot.Manager
	approval    ApprovalMode
}

// subagent is a launched child and its identity.
type subagent struct {
	runner *Runner
	id     string
	desc   string
}

// checkSubagentDepth refuses a second level of nesting. Depth 1 is a deliberate
// limit: a tree of agents multiplies cost and makes a runaway loop hard to see.
func (r *Runner) checkSubagentDepth() error {
	if r.isSubagent() {
		return fmt.Errorf("subagents cannot launch further subagents (depth limit 1)")
	}
	return nil
}

// newSubagent builds a child runner sharing the parent's runtime (MCP servers,
// background processes, file state) but with its own history, session
// recorder, context manager, and generator.
func (r *Runner) newSubagent(ctx context.Context, spec subagentSpec) (*subagent, error) {
	subID := uuid.New().String()
	root := r.workspace.Root()
	settings := r.settingsSnapshot()

	outputDir, _ := session.ChatsDir(root)
	ctxMgr := contextmgmt.NewManager(contextmgmt.ManagerConfig{
		Enabled:   true,
		SessionID: subID,
		OutputDir: outputDir,
	})

	var rec *session.Recorder
	if r.sessionRecorder != nil {
		if chatsDir, err := session.ChatsDir(root); err == nil {
			rec = session.NewRecorder(chatsDir, subID, session.ProjectHash(root), sessionKindSubagent)
		}
	}

	gen, err := r.newSubagentGenerator(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("subagent generator: %w", err)
	}

	child, err := NewRunner(RunnerConfig{
		Model: config.SubagentModel(spec.class, r.sagittariusSettings(), r.Model()),
		// Pinned so a mode override on the parent — which may route agent mode
		// to an engine chosen for something else entirely — cannot capture the
		// child through mode resolution.
		ModelPinned:       true,
		WorkDir:           root,
		ApprovalMode:      spec.approval,
		Interactive:       false,
		ContextManager:    ctxMgr,
		SessionRecorder:   rec,
		Settings:          settings,
		ProjectBoundary:   r.projectBoundary,
		Snapshotter:       spec.snapshotter,
		InitialMode:       spec.mode,
		Runtime:           r.runtime,
		SpillDir:          r.spillDir,
		ScriptToolEnabled: config.ScriptToolEnabled(settings, nil),
		Generator:         gen,
		WriteLease:        spec.lease,
		AgentID:           subID,
		FileState:         r.fileState,
		SubagentCharter:   spec.charter,
		SubagentGenerator: r.newSubagentGenerator,
	})
	if err != nil {
		return nil, fmt.Errorf("create subagent runner: %w", err)
	}
	slog.Info("launching subagent", "description", spec.description, "sessionID", subID,
		"class", spec.class, "mode", spec.mode)
	return &subagent{runner: child, id: subID, desc: spec.description}, nil
}

// run drives one turn to completion and returns the child's final message,
// forwarding coarse progress to the parent's tool card. Tool-level detail stays
// inside the child: isolating it is the point of a subagent.
func (s *subagent) run(ctx context.Context, prompt string, sink tools.ToolOutputSink) (string, error) {
	stream, err := s.runner.RunTurn(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("subagent %q failed: %w", s.desc, err)
	}
	// A child reports a failed turn as a StreamError and then closes normally.
	// Without capturing it the parent would be handed "(subagent finished
	// without producing text)" and treat a dead child as a completed one.
	var failure string
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case ev, ok := <-stream:
			if !ok {
				if failure != "" {
					return "", fmt.Errorf("subagent %q failed: %s", s.desc, failure)
				}
				return s.finalText(), nil
			}
			if ev.Type == ui.StreamError && failure == "" {
				failure = streamErrorText(ev)
			}
			forwardSubagentProgress(ev, sink)
		}
	}
}

// streamErrorText reads a stream error, which carries its detail in Err for a
// wrapped failure and in Text for a synthesized one.
func streamErrorText(ev ui.StreamEvent) string {
	if ev.Err != nil {
		return ev.Err.Error()
	}
	if ev.Text != "" {
		return ev.Text
	}
	return "unknown error"
}

func (s *subagent) finalText() string {
	if text := s.runner.LastAssistantText(); text != "" {
		return text
	}
	return "(subagent finished without producing text)"
}

func forwardSubagentProgress(ev ui.StreamEvent, sink tools.ToolOutputSink) {
	switch ev.Type {
	case ui.StreamToolStart:
		sink("Running " + ev.ToolName + "…")
	case ui.StreamToolResult:
		sink("Finished " + ev.ToolName)
	case ui.StreamTextDelta, ui.StreamReasoningDelta:
		if ev.Text != "" {
			sink("Thinking…")
		}
	}
}

// writtenPaths reports the workspace-relative files this runner's agent wrote,
// so a parent can see what a child actually changed rather than trusting the
// child's own account of it.
func (r *Runner) writtenPaths() []string {
	abs := r.fileState.WrittenBy(r.agentID)
	rel := make([]string, 0, len(abs))
	for _, p := range abs {
		if s, err := r.workspace.RelativePath(p); err == nil {
			rel = append(rel, s)
			continue
		}
		rel = append(rel, p)
	}
	return rel
}
