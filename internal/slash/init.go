package slash

import (
	"fmt"
	"os"
	"path/filepath"
)

// initAnalysisPrompt instructs the agent to explore the project with its tools
// and write a comprehensive AGENTS.md. Adapted from gemini-cli's /init prompt
// for Sagittarius (AGENTS.md instead of GEMINI.md).
const initAnalysisPrompt = `You are an AI coding agent. Your task is to analyze the current project directory and generate a comprehensive AGENTS.md file to be used as instructional context for future interactions.

**Analysis Process:**

1. **Initial Exploration:**
   - List the files and directories to get a high-level overview of the structure.
   - Read the README (e.g., README.md) if it exists — it is often the best starting point.

2. **Iterative Deep Dive (up to 10 files):**
   - Based on your findings, read the most important files (configuration, main source files, documentation). Let your discoveries guide which files to read next.

3. **Identify Project Type:**
   - Code project: look for go.mod, package.json, requirements.txt, pyproject.toml, Cargo.toml, pom.xml, build.gradle, or a src directory.
   - Non-code project: documentation, research, notes, etc.

**AGENTS.md Content:**

For a code project, cover:
- **Project Overview:** purpose, main technologies, and architecture.
- **Building and Running:** the key build/run/test commands, inferred from the files you read (package.json scripts, Makefile, etc.). Use a TODO placeholder if you cannot find them.
- **Development Conventions:** coding style, testing practices, and contribution guidelines you can infer.

For a non-code project, cover the directory's purpose, its key files, and how its contents are intended to be used.

**Final Output:**

Write the complete, well-formatted Markdown content to the AGENTS.md file in the project root using your file-writing tool.`

func initCommand() Command {
	return Command{
		Name:        "init",
		Description: "Analyze the project and generate a tailored AGENTS.md context file",
		Handler:     handleInit,
	}
}

// handleInit creates an empty AGENTS.md (when absent) and submits an analysis
// prompt so the agent populates it with its tools. When AGENTS.md already exists
// it makes no changes, mirroring gemini-cli.
func handleInit(ctx *Context) Result {
	if ctx.Deps.Hooks == nil {
		return InfoResult("Init unavailable.")
	}
	target := filepath.Join(ctx.Deps.Hooks.WorkDir(), "AGENTS.md")
	switch _, err := os.Stat(target); {
	case err == nil:
		return InfoResult("An AGENTS.md file already exists in this directory. No changes were made.")
	case !os.IsNotExist(err):
		return ErrorResult(fmt.Errorf("check AGENTS.md: %w", err))
	}
	if err := os.WriteFile(target, []byte{}, 0o644); err != nil {
		return ErrorResult(fmt.Errorf("create AGENTS.md: %w", err))
	}
	return Result{
		Handled:      true,
		Messages:     []string{"Empty AGENTS.md created. Analyzing the project to populate it."},
		SubmitPrompt: initAnalysisPrompt,
	}
}
