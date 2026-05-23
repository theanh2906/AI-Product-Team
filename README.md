# Mini AI Team Center

A lightweight Go-based orchestrator triggered by GitHub workflows that coordinates a virtual AI agent team (Team Lead, Developer) to process repository issues and implement/validate solutions automatically.

## 📂 Project Structure

```
📂 AI-Product-Team/
├── 📂 .github/
│   └── 📂 workflows/
│       └── 📄 ai-orchestrator.yml  # GitHub Actions workflow triggered on Issues
├── 📂 cmd/
│   └── 📄 main.go                  # Core orchestrator entry point
├── 📂 internal/
│   ├── 📂 agent/                   # Defines AI Agents (Team Lead, Developer, etc.)
│   │   ├── 📄 team_lead.go
│   │   └── 📄 developer.go
│   └── 📂 github/                  # Wrapper for GitHub API operations
│       └── 📄 client.go
├── 📄 go.mod                       # Go module definition
└── 📄 README.md                    # Project documentation
```

## 🚀 How It Works

1. **GitHub Trigger**: A GitHub issue is opened or edited.
2. **Workflow Activation**: `.github/workflows/ai-orchestrator.yml` fires, setting up a Go environment and passing the issue event payload to the orchestrator.
3. **Team Lead Phase**: The Team Lead agent reviews the issue title/description and produces an implementation plan.
4. **Developer Phase**: The Developer agent takes the plan and runs tasks (generating patches/files).
5. **Issue Response**: The GitHub Client writes back a comment summarizing the agent operations onto the issue.

## 🛠️ Local Development & Execution

To test the orchestrator locally without a GitHub action environment, simply run:

```bash
go run cmd/main.go
```

This runs the application in **Mock Mode**, simulating an issue flow and logging output to the terminal.
