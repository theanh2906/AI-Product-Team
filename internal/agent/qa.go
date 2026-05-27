package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"google.golang.org/genai"
)

// QAAgent represents the QA agent specialized in running tests and diagnosing failures
type QAAgent struct {
	Name string
}

// NewQAAgent initializes a new QA Agent
func NewQAAgent() *QAAgent {
	return &QAAgent{
		Name: "QA-Agent",
	}
}

// RunTests runs the test command and returns the logs along with the result (pass/fail)
func (qa *QAAgent) RunTests(ctx context.Context, testCommand string) (string, bool, error) {
	fmt.Printf(" [%s]: Starting test suite with command: %s\n", qa.Name, testCommand)

	parts := strings.Fields(testCommand)
	if len(parts) == 0 {
		return "", false, fmt.Errorf("test command is empty")
	}

	cmdName := parts[0]
	cmdArgs := parts[1:]

	cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
	// Allow configuring the test directory (e.g. the directory containing the Product Repo)
	if testDir := os.Getenv("TEST_DIR"); testDir != "" {
		cmd.Dir = testDir
		fmt.Printf(" [%s]: Running tests in directory: %s\n", qa.Name, testDir)
	}

	// Run and combine both stdout and stderr
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err != nil {
		fmt.Printf("❌ [%s]: Test suite failed!\n", qa.Name)
		return outputStr, false, nil
	}

	fmt.Printf("✅ [%s]: Test suite ran successfully!\n", qa.Name)
	return outputStr, true, nil
}

// DiagnoseFailure uses the Gemini SDK to analyze error logs and suggest fixes
func (qa *QAAgent) DiagnoseFailure(ctx context.Context, geminiAPIKey string, testLog string, taskTitle string) (string, error) {
	fmt.Printf(" [%s]: Sending test log to Gemini for failure diagnosis...\n", qa.Name)

	aiClient, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: geminiAPIKey})
	if err != nil {
		return "", fmt.Errorf("failed to create Gemini client: %w", err)
	}

	systemInstruction := `You are an outstanding AI QA Engineer, an expert in software testing and debugging.
Your task is to read the test failure log from the CI/CD system, analyze why the tests failed, identify the suspicious files/lines of code causing the failure, and suggest detailed, easy-to-understand fix solutions for the developer.`

	prompt := fmt.Sprintf("Below is the task information under test and the failure log:\nTask: %s\n\nTest failure log:\n%s\n\nPlease analyze and return a detailed bug fix report in Markdown format.", taskTitle, testLog)

	resp, err := aiClient.Models.GenerateContent(ctx, "gemini-3-flash-preview", genai.Text(prompt), &genai.GenerateContentConfig{
		SystemInstruction: &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText(systemInstruction),
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to generate diagnosis from Gemini: %w", err)
	}

	return resp.Text(), nil
}