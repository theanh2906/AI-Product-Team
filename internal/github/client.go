package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Issue represents GitHub issue details.
type Issue struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	CommentsURL string `json:"comments_url"`
	NodeID      string `json:"node_id"`
	HTMLURL     string `json:"html_url"`
}

// IssueEvent represents the github issue webhook event structure.
type IssueEvent struct {
	Action string `json:"action"`
	Issue  Issue  `json:"issue"`
}

// Client handles interaction with the GitHub API.
type Client struct {
	token string
}

// NewClient creates a new GitHub API client.
func NewClient(token string) *Client {
	return &Client{token: token}
}

// GetIssueEvent parses the event details from a JSON file path.
func (c *Client) GetIssueEvent(path string) (*IssueEvent, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open event file: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read event file: %w", err)
	}

	var event IssueEvent
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event: %w", err)
	}

	return &event, nil
}

// CommentOnIssue posts a comment to the specified issue URL.
func (c *Client) CommentOnIssue(commentsURL string, body string) error {
	if c.token == "" {
		return fmt.Errorf("GITHUB_TOKEN is not set")
	}

	payload := map[string]string{"body": body}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal comment payload: %w", err)
	}

	req, err := http.NewRequest("POST", commentsURL, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to post comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected response code: %d, response: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// CreateIssue creates a new issue on GitHub using the REST API.
func (c *Client) CreateIssue(ctx context.Context, owner, repo, title, body string) (*Issue, error) {
	if c.token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN is not set")
	}

	payload := map[string]string{
		"title": title,
		"body":  body,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal issue payload: %w", err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues", owner, repo)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mini-AI-Orchestrator")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to create issue: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create issue, status: %d, body: %s", resp.StatusCode, string(respBody))
	}

	var issue Issue
	if err := json.NewDecoder(resp.Body).Decode(&issue); err != nil {
		return nil, fmt.Errorf("failed to decode issue response: %w", err)
	}

	return &issue, nil
}
