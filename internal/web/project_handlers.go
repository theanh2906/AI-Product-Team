package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

func (s *server) getSettings(w http.ResponseWriter, _ *http.Request) {
	value, err := s.projectService.getSettings()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	var request updateSettingsRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	value, err := s.projectService.updateSettings(request)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) putTheme(w http.ResponseWriter, r *http.Request) {
	var request updateThemeRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	value, err := s.projectService.updateTheme(request.Theme)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) putRoleDefinition(w http.ResponseWriter, r *http.Request) {
	var request updateRoleDefinitionRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	value, err := s.projectService.updateRoleDefinition(r.PathValue("role"), request.Content)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) putWorkingStandard(w http.ResponseWriter, r *http.Request) {
	var request updateWorkingStandardRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	value, err := s.projectService.updateWorkingStandard(request.Content)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) putAgentSkill(w http.ResponseWriter, r *http.Request) {
	var request updateAgentSkillRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	value, err := s.projectService.updateAgentSkill(r.PathValue("skillID"), request)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) getEffectiveAgentProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := s.projectService.effectiveRoleProfile(r.PathValue("role"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *server) getProjectAgentStudio(w http.ResponseWriter, r *http.Request) {
	profile, err := s.projectService.getProjectAgentStudio(r.PathValue("projectID"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *server) putProjectWorkingStandard(w http.ResponseWriter, r *http.Request) {
	var request updateWorkingStandardRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	profile, err := s.projectService.updateProjectWorkingStandard(r.PathValue("projectID"), request.Content)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *server) putProjectRoleDefinition(w http.ResponseWriter, r *http.Request) {
	var request updateRoleDefinitionRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	profile, err := s.projectService.updateProjectRoleDefinition(r.PathValue("projectID"), r.PathValue("role"), request.Content)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *server) putProjectAgentSkill(w http.ResponseWriter, r *http.Request) {
	var request updateAgentSkillRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	profile, err := s.projectService.updateProjectAgentSkill(r.PathValue("projectID"), r.PathValue("skillID"), request)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *server) getProjectEffectiveAgentProfile(w http.ResponseWriter, r *http.Request) {
	profile, err := s.projectService.effectiveRoleProfileForProject(r.PathValue("projectID"), r.PathValue("role"))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *server) getProjects(w http.ResponseWriter, _ *http.Request) {
	projects, err := s.projectService.listProjects()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *server) getWorkspaces(w http.ResponseWriter, _ *http.Request) {
	workspaces, err := s.projectService.listWorkspaces()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, workspaces)
}

func (s *server) postWorkspace(w http.ResponseWriter, r *http.Request) {
	var request createWorkspaceRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	workspace, err := s.projectService.createWorkspace(request)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, workspace)
}

func (s *server) putWorkspace(w http.ResponseWriter, r *http.Request) {
	var request createWorkspaceRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	workspace, err := s.projectService.updateWorkspace(r.PathValue("workspaceID"), request)
	if err != nil {
		status := http.StatusUnprocessableEntity
		if errors.Is(err, errWorkspaceNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, workspace)
}

func (s *server) getGitRepositories(w http.ResponseWriter, _ *http.Request) {
	repositories, err := s.projectService.listGitRepositories()
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, repositories)
}

func (s *server) getGitCredentials(w http.ResponseWriter, _ *http.Request) {
	credentials, err := s.projectService.listGitCredentials()
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, credentials)
}

func (s *server) putSelectedGitCredential(w http.ResponseWriter, r *http.Request) {
	var request selectGitCredentialRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	credential, err := s.projectService.selectGitCredential(request.Login)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, credential)
}

func (s *server) importGitProject(w http.ResponseWriter, r *http.Request) {
	var request importGitRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	created, err := s.projectService.importGit(request)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *server) importLocalProject(w http.ResponseWriter, r *http.Request) {
	var request importLocalRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	created, err := s.projectService.importLocal(request.Path)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (s *server) selectFolder(w http.ResponseWriter, _ *http.Request) {
	path, err := s.folderPicker()
	if errors.Is(err, errFolderPickerCancelled) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": path})
}

func (s *server) getStorageDatasource(w http.ResponseWriter, _ *http.Request) {
	value, err := s.projectService.storageDatasourceSummary()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) checkStorageDatasource(w http.ResponseWriter, r *http.Request) {
	var request storageDatasourceRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	writeJSON(w, http.StatusOK, s.projectService.checkStorageDatasource(request.config()))
}

func (s *server) switchStorageDatasource(w http.ResponseWriter, r *http.Request) {
	var request switchStorageDatasourceRequest
	if !decodeRequest(w, r, &request) {
		return
	}
	result, err := s.projectService.switchStorageDatasource(request.config(), request.Confirm)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error(), "result": result})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *server) exportBackupSettings(w http.ResponseWriter, r *http.Request) {
	value, err := s.projectService.exportSettings()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="productcrew-settings-backup.json"`)
	writeJSON(w, http.StatusOK, value)
}

func (s *server) previewBackupSettings(w http.ResponseWriter, r *http.Request) {
	var payload BackupPayload
	if !decodeRequest(w, r, &payload) {
		return
	}
	value, err := s.projectService.previewSettings(payload)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *server) restoreBackupSettings(w http.ResponseWriter, r *http.Request) {
	var payload BackupPayload
	if !decodeRequest(w, r, &payload) {
		return
	}
	value, err := s.projectService.restoreSettings(payload)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func decodeRequest(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "The request body must be a valid JSON object."})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "The request body must contain one JSON object."})
		return false
	}
	return true
}
