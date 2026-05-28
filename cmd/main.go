package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/theanh2906/AI-Product-Team/internal/agent"
	ghWrapper "github.com/theanh2906/AI-Product-Team/internal/github"

	"github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

// loadEnv loads environment variables from a local .env file if it exists.
func loadEnv() {
	file, err := os.Open(".env")
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			// Strip quotes if present
			if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") {
				val = val[1 : len(val)-1]
			} else if strings.HasPrefix(val, "'") && strings.HasSuffix(val, "'") {
				val = val[1 : len(val)-1]
			}
			if os.Getenv(key) == "" {
				os.Setenv(key, val)
			}
		}
	}
}

func main() {
	// Load local environment variables from .env if present
	loadEnv()

	// 1. Retrieve environment variables
	githubToken := os.Getenv("GITHUB_TOKEN")
	orchestratorRepo := os.Getenv("GITHUB_REPOSITORY")
	owner := "theanh2906"
	issueNumStr := os.Getenv("ISSUE_NUMBER")
	issueBody := os.Getenv("ISSUE_BODY")
	issueTitle := os.Getenv("ISSUE_TITLE")

	// Product Repo name passed from the Workflow
	productRepoName := os.Getenv("PRODUCT_REPO_NAME")

	// Environment variables for Kanban Board
	projectNumStr := os.Getenv("PROJECT_NUMBER")
	projectOwner := "theanh2906"

	ctx := context.Background()

	// Initialize GitHub Client
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: githubToken})
	tc := oauth2.NewClient(ctx, ts)
	ghClient := github.NewClient(tc)

	var orchestratorRealName string
	if parts := strings.Split(orchestratorRepo, "/"); len(parts) == 2 {
		orchestratorRealName = parts[1]
	} else {
		orchestratorRealName = orchestratorRepo
	}
	issueNumber, _ := strconv.Atoi(issueNumStr)
	// 1.3 Determine product repo dir and auto-clone if needed
	productRepoDir := os.Getenv("TEST_DIR")
	if (productRepoDir == "" || productRepoDir == ".") && productRepoName != "" && productRepoName != orchestratorRealName {
		productRepoDir = productRepoName
		os.Setenv("TEST_DIR", productRepoDir)
	}

	if productRepoDir != "" && productRepoDir != "." {
		if _, err := os.Stat(filepath.Join(productRepoDir, ".git")); os.IsNotExist(err) {
			fmt.Printf(" [System]: Product repository directory %s does not exist. Cloning...\n", productRepoDir)
			cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", githubToken, owner, productRepoName)
			if err := runGitCommand(".", "clone", cloneURL, productRepoDir); err != nil {
				fmt.Printf("❌ Failed to clone Product Repository: %v\n", err)
				os.Exit(1)
			}
		}
	}

	// 1.2 Check if running in Kanban State-Machine mode (PROJECT_ITEM_ID)
	projectItemID := os.Getenv("PROJECT_ITEM_ID")
	if projectItemID != "" {
		runKanbanStateMachineFlow(ctx, ghClient, githubToken, projectItemID, projectNumStr, projectOwner)
		return
	}

	// 1.1 Check run mode (Orchestrator Mode)
	orchestratorMode := strings.ToLower(os.Getenv("ORCHESTRATOR_MODE"))
	if orchestratorMode == "qa" {
		runQAAgentFlow(ctx, ghClient, githubToken, owner, productRepoName, orchestratorRealName, issueNumber, issueTitle, projectNumStr, projectOwner)
		return
	}

	// 1.4 Kanban Polling Mode: If both PROJECT_ITEM_ID and ISSUE_NUMBER are empty, run the Kanban Polling mode!
	if projectItemID == "" && issueNumber == 0 {
		fmt.Println(" [Kanban Polling]: No specific issue or project item ID provided. Scanning Project Board for Todo tasks...")
		runKanbanPollingFlow(ctx, ghClient, githubToken, projectNumStr, projectOwner)
		return
	}

	// 1.5 Check if this is a Developer Agent child task triggered directly (e.g. by labeling it with ai-process)
	if strings.Contains(issueTitle, "[Senior Fullstack Engineer]") {
		fmt.Println(" [Orchestrator]: Detected child task for Senior Fullstack Engineer. Running Developer Agent directly...")

		// Find expected branch name from issueBody
		branchName := ""
		for _, line := range strings.Split(issueBody, "\n") {
			if strings.Contains(line, "Expected branch:") {
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					rawBranch := strings.TrimSpace(parts[1])
					rawBranch = strings.Trim(rawBranch, "*` ")
					if rawBranch != "" {
						branchName = rawBranch
						break
					}
				}
			}
		}
		if branchName == "" {
			branchName = sanitizeBranchName(fmt.Sprintf("task-%d-%s", issueNumber, issueTitle))
		}

		devAgent := agent.NewDeveloper()
		summaryReport, err := devAgent.DevelopTask(ctx, ghClient, githubToken, owner, productRepoName, issueNumber, issueTitle, issueBody, productRepoDir, branchName)
		if err != nil {
			fmt.Printf("❌ AI Developer coding failed: %v\n", err)
			errorComment := fmt.Sprintf(" **[AI Developer Agent Report]**\n\n❌ **Code generation process failed!**\n\n*Error details:* `%v`", err)
			_, _, _ = ghClient.Issues.CreateComment(ctx, owner, orchestratorRealName, issueNumber, &github.IssueComment{Body: github.String(errorComment)})
			os.Exit(1)
		}

		// Post the report as a comment on the child Issue
		_, _, _ = ghClient.Issues.CreateComment(ctx, owner, orchestratorRealName, issueNumber, &github.IssueComment{Body: github.String(summaryReport)})
		return
	}

	// 2. Call GitHub Models API (Team Lead)
	fmt.Println(" [Team Lead Agent]: Sending context to GitHub Models for analysis...")
	modelName := os.Getenv("TEAM_LEAD_MODEL")
	if modelName == "" {
		modelName = os.Getenv("AI_MODEL")
	}
	aiClient := agent.NewLLMClient(githubToken, modelName)

	systemInstruction := `You are an outstanding AI Team Lead and Business Analyst (PM). Your task is to break down Issues into detailed technical Tasks in JSON format. Assign all technical development tasks (assignee) to "Senior Fullstack Engineer".`
	prompt := fmt.Sprintf("Please analyze the following request from PM Ben:\nTitle: %s\nDetailed content:\n%s", issueTitle, issueBody)

	schema := &agent.JSONSchema{
		Name:   "team_lead_response",
		Strict: true,
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"analysis": map[string]interface{}{
					"type": "string",
				},
				"tasks": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"title": map[string]interface{}{
								"type": "string",
							},
							"description": map[string]interface{}{
								"type": "string",
							},
							"assignee": map[string]interface{}{
								"type": "string",
							},
							"branch_name": map[string]interface{}{
								"type": "string",
							},
							"depends_on": map[string]interface{}{
								"type":  "array",
								"items": map[string]interface{}{"type": "string"},
							},
						},
						"required":             []string{"title", "description", "assignee", "branch_name", "depends_on"},
						"additionalProperties": false,
					},
				},
			},
			"required":             []string{"analysis", "tasks"},
			"additionalProperties": false,
		},
	}

	respText, err := aiClient.GenerateContent(ctx, systemInstruction, prompt, schema)
	if err != nil {
		fmt.Printf("❌ GitHub Models call error: %v\n", err)
		os.Exit(1)
	}

	var aiResult agent.AIResponse
	json.Unmarshal([]byte(respText), &aiResult)

	// 3. AUTOMATICALLY CREATE BRANCHES IN THE PRODUCT REPO
	fmt.Printf(" [Team Lead Agent]: Starting to initialize branches in Repo: %s...\n", productRepoName)

	// 3.1 The default base branch is 'main'
	defaultBranch := "main"

	// 3.2 Get the SHA of the current main branch in the Product Repo to use as the base
	ref, _, err := ghClient.Repositories.GetBranch(ctx, owner, productRepoName, defaultBranch, 0)
	if err != nil {
		fmt.Printf("❌ Failed to get branch %s info from Product Repo: %v. Make sure the repo name is correct and the repo is not empty.\n", defaultBranch, err)
		os.Exit(1)
	}
	baseSHA := ref.GetCommit().GetSHA()
	fmt.Printf(" Base SHA of branch %s is: %s\n", defaultBranch, baseSHA)

	// Create the parent branch (Branch A) for the PM's issue: ai-implement/issue-<issueNumber>
	parentBranchName := fmt.Sprintf("ai-implement/issue-%d", issueNumber)
	parentRefString := fmt.Sprintf("refs/heads/%s", parentBranchName)
	parentRef := &github.Reference{
		Ref:    github.String(parentRefString),
		Object: &github.GitObject{SHA: github.String(baseSHA)},
	}
	_, _, err = ghClient.Git.CreateRef(ctx, owner, productRepoName, parentRef)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			fmt.Printf("ℹ️ Parent branch %s already exists\n", parentBranchName)
		} else {
			fmt.Printf("⚠️ Failed to create parent branch %s: %v\n", parentBranchName, err)
		}
	} else {
		fmt.Printf("✅ Parent branch %s successfully created\n", parentBranchName)
	}

	// 3.3 Iterate through the task array to create each corresponding branch
	createdBranchesReport := "\n###  Git Branch Initialization Status in Product Repo:\n"
	seenBranches := make(map[string]bool)

	for _, task := range aiResult.Tasks {
		// Normalize branch name (remove spaces if the AI accidentally creates an invalid name)
		cleanBranchName := strings.ReplaceAll(task.BranchName, " ", "-")

		if seenBranches[cleanBranchName] {
			continue
		}
		seenBranches[cleanBranchName] = true

		// Define the Ref structure on GitHub (must have the "refs/heads/" prefix)
		refString := fmt.Sprintf("refs/heads/%s", cleanBranchName)

		newRef := &github.Reference{
			Ref:    github.String(refString),
			Object: &github.GitObject{SHA: github.String(baseSHA)},
		}

		// Call GitHub API to create a new branch
		_, _, err := ghClient.Git.CreateRef(ctx, owner, productRepoName, newRef)
		if err != nil {
			// If the branch already exists (from a previous run), we log it instead of crashing the system
			if strings.Contains(err.Error(), "already exists") {
				createdBranchesReport += fmt.Sprintf("- ` %s`: Duplicate name (Already exists) ⚠️\n", cleanBranchName)
			} else {
				createdBranchesReport += fmt.Sprintf("- ` %s`: Creation failed (Error: %v) ❌\n", cleanBranchName, err)
			}
		} else {
			createdBranchesReport += fmt.Sprintf("- ` %s`: Successfully initialized for **[%s]** ✅\n", cleanBranchName, task.Assignee)
		}
	}

	// 4. CREATE CHILD ISSUES AND LINK TO KANBAN BOARD
	createdIssuesReport := ""

	if projectNumStr != "" {
		projectNum, err := strconv.Atoi(projectNumStr)
		if err != nil {
			fmt.Printf("⚠️ Warning: PROJECT_NUMBER is invalid (%s): %v\n", projectNumStr, err)
		} else {
			fmt.Printf(" [Kanban Integration]: Initializing connection to Project v2 (#%d) for owner: %s...\n", projectNum, projectOwner)

			// Initialize our wrapper client
			wrapperClient := ghWrapper.NewClient(githubToken)

			// Get Project ID
			projectID := "4"
			var err error
			if err != nil {
				fmt.Printf("❌ Failed to get Project ID for Project #%d: %v\n", projectNum, err)
			} else {
				fmt.Printf(" Found Project ID: %s\n", projectID)

				// Get Status field and "Todo" option
				statusFieldID, options, err := wrapperClient.GetProjectV2StatusOptions(ctx, projectID)
				var todoOptionID string
				if err == nil {
					if id, ok := ghWrapper.GetTodoStatusOptionID(options); ok {
						todoOptionID = id
					}
				}
				if err != nil || todoOptionID == "" {
					fmt.Printf("⚠️ Warning: Status/Todo column not found on Board: %v. Cards will be placed in the default column.\n", err)
				}

				createdIssuesReport = "\n###  Task Creation & Kanban Board Status in Product Repo:\n"

				for _, task := range aiResult.Tasks {
					// 4.1 Create a new Issue in the Product Repo
					issueTitle := fmt.Sprintf("[%s] %s", task.Assignee, task.Title)
					issueBody := fmt.Sprintf("%s\n\n---\n*Task assigned to: %s*\n*Expected branch: `%s`*\n*Parent Branch: `%s`*", task.Description, task.Assignee, task.BranchName, parentBranchName)

					issueReq := &github.IssueRequest{
						Title:  github.String(issueTitle),
						Body:   github.String(issueBody),
						Labels: &[]string{"ai-process", fmt.Sprintf("repo:%s", productRepoName)},
					}

					createdIssue, _, err := ghClient.Issues.Create(ctx, owner, productRepoName, issueReq)
					if err != nil {
						createdIssuesReport += fmt.Sprintf("- **%s**: Issue creation failed (Error: %v) ❌\n", task.Title, err)
						continue
					}

					issueNum := createdIssue.GetNumber()
					issueNodeID := createdIssue.GetNodeID()
					fmt.Printf("✅ Created Issue #%d for task: %s\n", issueNum, task.Title)

					// 4.2 Add Issue to Kanban Board and move to Todo column
					_, err = wrapperClient.CreateKanbanCardByIssueNodeID(ctx, projectID, statusFieldID, todoOptionID, issueNodeID)
					if err != nil {
						createdIssuesReport += fmt.Sprintf("- **%s**: Issue [#%d](https://github.com/%s/%s/issues/%d) created but failed to link to Kanban board (Error: %v) ⚠️\n",
							task.Title, issueNum, owner, productRepoName, issueNum, err)
					} else {
						createdIssuesReport += fmt.Sprintf("- **%s**: Issue [#%d](https://github.com/%s/%s/issues/%d) created and added to Kanban Board successfully! ✅\n",
							task.Title, issueNum, owner, productRepoName, issueNum)
					}
				}
			}
		}
	} else {
		fmt.Println("ℹ️ PROJECT_NUMBER is not configured. Skipping child Task creation on Kanban Board.")
	}

	// 5. Compile the final Markdown report to send to Ben
	markdownReport := fmt.Sprintf(" **[Team Lead & BA Agent Report]**\n\n###  Overall Analysis:\n%s\n\n###  Breakdown Task List:\n", aiResult.Analysis)
	for i, task := range aiResult.Tasks {
		markdownReport += fmt.Sprintf("%d. **[%s]** %s\n", i+1, task.Assignee, task.Title)
		markdownReport += fmt.Sprintf("   - *Description:* %s\n", task.Description)
		markdownReport += fmt.Sprintf("   - *Branch:* `%s`\n", task.BranchName)
		markdownReport += "\n"
	}

	// Append the branch creation status report to the comment
	markdownReport += createdBranchesReport

	// Append the Kanban task creation report
	if createdIssuesReport != "" {
		markdownReport += createdIssuesReport
	}

	comment := &github.IssueComment{Body: github.String(markdownReport)}
	ghClient.Issues.CreateComment(ctx, owner, orchestratorRealName, issueNumber, comment)

	fmt.Println(" Team Lead initialization cycle completed successfully!")
}

// runQAAgentFlow orchestrates the QA Agent test execution flow
func runQAAgentFlow(ctx context.Context, ghClient *github.Client, githubToken, owner, productRepoName, orchestratorRealName string, issueNumber int, issueTitle string, projectNumStr, projectOwner string) {
	fmt.Println(" [QA Agent]: Starting test mode...")

	// 1. Get test command configuration (Default: go test ./...)
	testCommand := os.Getenv("TEST_COMMAND")
	if testCommand == "" {
		testCommand = "go test ./..."
	}

	// 2. Run QA Agent to execute the test suite
	qaAgent := agent.NewQAAgent()
	testLog, pass, err := qaAgent.RunTests(ctx, testCommand)
	if err != nil {
		fmt.Printf("❌ Error running tests: %v\n", err)
		os.Exit(1)
	}

	var reportBody string
	var targetStatus string // "done" or "backlog"

	// 3. Process test results
	if pass {
		// Test Passed
		reportBody = fmt.Sprintf(" **[AI QA Agent Report]**\n\n✅ **Tests PASSED! (QA Passed)**\n\n- **Command:** `%s`\n\n### ️ Test Log Details:\n```text\n%s\n```", testCommand, testLog)
		targetStatus = "done"
	} else {
		// Test Failed -> Call GitHub Models for failure diagnosis
		diagnosis, diagErr := qaAgent.DiagnoseFailure(ctx, githubToken, testLog, issueTitle)
		if diagErr != nil {
			fmt.Printf("⚠️ Warning: Failed to call GitHub Models for failure diagnosis: %v\n", diagErr)
			diagnosis = "*(Could not retrieve automatic failure diagnosis from GitHub Models)*"
		}

		// 3.1 Create a new bug report Issue
		bugTitle := fmt.Sprintf("[QA Failed] Bug found: %s", issueTitle)
		bugBody := fmt.Sprintf("## ❌ Bug detected during automated testing\n\n**Related Task:** #%d\n**Test Command:** `%s`\n\n###  AI QA Failure Analysis:\n%s\n\n### ️ Test Log Details:\n```text\n%s\n```", issueNumber, testCommand, diagnosis, testLog)

		fmt.Printf(" [QA Agent]: Creating Bug Issue in Repo: %s...\n", productRepoName)
		bugReq := &github.IssueRequest{
			Title:  github.String(bugTitle),
			Body:   github.String(bugBody),
			Labels: &[]string{"bug", "qa-failed"},
		}

		createdBug, _, createBugErr := ghClient.Issues.Create(ctx, owner, productRepoName, bugReq)
		var bugIssueLink string
		if createBugErr != nil {
			fmt.Printf("❌ Failed to create Bug Issue on GitHub: %v\n", createBugErr)
			bugIssueLink = "*(Error creating automatic Bug Issue)*"
		} else {
			bugNum := createdBug.GetNumber()
			bugNodeID := createdBug.GetNodeID()
			bugIssueLink = fmt.Sprintf("[#%d](https://github.com/%s/%s/issues/%d)", bugNum, owner, productRepoName, bugNum)
			fmt.Printf("✅ Bug Issue #%d created successfully!\n", bugNum)

			// 3.2 Link this Bug Issue to the Kanban Board (first column like Backlog/PM)
			if projectNumStr != "" {
				projectNum, _ := strconv.Atoi(projectNumStr)
				_ = projectNum
				wrapperClient := ghWrapper.NewClient(githubToken)
				projectID := "4"
				var projErr error
				if projErr == nil {
					statusFieldID, options, optErr := wrapperClient.GetProjectV2StatusOptions(ctx, projectID)
					if optErr == nil {
						// Find the default column to place the Bug card
						var targetColID string
						if id, ok := ghWrapper.GetTodoStatusOptionID(options); ok {
							targetColID = id
						}

						if targetColID != "" {
							_, _ = wrapperClient.CreateKanbanCardByIssueNodeID(ctx, projectID, statusFieldID, targetColID, bugNodeID)
							fmt.Printf(" Bug card linked to Kanban board in the initial column.\n")
						}
					}
				}
			}
		}

		reportBody = fmt.Sprintf(" **[AI QA Agent Report]**\n\n❌ **Tests FAILED! (QA Failed)**\n\n- **Command:** `%s`\n- **Bug Issue created:** %s\n\n###  AI Failure Analysis:\n%s\n\n<details>\n<summary>View test log details</summary>\n\n```text\n%s\n```\n\n</details>", testCommand, bugIssueLink, diagnosis, testLog)
		targetStatus = "backlog"
	}

	// 4. Post the report as a comment on the current PR/Issue
	comment := &github.IssueComment{Body: github.String(reportBody)}
	_, _, commentErr := ghClient.Issues.CreateComment(ctx, owner, orchestratorRealName, issueNumber, comment)
	if commentErr != nil {
		fmt.Printf("❌ Failed to post QA report comment on GitHub: %v\n", commentErr)
	}

	// 5. Update the Kanban card status of the original Task/PR (if Kanban is configured)
	if projectNumStr != "" {
		projectNum, _ := strconv.Atoi(projectNumStr)
		_ = projectNum
		wrapperClient := ghWrapper.NewClient(githubToken)
		projectID := "4"
		var err error
		if err == nil {
			statusFieldID, options, err := wrapperClient.GetProjectV2StatusOptions(ctx, projectID)
			if err == nil {
				// Find the ID of the target column
				var targetColID string
				if id, ok := options[targetStatus]; ok {
					targetColID = id
				} else if targetStatus == "backlog" {
					if id, ok := ghWrapper.GetTodoStatusOptionID(options); ok {
						targetColID = id
					}
				}

				if targetColID != "" {
					// Get the Node ID of the original Issue/PR to find its corresponding Kanban Item ID
					refIssue, _, err := ghClient.Issues.Get(ctx, owner, orchestratorRealName, issueNumber)
					if err == nil {
						issueNodeID := refIssue.GetNodeID()
						itemID, err := wrapperClient.GetProjectV2ItemIDByContentID(ctx, projectID, issueNodeID)
						if err == nil {
							// Move the card to the corresponding column
							err = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, itemID, statusFieldID, targetColID)
							if err == nil {
								fmt.Printf(" Original Task status moved to: %s\n", targetStatus)
							} else {
								fmt.Printf("❌ Failed to update Task status on Kanban: %v\n", err)
							}
						} else {
							fmt.Printf("⚠️ Warning: Kanban Item not found for the current Task: %v\n", err)
						}
					}
				}
			}
		}
	}

	fmt.Println(" QA Agent test cycle completed!")
}

// runGitCommand executes a git command in the specified directory
func runGitCommand(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v failed: %w, output: %s", args, err, string(output))
	}
	return nil
}

// sanitizeBranchName normalizes the branch name using the formula: task-<issue_number>-<sanitized_title>
func sanitizeBranchName(title string) string {
	title = strings.ToLower(title)
	var sb strings.Builder
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '_' {
			sb.WriteRune('-')
		}
	}
	res := sb.String()
	// Replace multiple consecutive hyphens with a single one
	for strings.Contains(res, "--") {
		res = strings.ReplaceAll(res, "--", "-")
	}
	return strings.Trim(res, "-")
}

// runKanbanStateMachineFlow routes the execution flow based on the Kanban card status
func runKanbanStateMachineFlow(ctx context.Context, ghClient *github.Client, githubToken, projectItemID string, projectNumStr, projectOwner string) {
	fmt.Printf("️ [Kanban Router]: Activating State-Machine for card ID: %s\n", projectItemID)

	if projectNumStr == "" {
		fmt.Println("❌ Error: PROJECT_NUMBER is not configured.")
		os.Exit(1)
	}
	projectNum, err := strconv.Atoi(projectNumStr)
	_ = projectNum
	if err != nil {
		fmt.Printf("❌ Error: PROJECT_NUMBER is invalid: %v\n", err)
		os.Exit(1)
	}

	wrapperClient := ghWrapper.NewClient(githubToken)

	// 1. Get card details
	details, err := wrapperClient.GetProjectV2ItemDetails(ctx, projectItemID)
	if err != nil {
		fmt.Printf("❌ Error retrieving Kanban card details: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf(" [Kanban Router]: Card belongs to repo %s/%s, current column status: '%s'\n", details.RepoOwner, details.RepoName, details.Status)

	// 1.1 Determine product repo dir and auto-clone if needed
	orchestratorRepo := os.Getenv("GITHUB_REPOSITORY")
	var orchestratorRealName string
	if parts := strings.Split(orchestratorRepo, "/"); len(parts) == 2 {
		orchestratorRealName = parts[1]
	} else {
		orchestratorRealName = orchestratorRepo
	}

	productRepoDir := os.Getenv("TEST_DIR")
	if (productRepoDir == "" || productRepoDir == ".") && details.RepoName != "" && details.RepoName != orchestratorRealName {
		productRepoDir = details.RepoName
		os.Setenv("TEST_DIR", productRepoDir)
	}

	if productRepoDir != "" && productRepoDir != "." {
		if _, err := os.Stat(filepath.Join(productRepoDir, ".git")); os.IsNotExist(err) {
			fmt.Printf(" [Kanban Router]: Product repository directory %s does not exist. Cloning...\n", productRepoDir)
			cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", githubToken, details.RepoOwner, details.RepoName)
			if err := runGitCommand(".", "clone", cloneURL, productRepoDir); err != nil {
				fmt.Printf("❌ Failed to clone Product Repository: %v\n", err)
				os.Exit(1)
			}
		}
	}

	// Get Project ID
	projectID := "4"
	err = nil
	if err != nil {
		fmt.Printf("❌ Error getting Project ID: %v\n", err)
		os.Exit(1)
	}

	// Get the list of columns on the board
	statusFieldID, options, err := wrapperClient.GetProjectV2StatusOptions(ctx, projectID)
	if err != nil {
		fmt.Printf("❌ Error getting Status Options: %v\n", err)
		os.Exit(1)
	}

	statusNormalized := strings.ToLower(details.Status)

	switch statusNormalized {
	case "backlog", "todo":
		// Run Developer Agent (Senior Fullstack Engineer)
		runDeveloperAgentOnKanban(ctx, ghClient, wrapperClient, githubToken, details, projectID, statusFieldID, options)
	default:
		fmt.Printf("ℹ️ [Kanban Router]: Column '%s' has no automated action. Skipping.\n", details.Status)
	}
}

// runTeamLeadAgentOnKanban handles when the card is in the PM column: breaks it into child tasks and puts them in Backlog
func runTeamLeadAgentOnKanban(ctx context.Context, ghClient *github.Client, wrapperClient *ghWrapper.Client, githubToken string, details *ghWrapper.ProjectItemDetails, projectID, statusFieldID string, options map[string]string) {
	fmt.Println(" [Team Lead Agent]: Analyzing requirements and breaking down tasks...")

	modelName := os.Getenv("TEAM_LEAD_MODEL")
	if modelName == "" {
		modelName = os.Getenv("AI_MODEL")
	}
	aiClient := agent.NewLLMClient(githubToken, modelName)

	systemInstruction := `You are an outstanding AI Team Lead and Business Analyst (PM). Your task is to break down Issues into detailed technical Tasks in JSON format. Assign all technical development tasks (assignee) to "Senior Fullstack Engineer".`
	prompt := fmt.Sprintf("Please analyze the following request from PM Ben:\nTitle: %s\nDetailed content:\n%s", details.Title, details.Body)

	schema := &agent.JSONSchema{
		Name:   "team_lead_response",
		Strict: true,
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"analysis": map[string]interface{}{
					"type": "string",
				},
				"tasks": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"title": map[string]interface{}{
								"type": "string",
							},
							"description": map[string]interface{}{
								"type": "string",
							},
							"assignee": map[string]interface{}{
								"type": "string",
							},
							"branch_name": map[string]interface{}{
								"type": "string",
							},
							"depends_on": map[string]interface{}{
								"type":  "array",
								"items": map[string]interface{}{"type": "string"},
							},
						},
						"required":             []string{"title", "description", "assignee", "branch_name", "depends_on"},
						"additionalProperties": false,
					},
				},
			},
			"required":             []string{"analysis", "tasks"},
			"additionalProperties": false,
		},
	}

	respText, err := aiClient.GenerateContent(ctx, systemInstruction, prompt, schema)
	if err != nil {
		fmt.Printf("❌ GitHub Models call error: %v\n", err)
		os.Exit(1)
	}

	var aiResult agent.AIResponse
	json.Unmarshal([]byte(respText), &aiResult)

	// Get the SHA of the main branch to use as the base for branch creation
	defaultBranch := "main"
	ref, _, err := ghClient.Repositories.GetBranch(ctx, details.RepoOwner, details.RepoName, defaultBranch, 0)
	var baseSHA string
	if err == nil {
		baseSHA = ref.GetCommit().GetSHA()
	}

	// Create the parent branch (Branch A) for the PM's issue: ai-implement/issue-<details.Number>
	parentBranchName := fmt.Sprintf("ai-implement/issue-%d", details.Number)
	if baseSHA != "" {
		parentRefString := fmt.Sprintf("refs/heads/%s", parentBranchName)
		parentRef := &github.Reference{
			Ref:    github.String(parentRefString),
			Object: &github.GitObject{SHA: github.String(baseSHA)},
		}
		_, _, err = ghClient.Git.CreateRef(ctx, details.RepoOwner, details.RepoName, parentRef)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				fmt.Printf("ℹ️ Parent branch %s already exists\n", parentBranchName)
			} else {
				fmt.Printf("⚠️ Failed to create parent branch %s: %v\n", parentBranchName, err)
			}
		} else {
			fmt.Printf("✅ Parent branch %s successfully created\n", parentBranchName)
		}
	}

	createdBranchesReport := "\n###  Git Branch Initialization Status in Product Repo:\n"
	createdIssuesReport := "\n###  Task Creation & Kanban Board Status in Product Repo:\n"

	for _, task := range aiResult.Tasks {
		// Create new Issue
		issueTitle := fmt.Sprintf("[%s] %s", task.Assignee, task.Title)
		issueReq := &github.IssueRequest{
			Title:  github.String(issueTitle),
			Body:   github.String(task.Description),
			Labels: &[]string{"ai-process", fmt.Sprintf("repo:%s", details.RepoName)},
		}

		createdIssue, _, err := ghClient.Issues.Create(ctx, details.RepoOwner, details.RepoName, issueReq)
		if err != nil {
			createdIssuesReport += fmt.Sprintf("- **%s**: Issue creation failed (Error: %v) ❌\n", task.Title, err)
			continue
		}

		issueNum := createdIssue.GetNumber()
		issueNodeID := createdIssue.GetNodeID()

		// Generate normalized branch name: task-<number>-<title>
		cleanBranchName := sanitizeBranchName(fmt.Sprintf("task-%d-%s", issueNum, task.Title))

		// Update the Issue description to be complete
		updatedBody := fmt.Sprintf("%s\n\n---\n*Task assigned to: %s*\n*Expected branch: `%s`*\n*Parent Branch: `%s`*", task.Description, task.Assignee, cleanBranchName, parentBranchName)
		_, _, _ = ghClient.Issues.Edit(ctx, details.RepoOwner, details.RepoName, issueNum, &github.IssueRequest{
			Body: github.String(updatedBody),
		})

		// Create branch on GitHub
		if baseSHA != "" {
			refString := fmt.Sprintf("refs/heads/%s", cleanBranchName)
			newRef := &github.Reference{
				Ref:    github.String(refString),
				Object: &github.GitObject{SHA: github.String(baseSHA)},
			}
			_, _, err = ghClient.Git.CreateRef(ctx, details.RepoOwner, details.RepoName, newRef)
			if err != nil {
				createdBranchesReport += fmt.Sprintf("- ` %s`: Creation failed or already exists ⚠️\n", cleanBranchName)
			} else {
				createdBranchesReport += fmt.Sprintf("- ` %s`: Successfully initialized for **[%s]** ✅\n", cleanBranchName, task.Assignee)
			}
		}

		// Place the newly created child card into the Todo/Backlog column
		var targetColID string
		if id, ok := ghWrapper.GetTodoStatusOptionID(options); ok {
			targetColID = id
		}

		_, err = wrapperClient.CreateKanbanCardByIssueNodeID(ctx, projectID, statusFieldID, targetColID, issueNodeID)
		if err != nil {
			createdIssuesReport += fmt.Sprintf("- **%s**: Issue [#%d](https://github.com/%s/%s/issues/%d) created but failed to link to Kanban (Error: %v) ⚠️\n",
				task.Title, issueNum, details.RepoOwner, details.RepoName, issueNum, err)
		} else {
			createdIssuesReport += fmt.Sprintf("- **%s**: Issue [#%d](https://github.com/%s/%s/issues/%d) created and added to Backlog column successfully! ✅\n",
				task.Title, issueNum, details.RepoOwner, details.RepoName, issueNum)
		}
	}

	// Package the report and post it as a comment on the original Issue
	markdownReport := fmt.Sprintf(" **[Team Lead & BA Agent Report]**\n\n###  Overall Analysis:\n%s\n\n###  Breakdown Task List:\n", aiResult.Analysis)
	for i, task := range aiResult.Tasks {
		markdownReport += fmt.Sprintf("%d. **[%s]** %s\n", i+1, task.Assignee, task.Title)
	}
	markdownReport += createdBranchesReport + createdIssuesReport

	comment := &github.IssueComment{Body: github.String(markdownReport)}
	_, _, _ = ghClient.Issues.CreateComment(ctx, details.RepoOwner, details.RepoName, details.Number, comment)

	// Move the original PM card to the "In progress" column
	if inProgressColID, ok := options["in progress"]; ok {
		_ = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, details.ID, statusFieldID, inProgressColID)
	}

	fmt.Println(" Team Lead task breakdown cycle completed!")
}

// runDeveloperAgentOnKanban handles when the card is in the Backlog column: auto-codes and opens a PR, moves to In QA
func runDeveloperAgentOnKanban(ctx context.Context, ghClient *github.Client, wrapperClient *ghWrapper.Client, githubToken string, details *ghWrapper.ProjectItemDetails, projectID, statusFieldID string, options map[string]string) {
	fmt.Println(" [Developer Agent]: Received task and starting code development...")

	// 1. Determine the product code directory
	productRepoDir := os.Getenv("TEST_DIR")
	if productRepoDir == "" {
		productRepoDir = "."
	}

	// 2. Move Kanban card to "In progress" to signal work has started
	if inProgressColID, ok := options["in progress"]; ok {
		_ = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, details.ID, statusFieldID, inProgressColID)
	}

	// Generate normalized branch name in sync with Kanban Item: task-<number>-<title>
	branchName := sanitizeBranchName(fmt.Sprintf("task-%d-%s", details.Number, details.Title))

	// 3. Launch AI Developer to write code
	devAgent := agent.NewDeveloper()
	summaryReport, err := devAgent.DevelopTask(ctx, ghClient, githubToken, details.RepoOwner, details.RepoName, details.Number, details.Title, details.Body, productRepoDir, branchName)

	if err != nil {
		fmt.Printf("❌ AI Developer coding failed: %v\n", err)
		// Return card to Backlog/Todo column
		var targetColID string
		if id, ok := ghWrapper.GetTodoStatusOptionID(options); ok {
			targetColID = id
		}
		if targetColID != "" {
			_ = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, details.ID, statusFieldID, targetColID)
		}
		// Post error comment on Issue
		errorComment := fmt.Sprintf(" **[AI Developer Agent Report]**\n\n❌ **Code generation process failed!**\n\n*Error details:* `%v`", err)
		_, _, _ = ghClient.Issues.CreateComment(ctx, details.RepoOwner, details.RepoName, details.Number, &github.IssueComment{Body: github.String(errorComment)})
		return
	}

	// 4. Post the report as a comment on the Issue
	_, _, _ = ghClient.Issues.CreateComment(ctx, details.RepoOwner, details.RepoName, details.Number, &github.IssueComment{Body: github.String(summaryReport)})

	// 5. Move card to "Done" column to complete the work
	if doneColID, ok := options["done"]; ok {
		err = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, details.ID, statusFieldID, doneColID)
		if err == nil {
			fmt.Println(" Kanban card successfully moved to Done column!")
		}
	}
}

// runQAAgentOnKanban handles when the card is in the In QA column: auto-tests, updates column or creates bug
func runQAAgentOnKanban(ctx context.Context, ghClient *github.Client, wrapperClient *ghWrapper.Client, githubToken string, details *ghWrapper.ProjectItemDetails, projectID, statusFieldID string, options map[string]string) {
	fmt.Println(" [QA Agent]: Starting test suite execution...")

	// 1. Determine the product code directory
	productRepoDir := os.Getenv("TEST_DIR")
	if productRepoDir == "" {
		productRepoDir = "."
	}

	testCommand := os.Getenv("TEST_COMMAND")
	if testCommand == "" {
		testCommand = "go test ./..."
	}

	// Generate corresponding branch name
	branchName := sanitizeBranchName(fmt.Sprintf("task-%d-%s", details.Number, details.Title))

	// Checkout to the corresponding branch before running tests
	fmt.Printf(" [QA Agent]: Checking out branch: %s\n", branchName)
	if err := runGitCommand(productRepoDir, "checkout", branchName); err != nil {
		fmt.Printf("⚠️ Warning: Failed to checkout branch %s: %v. Tests will run on the current branch.\n", branchName, err)
	}

	// 2. Run tests
	qaAgent := agent.NewQAAgent()
	testLog, pass, err := qaAgent.RunTests(ctx, testCommand)
	if err != nil {
		fmt.Printf("❌ System error running test suite: %v\n", err)
		os.Exit(1)
	}

	var reportBody string
	var targetStatus string

	if pass {
		reportBody = fmt.Sprintf(" **[AI QA Agent Report]**\n\n✅ **Tests PASSED! (QA Passed)**\n\n- **Command:** `%s`\n- **Test branch:** `%s`\n\n### ️ Test Log Details:\n```text\n%s\n```", testCommand, branchName, testLog)
		targetStatus = "done"
	} else {
		// Call GitHub Models to analyze the failure log
		diagnosis, diagErr := qaAgent.DiagnoseFailure(ctx, githubToken, testLog, details.Title)
		if diagErr != nil {
			diagnosis = "*(Could not retrieve automatic failure diagnosis from GitHub Models)*"
		}

		// Create a new Bug Issue and add to Kanban
		bugTitle := fmt.Sprintf("[QA Failed] Bug found: %s", details.Title)
		bugBody := fmt.Sprintf("## ❌ Bug detected during automated testing\n\n**Related Task:** #%d\n**Test Command:** `%s`\n**Test Branch:** `%s`\n\n###  AI QA Failure Analysis:\n%s\n\n### ️ Test Log Details:\n```text\n%s\n```", details.Number, testCommand, branchName, diagnosis, testLog)

		bugReq := &github.IssueRequest{
			Title:  github.String(bugTitle),
			Body:   github.String(bugBody),
			Labels: &[]string{"bug", "qa-failed"},
		}

		createdBug, _, createBugErr := ghClient.Issues.Create(ctx, details.RepoOwner, details.RepoName, bugReq)
		var bugLink string
		if createBugErr == nil {
			bugNum := createdBug.GetNumber()
			bugNodeID := createdBug.GetNodeID()
			bugLink = fmt.Sprintf("[#%d](https://github.com/%s/%s/issues/%d)", bugNum, details.RepoOwner, details.RepoName, bugNum)

			// Place Bug Card in the Todo/Backlog column
			var targetColID string
			if id, ok := ghWrapper.GetTodoStatusOptionID(options); ok {
				targetColID = id
			}
			if targetColID != "" {
				_, _ = wrapperClient.CreateKanbanCardByIssueNodeID(ctx, projectID, statusFieldID, targetColID, bugNodeID)
			}
		} else {
			bugLink = "*(Error creating automatic Bug Issue)*"
		}

		reportBody = fmt.Sprintf(" **[AI QA Agent Report]**\n\n❌ **Tests FAILED! (QA Failed)**\n\n- **Command:** `%s`\n- **Test branch:** `%s`\n- **Bug Issue created:** %s\n\n###  AI Failure Analysis:\n%s\n\n<details>\n<summary>View test log details</summary>\n\n```text\n%s\n```\n\n</details>", testCommand, branchName, bugLink, diagnosis, testLog)
		targetStatus = "backlog"
	}

	// Post comment on Issue
	_, _, _ = ghClient.Issues.CreateComment(ctx, details.RepoOwner, details.RepoName, details.Number, &github.IssueComment{Body: github.String(reportBody)})

	// Update the current card status to Done or return to Todo/Backlog
	var targetColID string
	if id, ok := options[targetStatus]; ok {
		targetColID = id
	} else if targetStatus == "backlog" {
		if id, ok := ghWrapper.GetTodoStatusOptionID(options); ok {
			targetColID = id
		}
	}

	if targetColID != "" {
		_ = wrapperClient.UpdateProjectV2ItemStatus(ctx, projectID, details.ID, statusFieldID, targetColID)
		fmt.Printf(" Current Kanban card status updated to: %s\n", targetStatus)
	}
}

// runKanbanPollingFlow scans the Project Board and processes Todo or Backlog items
func runKanbanPollingFlow(ctx context.Context, ghClient *github.Client, githubToken string, projectNumStr, projectOwner string) {
	if projectNumStr == "" {
		fmt.Println("❌ Error: PROJECT_NUMBER is not configured.")
		return
	}
	projectNum, err := strconv.Atoi(projectNumStr)
	_ = projectNum
	if err != nil {
		fmt.Printf("❌ Error: PROJECT_NUMBER is invalid (%s): %v\n", projectNumStr, err)
		return
	}

	wrapperClient := ghWrapper.NewClient(githubToken)

	// Get Project ID
	projectID := "4"

	// List all items on the board
	items, err := wrapperClient.ListProjectV2Items(ctx, projectID)
	if err != nil {
		fmt.Printf("❌ Error listing project items: %v\n", err)
		return
	}

	// Get status field options to update status later
	statusFieldID, options, err := wrapperClient.GetProjectV2StatusOptions(ctx, projectID)
	if err != nil {
		fmt.Printf("❌ Error getting status options: %v\n", err)
		return
	}

	found := false
	for _, item := range items {
		statusNormalized := strings.ToLower(item.Status)
		if statusNormalized == "todo" || statusNormalized == "backlog" {
			fmt.Printf(" [Kanban Polling]: Found task in '%s' status: #%d - '%s'\n", item.Status, item.Number, item.Title)
			found = true

			// Resolve TEST_DIR dynamically for this item's repository
			orchestratorRepo := os.Getenv("GITHUB_REPOSITORY")
			var orchestratorRealName string
			if parts := strings.Split(orchestratorRepo, "/"); len(parts) == 2 {
				orchestratorRealName = parts[1]
			} else {
				orchestratorRealName = orchestratorRepo
			}

			productRepoDir := os.Getenv("TEST_DIR")
			if (productRepoDir == "" || productRepoDir == ".") && item.RepoName != "" && item.RepoName != orchestratorRealName {
				productRepoDir = item.RepoName
				os.Setenv("TEST_DIR", productRepoDir)
			}

			if productRepoDir != "" && productRepoDir != "." {
				if _, err := os.Stat(filepath.Join(productRepoDir, ".git")); os.IsNotExist(err) {
					fmt.Printf(" [Kanban Polling]: Product repository directory %s does not exist. Cloning...\n", productRepoDir)
					cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s/%s.git", githubToken, item.RepoOwner, item.RepoName)
					if err := runGitCommand(".", "clone", cloneURL, productRepoDir); err != nil {
						fmt.Printf("❌ Failed to clone Product Repository: %v\n", err)
						continue
					}
				}
			}

			// Run Developer Agent on this item!
			runDeveloperAgentOnKanban(ctx, ghClient, wrapperClient, githubToken, item, projectID, statusFieldID, options)
		}
	}
	if !found {
		fmt.Println(" [Kanban Polling]: No tasks found in 'Todo' or 'Backlog' status.")
	}
}
