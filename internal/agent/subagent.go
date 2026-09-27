package agent

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/undeadindustries/sagittarius/internal/config"
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
	// roundCapped is set when the child's turn ended by hitting max tool
	// rounds rather than finishing. The tools report that as an incomplete
	// hand-off (with whatever the child did manage) instead of an error.
	roundCapped bool
}

// roundCapMarker is the terminal stream error a turn emits when the tool-round
// cap stops it (see runAgentLoop). A child that ends this way did partial work;
// the hand-off reports it as incomplete rather than failed.
const roundCapMarker = "max tool rounds exceeded"

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
// recorder, context manager, and generator. The child's (provider, model) pair
// comes from config.ResolveSubagentTarget: a class or default pin may route it
// to a different provider than the parent, in which case the generator and the
// context manager are built from a settings clone with that pair forced active
// (the auxGenerator pattern). A pin that cannot be built fails here with the
// slot and pair named; it never falls back to the parent's model.
func (r *Runner) newSubagent(ctx context.Context, spec subagentSpec) (*subagent, error) {
	subID := uuid.New().String()
	root := r.workspace.Root()
	settings := r.settingsSnapshot()

	target := config.ResolveSubagentTarget(spec.class, r.sagittariusSettings(), r.activeProviderID(), r.Model())
	childSettings := settings
	if target.Source != config.SubagentTargetLive {
		clone, err := settingsForTarget(settings, target.Provider, target.Model)
		if err != nil {
			return nil, fmt.Errorf("subagent %s target %s/%s: %w (see /subagents)", spec.class, target.Provider, target.Model, err)
		}
		childSettings = clone
	}
	effectiveProvider := target.Provider
	if effectiveProvider == "" {
		effectiveProvider = r.activeProviderID()
	}

	var rec *session.Recorder
	if r.sessionRecorder != nil {
		if chatsDir, err := session.ChatsDir(root); err == nil {
			rec = session.NewRecorder(chatsDir, subID, session.ProjectHash(root), sessionKindSubagent)
		}
	}

	gen, err := r.newSubagentGenerator(ctx, childSettings)
	if err != nil {
		return nil, fmt.Errorf("subagent %s target %s/%s: generator: %w (see /subagents)", spec.class, effectiveProvider, target.Model, err)
	}

	child, err := NewRunner(RunnerConfig{
		Model: target.Model,
		// Pinned so a mode override on the parent — which may route agent mode
		// to an engine chosen for something else entirely — cannot capture the
		// child through mode resolution.
		ModelPinned:       true,
		WorkDir:           root,
		ApprovalMode:      spec.approval,
		Interactive:       false,
		SessionRecorder:   rec,
		Settings:          childSettings,
		ProjectBoundary:   r.projectBoundary,
		Snapshotter:       spec.snapshotter,
		InitialMode:       spec.mode,
		Runtime:           r.runtime,
		SpillDir:          r.spillDir,
		ScriptToolEnabled: config.ScriptToolEnabled(childSettings, nil),
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
	if mgr := NewContextManager(childSettings, gen, child.Model, child.ActiveProviderID,
		func() string { return child.InteractionMode().String() },
		subID, child.RecordUsage, child.OnWillCompress); mgr != nil {
		child.SetContextManager(mgr)
	}
	slog.Info("launching subagent", "description", spec.description, "sessionID", subID,
		"class", spec.class, "mode", spec.mode,
		"provider", effectiveProvider, "model", target.Model, "source", string(target.Source))
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
	// A round-cap error additionally arms roundCapped so the hand-off reports
	// incomplete (with partial work) instead of failed.
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
				if strings.Contains(failure, roundCapMarker) {
					s.roundCapped = true
				}
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
