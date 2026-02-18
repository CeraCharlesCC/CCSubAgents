

# What is this repository?

This repository is a personal collection of `agent.md` files designed for use with GitHub Copilot in VSCode, along with a companion MCP server (`artifact-mcp`) that enables artifact-based communication between agents.

The idea is inspired by Antigravity's artifact concept.

### Agents

| Name | Purpose |
|------|---------|
| orchestrator.agent.md | Oversees all sub-agents. Invokes each sub-agent via `#tool:agent/runSubagent`. |
| plan.agent.md | Reads the codebase, formulates an implementation plan, and returns it to the orchestrator. Essentially read-only. |
| implementation.agent.md | Carries out the actual implementation based on the plan passed down from the orchestrator. |
| review-alpha.agent.md | Reviews the implementation. Alpha's model is set to GPT-5.3-Codex. |
| review-beta.agent.md | Reviews the implementation. Beta's model is set to Claude Opus 4.6, ensuring diversity in the review process. |

### Artifact-MCP

All agents are defined with the assumption that an MCP called `artifact-mcp` is available in their toolset. This local artifact system is designed to prevent the orchestrator from becoming a bottleneck by having to relay sub-agent outputs — for example, having subagent-plan return a plan directly to the orchestrator, which then passes it verbatim to subagent-impl (which amounts to nothing more than a scaling bottleneck). Instead, sub-agents write their outputs to named artifacts and only pass the artifact **name** through the orchestrator, keeping context lean.

### TODO Tracking

The artifact system also ships with `artifact-mcp/todo`, which provides cross-session context preservation. Unlike VSCode's built-in TODO tool (which is tied to a single chat session and does not carry over), artifact-mcp TODOs are bound to an existing artifact and persist across sessions and agents. For example, even if the implementation sub-agent crashes mid-way, the orchestrator or a successor sub-agent can easily track progress and resume from where it left off.

### Design Philosophy

As a guiding principle, these agent definitions specify *what* to produce but deliberately avoid prescribing strict output formats. This serves two purposes: it reduces the context footprint of the `agent.md` files themselves, and it reflects trust in modern state-of-the-art models — the belief that they are capable of producing sufficiently structured output without rigid formatting constraints.

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