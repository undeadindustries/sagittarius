package contextmgmt

// Compression summarization instructions. The system prompt is a faithful,
// concise port of the fork getCompressionPrompt; the approved-plan-path
// injection is deferred (plan mode is not yet ported — see AD-015).
const (
	newSnapshotInstruction      = "Generate a new <state_snapshot> based on the provided history."
	anchoredSnapshotInstruction = "A previous <state_snapshot> exists in the history. You MUST integrate all still-relevant information from that snapshot into the new one, updating it with the more recent events. Do not lose established constraints or critical knowledge."
	verificationInstruction     = "Critically evaluate the <state_snapshot> you just generated. Did you omit any specific technical details, file paths, tool results, or user constraints mentioned in the history? If anything is missing or could be more precise, generate a FINAL, improved <state_snapshot>. Otherwise, repeat the exact same <state_snapshot> again."
)

// DefaultCompressionPrompt is the system instruction sent to the summarizer.
// It instructs the model to distill the conversation into a structured
// <state_snapshot> that preserves goals, constraints, decisions, and file/tool
// state so the agent can continue without the raw history.
const DefaultCompressionPrompt = `You are the component that summarizes internal chat history into a given structure.

When the conversation history grows too large, you will be invoked to distill the entire history into a concise, structured XML snapshot. This snapshot is CRITICAL, as it will become the agent's only memory of the past. The agent will resume work based solely on this snapshot. All crucial details, plans, errors, and user directives MUST be preserved.

First, you will think through the entire history in a private <scratchpad>. Review the user's overall goal, the agent's actions, tool outputs, file modifications, and any unresolved questions. Identify every piece of information that is essential for future actions.

After your reasoning is complete, generate the final <state_snapshot> XML object. Be incredibly dense with information. Omit any irrelevant conversational filler.

The structure MUST be as follows:

<state_snapshot>
    <overall_goal>The single, overarching objective the user wants to achieve.</overall_goal>
    <key_knowledge>Crucial facts, conventions, constraints, and decisions the agent must remember.</key_knowledge>
    <file_system_state>Files created, read, modified, or deleted, with a brief status of each.</file_system_state>
    <recent_actions>A summary of the most recent significant actions and their outcomes.</recent_actions>
    <current_plan>The step-by-step plan, marking completed steps and the next action.</current_plan>
</state_snapshot>`
