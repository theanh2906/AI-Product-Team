package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
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

var todoStatusOptionPriorities = []string{"todo", "to do", "new", "backlog", "pm"}

// GetTodoStatusOptionID chọn option ID phù hợp nhất cho cột khởi đầu Todo.
func GetTodoStatusOptionID(options map[string]string) (string, bool) {
	for _, targetName := range todoStatusOptionPriorities {
		if id, ok := options[targetName]; ok {
			return id, true
		}
	}

	if len(options) == 0 {
		return "", false
	}

	optionNames := make([]string, 0, len(options))
	for name := range options {
		optionNames = append(optionNames, name)
	}
	sort.Strings(optionNames)
	return options[optionNames[0]], true
}

func getTodoStatusOptionIDFromProjectOptions(options []ProjectFieldOption) (string, bool) {
	optionsByName := make(map[string]string, len(options))
	for _, opt := range options {
		optionsByName[strings.ToLower(opt.Name)] = opt.ID
	}
	return GetTodoStatusOptionID(optionsByName)
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
			if id, ok := getTodoStatusOptionIDFromProjectOptions(field.Options); ok {
				todoOptionID = id
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

// ProjectItemDetails chứa thông tin chi tiết về thẻ Kanban
type ProjectItemDetails struct {
	ID          string
	Status      string
	ContentType string // "Issue" hoặc "PullRequest"
	Title       string
	Body        string
	Number      int
	ContentID   string
	RepoOwner   string
	RepoName    string
}

// GetProjectV2ItemDetails lấy thông tin chi tiết của một thẻ Kanban qua ID thẻ
func (c *Client) GetProjectV2ItemDetails(ctx context.Context, itemID string) (*ProjectItemDetails, error) {
	query := `query($itemId: ID!) {
		node(id: $itemId) {
			... on ProjectV2Item {
				id
				statusValue: fieldValueByName(name: "Status") {
					... on ProjectV2ItemFieldSingleSelectValue {
						name
					}
				}
				content {
					... on Issue {
						__typename
						id
						title
						body
						number
						repository {
							name
							owner {
								login
							}
						}
						labels(first: 10) {
							nodes {
								name
							}
						}
					}
					... on PullRequest {
						__typename
						id
						title
						body
						number
						repository {
							name
							owner {
								login
							}
						}
						labels(first: 10) {
							nodes {
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
			ID          string `json:"id"`
			StatusValue struct {
				Name string `json:"name"`
			} `json:"statusValue"`
			Content struct {
				Typename   string `json:"__typename"`
				ID         string `json:"id"`
				Title      string `json:"title"`
				Body       string `json:"body"`
				Number     int    `json:"number"`
				Repository struct {
					Name  string `json:"name"`
					Owner struct {
						Login string `json:"login"`
					} `json:"owner"`
				} `json:"repository"`
				Labels struct {
					Nodes []struct {
						Name string `json:"name"`
					} `json:"nodes"`
				} `json:"labels"`
			} `json:"content"`
		} `json:"node"`
	}

	err := c.queryGraphQL(ctx, query, map[string]interface{}{"itemId": itemID}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to get project item details: %w", err)
	}

	repoName := result.Node.Content.Repository.Name
	for _, lbl := range result.Node.Content.Labels.Nodes {
		if strings.HasPrefix(lbl.Name, "repo:") {
			repoName = strings.TrimPrefix(lbl.Name, "repo:")
			break
		}
	}

	details := &ProjectItemDetails{
		ID:          result.Node.ID,
		Status:      result.Node.StatusValue.Name,
		ContentType: result.Node.Content.Typename,
		Title:       result.Node.Content.Title,
		Body:        result.Node.Content.Body,
		Number:      result.Node.Content.Number,
		ContentID:   result.Node.Content.ID,
		RepoOwner:   result.Node.Content.Repository.Owner.Login,
		RepoName:    repoName,
	}

	return details, nil
}

// QueryGraphQLRaw thực hiện gửi request GraphQL lên GitHub API và trả về payload phản hồi dạng chuỗi (raw JSON)
func (c *Client) QueryGraphQLRaw(ctx context.Context, query string, variables map[string]interface{}) (string, error) {
	reqBody := GraphQLRequest{
		Query:     query,
		Variables: variables,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.github.com/graphql", bytes.NewBuffer(data))
	if err != nil {
		return "", fmt.Errorf("failed to create graphql request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mini-AI-Orchestrator")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send graphql request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("graphql request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return string(bodyBytes), nil
}

// ListProjectV2Items lists all items (Issues and Pull Requests) in a Project v2 board.
func (c *Client) ListProjectV2Items(ctx context.Context, projectID string) ([]*ProjectItemDetails, error) {
	query := `query($projectId: ID!) {
		node(id: $projectId) {
			... on ProjectV2 {
				items(first: 100) {
					nodes {
						id
						statusValue: fieldValueByName(name: "Status") {
							... on ProjectV2ItemFieldSingleSelectValue {
								name
							}
						}
						content {
							... on Issue {
								__typename
								id
								title
								body
								number
								repository {
									name
									owner {
										login
									}
								}
								labels(first: 10) {
									nodes {
										name
									}
								}
							}
							... on PullRequest {
								__typename
								id
								title
								body
								number
								repository {
									name
									owner {
										login
									}
								}
								labels(first: 10) {
									nodes {
										name
									}
								}
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
					ID          string `json:"id"`
					StatusValue struct {
						Name string `json:"name"`
					} `json:"statusValue"`
					Content struct {
						Typename   string `json:"__typename"`
						ID         string `json:"id"`
						Title      string `json:"title"`
						Body       string `json:"body"`
						Number     int    `json:"number"`
						Repository struct {
							Name  string `json:"name"`
							Owner struct {
								Login string `json:"login"`
							} `json:"owner"`
						} `json:"repository"`
						Labels struct {
							Nodes []struct {
								Name string `json:"name"`
							} `json:"nodes"`
						} `json:"labels"`
					} `json:"content"`
				} `json:"nodes"`
			} `json:"items"`
		} `json:"node"`
	}

	err := c.queryGraphQL(ctx, query, map[string]interface{}{"projectId": projectID}, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to query project items: %w", err)
	}

	var items []*ProjectItemDetails
	for _, node := range result.Node.Items.Nodes {
		// Content can be empty if it's a draft issue, skip or handle it
		if node.Content.ID == "" {
			continue
		}
		repoName := node.Content.Repository.Name
		for _, lbl := range node.Content.Labels.Nodes {
			if strings.HasPrefix(lbl.Name, "repo:") {
				repoName = strings.TrimPrefix(lbl.Name, "repo:")
				break
			}
		}
		items = append(items, &ProjectItemDetails{
			ID:          node.ID,
			Status:      node.StatusValue.Name,
			ContentType: node.Content.Typename,
			Title:       node.Content.Title,
			Body:        node.Content.Body,
			Number:      node.Content.Number,
			ContentID:   node.Content.ID,
			RepoOwner:   node.Content.Repository.Owner.Login,
			RepoName:    repoName,
		})
	}

	return items, nil
}

// MoveProjectV2ItemToStatus updates the status column of an item on the project board by column name.
func (c *Client) MoveProjectV2ItemToStatus(ctx context.Context, projectID string, itemID string, statusName string) error {
	statusFieldID, options, err := c.GetProjectV2StatusOptions(ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to get project status options: %w", err)
	}

	optionID, ok := options[strings.ToLower(statusName)]
	if !ok {
		var available []string
		for name := range options {
			available = append(available, name)
		}
		return fmt.Errorf("status column %q not found. Available columns: %v", statusName, available)
	}

	err = c.UpdateProjectV2ItemStatus(ctx, projectID, itemID, statusFieldID, optionID)
	if err != nil {
		return fmt.Errorf("failed to update project item status: %w", err)
	}

	return nil
}

