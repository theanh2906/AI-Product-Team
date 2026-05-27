package agent

// Task represents a child work item after decomposition
type Task struct {
	Title       string   `json:"title"`       // Task title (e.g. "Design Users database table")
	Description string   `json:"description"` // Detailed description of the steps to be done
	Assignee    string   `json:"assignee"`    // "Senior Fullstack Engineer"
	BranchName  string   `json:"branch_name"` // Expected branch name (e.g. "feat/user-db-setup")
	DependsOn   []string `json:"depends_on"`  // Name of prerequisite tasks if any (for sequential execution)
}

// AIResponse is the outer wrapper structure that Gemini must return
type AIResponse struct {
	Analysis string `json:"analysis"` // Team Lead's overall assessment of this feature
	Tasks    []Task `json:"tasks"`    // List of child tasks corresponding to the workflow
}
