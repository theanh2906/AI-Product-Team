package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/zalando/go-keyring"
)

const githubHost = "github.com"

type gitCredential struct {
	Login    string `json:"login"`
	Host     string `json:"host"`
	Active   bool   `json:"active"`
	Selected bool   `json:"selected"`
	Source   string `json:"source"`
}

type selectGitCredentialRequest struct {
	Login string `json:"login"`
}

type ghAuthStatus struct {
	Hosts map[string][]struct {
		State       string `json:"state"`
		Active      bool   `json:"active"`
		Host        string `json:"host"`
		Login       string `json:"login"`
		TokenSource string `json:"tokenSource"`
	} `json:"hosts"`
}

func (s *projectService) listGitCredentials() ([]gitCredential, error) {
	accounts, err := s.readGHCredentials()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	settings, settingsErr := s.readSettings()
	s.mu.Unlock()
	if settingsErr != nil {
		return nil, settingsErr
	}
	selectedLogin := settings.GitHubAccount
	if selectedLogin == "" {
		for _, account := range accounts {
			if account.Active {
				selectedLogin = account.Login
				break
			}
		}
	}
	for index := range accounts {
		accounts[index].Selected = accounts[index].Login == selectedLogin
	}
	return accounts, nil
}

func (s *projectService) selectGitCredential(login string) (gitCredential, error) {
	login = strings.TrimSpace(login)
	accounts, err := s.readGHCredentials()
	if err != nil {
		return gitCredential{}, err
	}
	var selected *gitCredential
	for index := range accounts {
		if accounts[index].Login == login {
			selected = &accounts[index]
			break
		}
	}
	if selected == nil {
		return gitCredential{}, fmt.Errorf("the selected GitHub CLI account is not configured")
	}
	if output, err := s.runCommand("gh", "auth", "switch", "--hostname", githubHost, "--user", login); err != nil {
		return gitCredential{}, fmt.Errorf("switch GitHub CLI account: %s", truncateMessage(string(output)))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	settings, err := s.readSettings()
	if err != nil {
		return gitCredential{}, err
	}
	settings.GitHubAccount = login
	if err := s.writeJSON("settings.json", settings); err != nil {
		return gitCredential{}, err
	}
	selected.Active = true
	selected.Selected = true
	return *selected, nil
}

func (s *projectService) readGHCredentials() ([]gitCredential, error) {
	output, err := s.runCommand("gh", "auth", "status", "--hostname", githubHost, "--json", "hosts")
	if err != nil {
		return nil, fmt.Errorf("read GitHub CLI accounts: %s", truncateMessage(string(output)))
	}
	var status ghAuthStatus
	if err := json.Unmarshal(output, &status); err != nil {
		return nil, fmt.Errorf("decode GitHub CLI accounts: %w", err)
	}
	accounts := make([]gitCredential, 0, len(status.Hosts[githubHost]))
	for _, account := range status.Hosts[githubHost] {
		if account.State != "success" || strings.TrimSpace(account.Login) == "" {
			continue
		}
		accounts = append(accounts, gitCredential{
			Login:  account.Login,
			Host:   account.Host,
			Active: account.Active,
			Source: "GitHub CLI " + account.TokenSource,
		})
	}
	sort.Slice(accounts, func(i, j int) bool {
		if accounts[i].Active != accounts[j].Active {
			return accounts[i].Active
		}
		return strings.ToLower(accounts[i].Login) < strings.ToLower(accounts[j].Login)
	})
	return accounts, nil
}

func (s *projectService) resolveGitCredential(settings settingsFile) (string, string, error) {
	login := settings.GitHubAccount
	if login == "" {
		if accounts, err := s.readGHCredentials(); err == nil {
			for _, account := range accounts {
				if account.Active {
					login = account.Login
					break
				}
			}
		}
	}
	if login != "" {
		output, err := s.runCommand("gh", "auth", "token", "--hostname", githubHost, "--user", login)
		if err != nil {
			return "", "", fmt.Errorf("read token for GitHub CLI account %s", login)
		}
		token := strings.TrimSpace(string(output))
		if token == "" {
			return "", "", fmt.Errorf("GitHub CLI returned an empty token for account %s", login)
		}
		return login, token, nil
	}

	token, err := s.credentials.Get(credentialUser)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", "", fmt.Errorf("configure a GitHub CLI account or PAT in Settings")
	}
	if err != nil {
		return "", "", fmt.Errorf("read Git credential: %w", err)
	}
	username := settings.GitUsername
	if username == "" {
		username = defaultGitUsername(settings.GitProvider)
	}
	return username, token, nil
}

func credentialType(settings settingsFile) string {
	if settings.GitHubAccount != "" {
		return "GitHub CLI keyring"
	}
	return "OS keyring"
}
