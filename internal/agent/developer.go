package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v60/github"
)

// FileChange represents a file change (create or edit) decided by the AI
type FileChange struct {
	Path    string `json:"path"`    // Relative path from the project root, e.g. "src/main.go"
	Content string `json:"content"` // Full content of the new/edited file
}

// DeveloperResponse is the response structure containing the explanation and file changes
type DeveloperResponse struct {
	Explanation string       `json:"explanation"`
	Changes     []FileChange `json:"changes"`
}

// Developer represents the AI Developer Agent
type Developer struct {
	Name string
}

// NewDeveloper initializes a new Developer Agent
func NewDeveloper() *Developer {
	return &Developer{
		Name: "Developer-Agent",
	}
}

// DevelopTask analyzes the task, auto-writes code, creates a branch, commits, pushes and opens a PR
func (d *Developer) DevelopTask(ctx context.Context, ghClient *github.Client, githubToken string, owner, repo string, issueNumber int, taskTitle string, taskDescription string, productRepoDir string, branchName string) (string, error) {
	fmt.Printf(" [%s]: Researching task #%d: '%s'...\n", d.Name, issueNumber, taskTitle)

	// 1. Call GitHub Models API to generate source code
	modelName := os.Getenv("DEVELOPER_MODEL")
	if modelName == "" {
		modelName = os.Getenv("AI_MODEL")
	}
	aiClient := NewLLMClient(githubToken, modelName)

	var projectContext string
	var relevantFilesContent string
	var repomixContext string
	var errRepomix error

	fmt.Printf(" [%s]: Running Repomix to bundle codebase context...\n", d.Name)
	repomixContext, errRepomix = runRepomix(productRepoDir)
	if errRepomix != nil {
		fmt.Printf("⚠️ Warning: Repomix failed: %v. Falling back to local directory scanner.\n", errRepomix)
		projectContext = getProjectContext(productRepoDir)
		relevantFilesContent = getRelevantFilesContent(productRepoDir, taskTitle, taskDescription)
	} else {
		fmt.Printf("✅ [%s]: Codebase successfully bundled using Repomix!\n", d.Name)
	}

	systemInstruction := `You are a Senior Fullstack Engineer with excellent skills in writing clean and optimized code.
Your task is to read the development requirement or bug report, analyze the provided project context (file tree, file types, reference code), and design/write the source file changes needed.
You MUST write code matching the existing project's language, file extensions (e.g., .tsx for React TypeScript components, .ts for TypeScript modules, .go for Go, etc.), directory structure, and coding style.
Do not use placeholders or write stub code; the source code must be immediately runnable and complete.`

	var codebaseContext string
	if repomixContext != "" {
		codebaseContext = fmt.Sprintf("### Repomix Codebase Bundle\nHere is the bundled codebase context including existing file tree and files contents:\n\n%s", repomixContext)
	} else {
		codebaseContext = fmt.Sprintf("Here is the project context and file structure:\n%s\n%s", projectContext, relevantFilesContent)
	}

	prompt := fmt.Sprintf("%s\n\nPlease resolve the following task:\nTitle: %s\nDetailed requirements:\n%s\n\nReturn the list of corresponding source file changes.", codebaseContext, taskTitle, taskDescription)

	schema := &JSONSchema{
		Name:   "developer_response",
		Strict: true,
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"explanation": map[string]interface{}{
					"type": "string",
				},
				"changes": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{
								"type": "string",
							},
							"content": map[string]interface{}{
								"type": "string",
							},
						},
						"required":             []string{"path", "content"},
						"additionalProperties": false,
					},
				},
			},
			"required":             []string{"explanation", "changes"},
			"additionalProperties": false,
		},
	}

	respText, err := aiClient.GenerateContent(ctx, systemInstruction, prompt, schema)
	if err != nil {
		return "", fmt.Errorf("failed to generate code from GitHub Models: %w", err)
	}

	var devResult DeveloperResponse
	if err := json.Unmarshal([]byte(respText), &devResult); err != nil {
		return "", fmt.Errorf("failed to parse Developer response: %w, raw response: %s", err, respText)
	}

	fmt.Printf(" [%s]: Solution explanation: %s\n", d.Name, devResult.Explanation)
	fmt.Printf("️ [%s]: Detected %d file(s) to write/modify. Starting Git branch processing: %s...\n", d.Name, len(devResult.Changes), branchName)

	// 2. Git operations: Create and checkout a new branch
	if err := runGitCommand(productRepoDir, "checkout", "main"); err != nil {
		// Try checking out master if main does not exist
		_ = runGitCommand(productRepoDir, "checkout", "master")
	}
	// Delete old branch if it exists from a previous run to create a fresh one
	_ = runGitCommand(productRepoDir, "branch", "-D", branchName)
	if err := runGitCommand(productRepoDir, "checkout", "-b", branchName); err != nil {
		return "", fmt.Errorf("failed to checkout branch %s: %w", branchName, err)
	}

	// 3. Write the generated files to disk
	for _, change := range devResult.Changes {
		// Prevent writing outside the Product Repo directory (security)
		cleanPath := filepath.Clean(change.Path)
		if strings.HasPrefix(cleanPath, "..") || filepath.IsAbs(cleanPath) {
			fmt.Printf("⚠️ Warning: Skipping file outside scope: %s\n", change.Path)
			continue
		}

		filePath := filepath.Join(productRepoDir, cleanPath)
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return "", fmt.Errorf("failed to create directory for file %s: %w", filePath, err)
		}

		if err := os.WriteFile(filePath, []byte(change.Content), 0644); err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
		fmt.Printf(" [%s]: Successfully wrote file: %s\n", d.Name, change.Path)
	}

	// 4. Commit & Push code
	_ = runGitCommand(productRepoDir, "config", "user.name", "github-actions[bot]")
	_ = runGitCommand(productRepoDir, "config", "user.email", "github-actions[bot]@users.noreply.github.com")

	if err := runGitCommand(productRepoDir, "add", "."); err != nil {
		return "", fmt.Errorf("failed to git add: %w", err)
	}
	commitMsg := fmt.Sprintf("feat: implement task #%d - %s", issueNumber, taskTitle)
	if err := runGitCommand(productRepoDir, "commit", "-m", commitMsg); err != nil {
		// There may be no changes to commit
		fmt.Printf("ℹ️ No changes to commit or minor git commit error: %v\n", err)
	}

	// Push branch to remote (use -f to overwrite if re-running)
	if err := runGitCommand(productRepoDir, "push", "origin", branchName, "-f"); err != nil {
		return "", fmt.Errorf("failed to git push branch %s: %w", branchName, err)
	}
	fmt.Printf(" [%s]: Code successfully pushed to branch %s!\n", d.Name, branchName)

	// Parse base branch from description (defaults to main)
	baseBranch := "main"
	for _, line := range strings.Split(taskDescription, "\n") {
		if strings.Contains(line, "Parent Branch:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				rawBranch := strings.TrimSpace(parts[1])
				rawBranch = strings.Trim(rawBranch, "*` ")
				if rawBranch != "" {
					baseBranch = rawBranch
					break
				}
			}
		}
	}

	// 5. Create Pull Request on GitHub
	prTitle := fmt.Sprintf("PR for Task #%d: %s", issueNumber, taskTitle)
	prBody := fmt.Sprintf("##  AI Developer Agent Pull Request\n\nRelated Task: #%d\n\n###  Change Explanation:\n%s", issueNumber, devResult.Explanation)

	newPR := &github.NewPullRequest{
		Title: github.String(prTitle),
		Head:  github.String(branchName),
		Base:  github.String(baseBranch),
		Body:  github.String(prBody),
	}

	pr, _, prErr := ghClient.PullRequests.Create(ctx, owner, repo, newPR)
	var prLink string
	if prErr != nil {
		if strings.Contains(prErr.Error(), "A pull request already exists") {
			// PR already exists from a previous run, find the current PR
			prs, _, _ := ghClient.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{
				Head: owner + ":" + branchName,
			})
			if len(prs) > 0 {
				prLink = prs[0].GetHTMLURL()
				fmt.Printf("ℹ️ PR already exists: %s\n", prLink)
			} else {
				prLink = "*(Pull Request already exists)*"
			}
		} else {
			fmt.Printf("⚠️ Warning: Failed to create PR: %v\n", prErr)
			prLink = "*(Failed to automatically create PR, you can create it manually)*"
		}
	} else {
		prLink = pr.GetHTMLURL()
		fmt.Printf("✅ [%s]: Pull Request created successfully: %s\n", d.Name, prLink)
	}

	summaryReport := fmt.Sprintf(" **[AI Developer Agent Report]**\n\n- **Development branch:** `%s`\n- **Explanation:** %s\n- **Pull Request:** %s", branchName, devResult.Explanation, prLink)
	return summaryReport, nil
}

// runGitCommand executes a Git command in the specified directory
func runGitCommand(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %v failed: %w, output: %s", args, err, string(output))
	}
	return nil
}

// getProjectContext scans the repository directory to build a file list and detect file extensions
func getProjectContext(productRepoDir string) string {
	if productRepoDir == "" || productRepoDir == "." {
		return ""
	}

	var fileList []string
	var extensions = make(map[string]int)
	maxFiles := 150

	_ = filepath.Walk(productRepoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		
		rel, err := filepath.Rel(productRepoDir, path)
		if err != nil || rel == "." {
			return nil
		}

		if info.IsDir() {
			dirName := info.Name()
			if strings.HasPrefix(dirName, ".") || dirName == "node_modules" || dirName == "dist" || dirName == "build" || dirName == ".next" || dirName == "out" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := strings.ToLower(filepath.Ext(info.Name()))
		if ext != "" {
			extensions[ext]++
		}

		if len(fileList) < maxFiles {
			fileList = append(fileList, rel)
		}
		return nil
	})

	var extStrings []string
	for ext, count := range extensions {
		extStrings = append(extStrings, fmt.Sprintf("%s (%d files)", ext, count))
	}

	sb := strings.Builder{}
	sb.WriteString("### Product Repository File Types\n")
	sb.WriteString(fmt.Sprintf("- Primary file types found: %s\n", strings.Join(extStrings, ", ")))
	sb.WriteString("\n### Existing File Tree (Subset):\n")
	for _, f := range fileList {
		sb.WriteString(fmt.Sprintf("- %s\n", f))
	}

	return sb.String()
}

// getRelevantFilesContent matches keywords in task to read existing code files and provide them as context
func getRelevantFilesContent(productRepoDir string, taskTitle string, taskDescription string) string {
	if productRepoDir == "" || productRepoDir == "." {
		return ""
	}

	content := strings.ToLower(taskTitle + " " + taskDescription)
	
	var matchedFiles []string
	_ = filepath.Walk(productRepoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		if info.IsDir() {
			dirName := info.Name()
			if strings.HasPrefix(dirName, ".") || dirName == "node_modules" || dirName == "dist" || dirName == "build" || dirName == ".next" || dirName == "out" {
				return filepath.SkipDir
			}
			return nil
		}
		
		rel, err := filepath.Rel(productRepoDir, path)
		if err != nil {
			return nil
		}

		fileNameLower := strings.ToLower(info.Name())
		nameWithoutExt := strings.TrimSuffix(fileNameLower, filepath.Ext(fileNameLower))
		
		if len(nameWithoutExt) > 3 && strings.Contains(content, nameWithoutExt) {
			matchedFiles = append(matchedFiles, rel)
		}
		
		return nil
	})

	if len(matchedFiles) > 5 {
		matchedFiles = matchedFiles[:5]
	}

	if len(matchedFiles) == 0 {
		return ""
	}

	sb := strings.Builder{}
	sb.WriteString("\n### Reference Source Code of Relevant Existing Files\n")
	for _, relPath := range matchedFiles {
		fullPath := filepath.Join(productRepoDir, relPath)
		fileData, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		
		lines := strings.Split(string(fileData), "\n")
		if len(lines) > 400 {
			lines = lines[:400]
			lines = append(lines, "... (content truncated for length) ...")
		}
		
		sb.WriteString(fmt.Sprintf("File: `%s`\n```\n%s\n```\n\n", relPath, strings.Join(lines, "\n")))
	}

	return sb.String()
}

// runRepomix executes Repomix to bundle the codebase context, ignoring unnecessary folders
func runRepomix(productRepoDir string) (string, error) {
	if productRepoDir == "" || productRepoDir == "." {
		return "", fmt.Errorf("invalid product repo dir")
	}

	outputFile := filepath.Join(productRepoDir, "repomix-output.txt")
	_ = os.Remove(outputFile)

	npxCmd, npxArgs := getNpxCommand()
	args := append(npxArgs, "-y", "repomix", "--output", "repomix-output.txt", "--ignore", "**/.agents/**/*,**/.github/**/*,**/.vscode/**/*,**/.idea/**/*,**/package-lock.json,**/yarn.lock,**/pnpm-lock.yaml")

	cmd := exec.Command(npxCmd, args...)
	cmd.Dir = productRepoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("npx repomix failed: %w, output: %s", err, string(output))
	}

	data, err := os.ReadFile(outputFile)
	if err != nil {
		return "", fmt.Errorf("failed to read repomix output: %w", err)
	}

	_ = os.Remove(outputFile)
	return string(data), nil
}

// getNpxCommand resolves NPX command name depending on the operating system
func getNpxCommand() (string, []string) {
	if os.PathSeparator == '\\' {
		return "cmd", []string{"/c", "npx"}
	}
	return "npx", []string{}
}
