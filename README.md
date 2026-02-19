

# What is this repository?

This repository is a personal collection of `agent.md` files designed for use with GitHub Copilot in VSCode. It currently contains the following:

| Name | Purpose |
|------|---------|
| orchestrator.agent.md | Oversees all sub-agents. Invokes each sub-agent via `#tool:agent/runSubagent`. |
| plan.agent.md | Reads the codebase, formulates an implementation plan, and returns it to the orchestrator. Essentially read-only. |
| implementation.agent.md | Carries out the actual implementation based on the plan passed down from the orchestrator. |
| review-alpha.agent.md | Reviews the implementation. Alpha's model is set to GPT-5.3-Codex. |
| review-beta.agent.md | Reviews the implementation. Beta's model is set to Claude Opus 4.6, ensuring diversity in the review process. |

All of these agents are defined with the assumption that an MCP called `artifact-mcp` is available in their toolset. This local artifact system is designed to prevent the orchestrator from becoming a bottleneck by having to relay sub-agent outputs; for example, having subagent-plan return a plan directly to the orchestrator, which then passes it verbatim, word for word, to subagent-impl (which amounts to nothing more than a scaling bottleneck). Furthermore, unlike the existing VS Code built-in TODO tool, a artifact-mcp/todo tool tied to an artifact-plan that persists across sessions (rather than just a chat session) allows subsequent sub-agents or orchestrators to pick up where things left off and track progress, even if a sub-agent crashes due to an error.



The idea is inspired by Antigravity's artifact concept.

## How to Install

### Build from source

Prerequisites:
- Go toolchain

Commands:

```bash
cd ccsubagents
go build -o ccsubagents ./cmd/ccsubagents
./ccsubagents install
```

### Use the prebuilt binary

1. Download the latest `ccsubagents` binary from this repository's Releases page.
2. Mark it executable and run install:

```bash
chmod +x ./ccsubagents
./ccsubagents install
```

## What's the benefit?

LLMs are not perfect (nor, for that matter, is any intelligent being). They sometimes fail tool calls, or inspect files they didn't need to look at — and unlike a human who can simply forget, that information lingers in the context. (On that note, it would be interesting to have a tool that lets the LLM itself omit tool-call results it deems unnecessary, with a stated reason.)

This is what's commonly referred to as *context pollution* or *context congestion*. By splitting work into sub-agents, you can — with the caveat that this only applies as of the moment each sub-agent is dispatched — largely mitigate context bloat and pollution. (Granted, the longer a sub-agent runs, the more its own context gets polluted, but it's still far better than cramming planning, implementation, and review all into a single session.)