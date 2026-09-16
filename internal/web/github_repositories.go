package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

const githubAPIVersion = "2026-03-10"

type gitRepository struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"fullName"`
	CloneURL      string `json:"cloneUrl"`
	DefaultBranch string `json:"defaultBranch"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
}

type githubRepositoryResponse struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
}

func (s *projectService) listGitRepositories() ([]gitRepository, error) {
	s.mu.Lock()
	settings, err := s.readSettings()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if settings.GitProvider != "github" {
		return nil, fmt.Errorf("select GitHub as the Git provider in Settings")
	}
	_, token, err := s.resolveGitCredential(settings)
	if err != nil {
		return nil, err
	}

	repositories := make([]gitRepository, 0)
	for page := 1; page <= 20; page++ {
		endpoint := fmt.Sprintf("%s/user/repos?affiliation=owner,collaborator,organization_member&sort=updated&per_page=100&page=%d", strings.TrimRight(s.githubAPIBaseURL, "/"), page)
		request, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("create GitHub repository request: %w", err)
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
		request.Header.Set("User-Agent", "ProductCrew")

		response, err := s.httpClient.Do(request)
		if err != nil {
			return nil, fmt.Errorf("load GitHub repositories: %w", err)
		}
		var pageRepositories []githubRepositoryResponse
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(&pageRepositories)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, githubRepositoryStatusError(response.StatusCode)
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("decode GitHub repositories: %w", decodeErr)
		}
		for _, repository := range pageRepositories {
			repositories = append(repositories, gitRepository{
				ID:            repository.ID,
				Name:          repository.Name,
				FullName:      repository.FullName,
				CloneURL:      repository.CloneURL,
				DefaultBranch: repository.DefaultBranch,
				Private:       repository.Private,
				Archived:      repository.Archived,
			})
		}
		if len(pageRepositories) < 100 {
			break
		}
	}
	sort.Slice(repositories, func(i, j int) bool {
		return strings.ToLower(repositories[i].FullName) < strings.ToLower(repositories[j].FullName)
	})
	return repositories, nil
}

func githubRepositoryStatusError(status int) error {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("GitHub rejected the selected credential; switch account or update authentication")
	case http.StatusForbidden:
		return fmt.Errorf("GitHub did not allow repository access; check credential permissions and organization SSO")
	default:
		return fmt.Errorf("GitHub repository request failed with status %d", status)
	}
}
