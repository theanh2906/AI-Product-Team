package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ghStatusFixture = `{"hosts":{"github.com":[
  {"state":"success","active":true,"host":"github.com","login":"work-user","tokenSource":"keyring"},
  {"state":"success","active":false,"host":"github.com","login":"personal-user","tokenSource":"keyring"}
]}}`

func TestSelectGitCredentialSwitchesGHAndPersistsLogin(t *testing.T) {
	directory := t.TempDir()
	service, err := newProjectService(directory, &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	var switchedCommand string
	service.runCommand = func(name string, args ...string) ([]byte, error) {
		command := name + " " + strings.Join(args, " ")
		if strings.Contains(command, "auth status") {
			return []byte(ghStatusFixture), nil
		}
		if strings.Contains(command, "auth switch") {
			switchedCommand = command
			return nil, nil
		}
		return nil, nil
	}

	selected, err := service.selectGitCredential("personal-user")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Login != "personal-user" || !selected.Selected {
		t.Fatalf("unexpected selected credential: %+v", selected)
	}
	if !strings.Contains(switchedCommand, "--user personal-user") {
		t.Fatalf("gh account was not switched: %s", switchedCommand)
	}
	data, err := os.ReadFile(filepath.Join(directory, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"githubAccount": "personal-user"`) {
		t.Fatalf("selected account was not persisted: %s", data)
	}
}

func TestResolveGitCredentialUsesSelectedGHAccount(t *testing.T) {
	service, err := newProjectService(t.TempDir(), &memoryCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	service.runCommand = func(name string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "auth token") {
			return []byte("selected-token\n"), nil
		}
		return nil, nil
	}
	username, token, err := service.resolveGitCredential(settingsFile{GitHubAccount: "personal-user"})
	if err != nil {
		t.Fatal(err)
	}
	if username != "personal-user" || token != "selected-token" {
		t.Fatalf("unexpected resolved credential: %s %s", username, token)
	}
}
