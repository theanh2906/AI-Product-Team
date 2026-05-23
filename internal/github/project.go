package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// GraphQLRequest đại diện cho cấu trúc yêu cầu GraphQL
type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

// GraphQLResponse đại diện cho cấu trúc phản hồi GraphQL
type GraphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ProjectFieldOption đại diện cho một Option trong Single Select field (như Status)
type ProjectFieldOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ProjectField đại diện cho thông tin một field trong Project v2
type ProjectField struct {
	ID      string               `json:"id"`
	Name    string               `json:"name"`
	Options []ProjectFieldOption `json:"options,omitempty"`
}

// queryGraphQL thực hiện gửi request GraphQL lên GitHub API
func (c *Client) queryGraphQL(ctx context.Context, query string, variables map[string]interface{}, response interface{}) error {
	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to create graphql request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mini-AI-Orchestrator")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send graphql request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("graphql request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var gqlResp GraphQLResponse
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return fmt.Errorf("failed to decode graphql response: %w", err)
	}

	if len(gqlResp.Errors) > 0 && len(gqlResp.Data) == 0 {
		return fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}

	if response != nil && len(gqlResp.Data) > 0 {
		if err := json.Unmarshal(gqlResp.Data, response); err != nil {
			return fmt.Errorf("failed to unmarshal graphql data: %w", err)
		}
	}
	return nil
}

// GetProjectV2ID tìm ID của Project v2 theo owner và số project
func (c *Client) GetProjectV2ID(ctx context.Context, owner string, number int) (string, error) {
	// 1. Thử tìm theo Organization trước
	orgQuery := `query($owner: String!, $number: Int!) {
		organization(login: $owner) {
			projectV2(number: $number) {
				id
			}
		}
	}`
	var orgResult struct {
		Organization *struct {
			ProjectV2 *struct {
				ID string `json:"id"`
			} `json:"projectV2"`
		} `json:"organization"`
	}

	err := c.queryGraphQL(ctx, orgQuery, map[string]interface{}{"owner": owner, "number": number}, &orgResult)
	if err == nil && orgResult.Organization != nil && orgResult.Organization.ProjectV2 != nil {
		return orgResult.Organization.ProjectV2.ID, nil
	}

	// 2. Fallback tìm theo User
	userQuery := `query($owner: String!, $number: Int!) {
		user(login: $owner) {
			projectV2(number: $number) {
				id
			}
		}
	}`
	var userResult struct {
		User *struct {
			ProjectV2 *struct {
				ID string `json:"id"`
			} `json:"projectV2"`
		} `json:"user"`
	}

	err = c.queryGraphQL(ctx, userQuery, map[string]interface{}{"owner": owner, "number": number}, &userResult)
	if err != nil {
		return "", fmt.Errorf("failed to get project v2 id: %w", err)
	}
	if userResult.User == nil || userResult.User.ProjectV2 == nil {
		return "", fmt.Errorf("project v2 not found for owner %s with number %d", owner, number)
	}
	return userResult.User.ProjectV2.ID, nil
}

// GetProjectV2StatusField tìm ID của trường Status và ID của Option ứng với "Todo"
func (c *Client) GetProjectV2StatusField(ctx context.Context, projectID string) (statusFieldID string, todoOptionID string, err error) {
	query := `query($projectId: ID!) {
		node(id: $projectId) {
			... on ProjectV2 {
				fields(first: 50) {
					nodes {
						... on ProjectV2Field {
							id
							name
						}
						... on ProjectV2SingleSelectField {
							id
							name
							options {
								id
								name
							}
						}
					}
				}
			}
		}
	}`

	var result struct {
		Node struct {
			Fields struct {
				Nodes []ProjectField `json:"nodes"`
			} `json:"fields"`
		} `json:"node"`
	}

	err = c.queryGraphQL(ctx, query, map[string]interface{}{"projectId": projectID}, &result)
	if err != nil {
		return "", "", fmt.Errorf("failed to get project fields: %w", err)
	}

	for _, field := range result.Node.Fields.Nodes {
		if strings.EqualFold(field.Name, "Status") {
			statusFieldID = field.ID
			// Tìm option theo thứ tự ưu tiên (phù hợp với các loại Kanban Board khác nhau)
			priorities := []string{"Todo", "To Do", "New", "Backlog", "PM"}
			for _, targetName := range priorities {
				for _, opt := range field.Options {
					if strings.EqualFold(opt.Name, targetName) {
						todoOptionID = opt.ID
						return statusFieldID, todoOptionID, nil
					}
				}
			}
			// Nếu không khớp tên nào, lấy option đầu tiên làm mặc định
			if len(field.Options) > 0 {
				todoOptionID = field.Options[0].ID
				return statusFieldID, todoOptionID, nil
			}
		}
	}

	return "", "", fmt.Errorf("status field not found in project v2 %s", projectID)
}

// GetProjectV2StatusOptions trả về ID trường Status và map lưu trữ các Option (tên lowercased -> ID)
func (c *Client) GetProjectV2StatusOptions(ctx context.Context, projectID string) (statusFieldID string, options map[string]string, err error) {
	query := `query($projectId: ID!) {
		node(id: $projectId) {
			... on ProjectV2 {
				fields(first: 50) {
					nodes {
						... on ProjectV2Field {
							id
							name
						}
						... on ProjectV2SingleSelectField {
							id
							name
							options {
								id
								name
							}
						}
					}
				}
			}
		}
	}`

	var result struct {
		Node struct {
			Fields struct {
				Nodes []ProjectField `json:"nodes"`
			} `json:"fields"`
		} `json:"node"`
	}

	err = c.queryGraphQL(ctx, query, map[string]interface{}{"projectId": projectID}, &result)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get project fields: %w", err)
	}

	options = make(map[string]string)
	for _, field := range result.Node.Fields.Nodes {
		if strings.EqualFold(field.Name, "Status") {
			statusFieldID = field.ID
			for _, opt := range field.Options {
				options[strings.ToLower(opt.Name)] = opt.ID
			}
			return statusFieldID, options, nil
		}
	}

	return "", nil, fmt.Errorf("status field not found in project v2 %s", projectID)
}

// AddProjectV2ItemByID liên kết một Issue (hoặc PR) vào Project v2 thông qua NodeID (contentId)
func (c *Client) AddProjectV2ItemByID(ctx context.Context, projectID string, contentID string) (string, error) {
	mutation := `mutation($projectId: ID!, $contentId: ID!) {
		addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) {
			item {
				id
			}
		}
	}`

	var result struct {
		AddProjectV2ItemByID struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"addProjectV2ItemById"`
	}

	err := c.queryGraphQL(ctx, mutation, map[string]interface{}{
		"projectId": projectID,
		"contentId": contentID,
	}, &result)
	if err != nil {
		return "", fmt.Errorf("failed to add item to project v2: %w", err)
	}
	return result.AddProjectV2ItemByID.Item.ID, nil
}

// UpdateProjectV2ItemStatus cập nhật trạng thái của item sang cột được chỉ định (Todo)
func (c *Client) UpdateProjectV2ItemStatus(ctx context.Context, projectID string, itemID string, statusFieldID string, todoOptionID string) error {
	mutation := `mutation($projectId: ID!, $itemId: ID!, $fieldId: ID!, $optionId: String!) {
		updateProjectV2ItemFieldValue(input: {
			projectId: $projectId,
			itemId: $itemId,
			fieldId: $fieldId,
			value: {
				singleSelectOptionId: $optionId
			}
		}) {
			projectV2Item {
				id
			}
		}
	}`

	err := c.queryGraphQL(ctx, mutation, map[string]interface{}{
		"projectId": projectID,
		"itemId":    itemID,
		"fieldId":   statusFieldID,
		"optionId":  todoOptionID,
	}, nil)
	if err != nil {
		return fmt.Errorf("failed to update project item status: %w", err)
	}
	return nil
}

// CreateKanbanCardByIssueNodeID thực hiện liên kết Issue vào Kanban Board và gán cột "Todo"
func (c *Client) CreateKanbanCardByIssueNodeID(ctx context.Context, projectID string, statusFieldID string, todoOptionID string, issueNodeID string) (string, error) {
	itemID, err := c.AddProjectV2ItemByID(ctx, projectID, issueNodeID)
	if err != nil {
		return "", err
	}

	if statusFieldID != "" && todoOptionID != "" {
		err = c.UpdateProjectV2ItemStatus(ctx, projectID, itemID, statusFieldID, todoOptionID)
		if err != nil {
			// Chỉ log cảnh báo, không gây crash luồng vì item đã được tạo thành công
			fmt.Printf("⚠️ Warning: Failed to set status to 'Todo' for item %s: %v\n", itemID, err)
		}
	}
	return itemID, nil
}

// GetProjectV2ItemIDByContentID tìm ID của ProjectItem ứng với Content NodeID (của Issue/PR)
func (c *Client) GetProjectV2ItemIDByContentID(ctx context.Context, projectID string, contentNodeID string) (string, error) {
	query := `query($projectId: ID!) {
		node(id: $projectId) {
			... on ProjectV2 {
				items(first: 100) {
					nodes {
						id
						content {
							... on Issue {
								id
							}
							... on PullRequest {
								id
							}
						}
					}
				}
			}
		}
	}`

	var result struct {
		Node struct {
			Items struct {
				Nodes []struct {
					ID      string `json:"id"`
					Content struct {
						ID string `json:"id"`
					} `json:"content"`
				} `json:"nodes"`
			} `json:"items"`
		} `json:"node"`
	}

	err := c.queryGraphQL(ctx, query, map[string]interface{}{"projectId": projectID}, &result)
	if err != nil {
		return "", fmt.Errorf("failed to query project items: %w", err)
	}

	for _, node := range result.Node.Items.Nodes {
		if node.Content.ID == contentNodeID {
			return node.ID, nil
		}
	}

	return "", fmt.Errorf("project item not found for content node id %s", contentNodeID)
}
