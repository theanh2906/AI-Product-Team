package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestListGitRepositoriesUsesConfiguredPAT(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer github-token" {
			t.Fatalf("unexpected authorization header: %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-GitHub-Api-Version") != githubAPIVersion {
			t.Fatalf("missing GitHub API version header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":2,"name":"zeta","full_name":"acme/zeta","clone_url":"https://github.com/acme/zeta.git","default_branch":"main","private":true,"archived":false},
			{"id":1,"name":"alpha","full_name":"acme/alpha","clone_url":"https://github.com/acme/alpha.git","default_branch":"develop","private":false,"archived":false}
		]`))
	}))
	defer server.Close()

	directory := t.TempDir()
	credentials := &memoryCredentialStore{value: "github-token"}
	service, err := newProjectService(directory, credentials)
	if err != nil {
		t.Fatal(err)
	}
	service.githubAPIBaseURL = server.URL
	service.runCommand = func(string, ...string) ([]byte, error) {
		return nil, errors.New("gh unavailable in PAT fallback test")
	}
	if _, err := service.updateSettings(updateSettingsRequest{
		ClonePath: filepath.Join(directory, "workspaces"), GitProvider: "github",
	}); err != nil {
		t.Fatal(err)
	}

	repositories, err := service.listGitRepositories()
	if err != nil {
		t.Fatal(err)
	}
	if len(repositories) != 2 || repositories[0].FullName != "acme/alpha" {
		t.Fatalf("unexpected repositories: %+v", repositories)
	}
	if repositories[0].DefaultBranch != "develop" || !repositories[1].Private {
		t.Fatalf("repository metadata was not mapped: %+v", repositories)
	}
}

func TestListGitRepositoriesRequiresPAT(t *testing.T) {
	service, err := newProjectService(t.TempDir(), &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	service.runCommand = func(string, ...string) ([]byte, error) {
		return nil, errors.New("gh unavailable")
	}
	_, err = service.listGitRepositories()
	if err == nil {
		t.Fatal("expected missing PAT error")
	}
}
