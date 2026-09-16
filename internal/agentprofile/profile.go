package agentprofile

import (
	"fmt"
	"sort"
	"strings"
)

const CurrentVersion = 1

const legacyIndependentVerificationInstructions = `## Verification workflow
- Derive checks from the approved plan, acceptance criteria, design handoff, and actual implementation.
- Inspect before judging and verify functional behavior, regression risk, error states, and design fidelity.
- Report only concrete reproducible bugs with severity, steps, expected behavior, actual behavior, and evidence.
- Do not modify production code; return a clean pass when no reproducible defect remains in scope.`

const focusedIndependentVerificationInstructions = `## Verification workflow
1. Build a finite checklist from the assigned QA task and its acceptance criteria before testing.
2. For a bug fix, verify only the reported behavior, its focused regression coverage, and directly touched behavior. For a feature, verify the full assigned acceptance scope.
3. Complete every feasible scoped check before deciding; report all in-scope defects from the run and consolidate findings that share one root cause.
4. Fail only for a concrete reproducible product defect inside the assigned scope. Treat missing tools, runtime, permissions, or environment as a verification blocker, never as a product bug.
5. Do not reopen a resolved finding without current reproduction evidence. Do not report unrelated pre-existing defects or broader improvement opportunities as failures.
6. Do not modify production code. Return a clean pass when no reproducible defect remains in scope.`

var supportedRoles = map[string]struct{}{
	"team-lead":     {},
	"designer":      {},
	"developer":     {},
	"qa":            {},
	"bug-scanner":   {},
	"feature-radar": {},
}

// Skill is a reusable workflow that ProductCrew can attach to one or more roles.
// Instructions are kept in settings.json so the same operating knowledge applies
// to every imported project without changing that project's repository files.
type Skill struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Instructions string   `json:"instructions"`
	Roles        []string `json:"roles"`
}

type EffectiveProfile struct {
	Role    string   `json:"role"`
	Version int      `json:"version"`
	Skills  []string `json:"skills"`
	Content string   `json:"content"`
}

func DefaultWorkingStandard() string {
	return `# ProductCrew Working Standard

## Operating principles
- Inspect the actual repository, configuration, runtime state, and logs before deciding.
- Reproduce the problem or collect concrete evidence before diagnosing it.
- Identify the root cause instead of patching only the visible symptom.
- Prefer the smallest production-ready change that satisfies the approved scope.
- Preserve unrelated user work and follow the repository's existing architecture and conventions.
- Verify with the most relevant available tests, lint, type-check, build, or runtime checks.
- Never claim success without verification evidence.
- Clearly distinguish facts, assumptions, recommendations, and blockers.
- Do not access secrets, credentials, environment files, or files outside the assigned workspace.
- Report exact changed files, executed checks, results, and remaining risks.

## Decision policy
- Search source, references, configuration, logs, and repository history when they can answer the question.
- Prefer primary sources when external research materially reduces uncertainty.
- Do not introduce dependencies, broaden scope, or change architecture without concrete justification.
- If verification cannot run, state the exact blocker and do not present an unverified result as complete.`
}

func DefaultSkills() []Skill {
	return []Skill{
		{
			ID: "evidence-first-investigation", Name: "Evidence-first investigation",
			Description: "Reproduce, collect evidence, and separate facts from assumptions before deciding.",
			Roles:       []string{"team-lead", "designer", "developer", "qa", "bug-scanner", "feature-radar"},
			Instructions: `## Workflow
1. Inspect the live repository and relevant configuration before proposing work.
2. Reproduce the behavior or collect concrete source, log, runtime, or test evidence.
3. State the root cause or the remaining uncertainty explicitly.
4. Base decisions and reports on evidence that another engineer can verify.`,
		},
		{
			ID: "dependency-aware-planning", Name: "Dependency-aware planning",
			Description: "Create implementation-ready documents and single-role tasks with valid dependencies.",
			Roles:       []string{"team-lead"},
			Instructions: `## Planning contract
- Produce concrete product, technical, design, and QA guidance only when required by the request.
- Assign exactly one role to each task and use stable references for tasks and documents.
- Express dependencies using tasks from the same plan and avoid orchestration or filler tasks.
- Keep the plan independently executable by downstream agents without hidden assumptions.`,
		},
		{
			ID: "implementation-ready-design", Name: "Implementation-ready design",
			Description: "Ground UI/UX decisions in the existing product and hand off testable states.",
			Roles:       []string{"designer"},
			Instructions: `## Design contract
- Inspect the current routes, components, visual tokens, interaction patterns, and desktop constraints.
- Define primary flow, loading, empty, error, disabled, success, and edge states where relevant.
- Cover accessibility, copy, hierarchy, and responsive behavior required by the approved scope.
- Produce artifacts and acceptance notes that Developer can implement without guessing.`,
		},
		{
			ID: "minimal-safe-implementation", Name: "Minimal safe implementation",
			Description: "Make the smallest maintainable code change and verify focused regressions.",
			Roles:       []string{"developer"},
			Instructions: `## Implementation workflow
1. Reproduce or verify the assigned behavior before editing.
2. Trace the owning code path and identify the root cause.
3. Implement the smallest production-ready change consistent with existing conventions.
4. Add or update focused regression coverage.
5. Run the most relevant checks and report exact results and remaining risks.`,
		},
		{
			ID: "independent-verification", Name: "Independent verification",
			Description:  "Verify a finite assigned scope, separating product defects from environment blockers.",
			Roles:        []string{"qa"},
			Instructions: focusedIndependentVerificationInstructions,
		},
	}
}

// MergeDefaults preserves persisted customizations and appends newly shipped
// ProductCrew skills so upgrades remain backward compatible.
func MergeDefaults(skills []Skill) []Skill {
	defaults := DefaultSkills()
	if len(skills) == 0 {
		return defaults
	}
	result := append([]Skill(nil), skills...)
	for index := range result {
		if result[index].ID == "independent-verification" && strings.TrimSpace(result[index].Instructions) == legacyIndependentVerificationInstructions {
			updated := defaultSkillByID(defaults, "independent-verification")
			updated.Roles = append([]string(nil), result[index].Roles...)
			result[index] = updated
		}
	}
	seen := make(map[string]struct{}, len(result))
	for _, skill := range result {
		seen[skill.ID] = struct{}{}
	}
	for _, skill := range defaults {
		if _, exists := seen[skill.ID]; !exists {
			result = append(result, skill)
		}
	}
	return result
}

func defaultSkillByID(skills []Skill, id string) Skill {
	for _, skill := range skills {
		if skill.ID == id {
			return skill
		}
	}
	return Skill{}
}

func NormalizeRole(role string) (string, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "teamlead" || role == "team_lead" {
		role = "team-lead"
	}
	if role == "bugscanner" || role == "bug_scanner" {
		role = "bug-scanner"
	}
	if role == "featureradar" || role == "feature_radar" {
		role = "feature-radar"
	}
	if _, ok := supportedRoles[role]; !ok {
		return "", fmt.Errorf("unsupported agent role %q", role)
	}
	return role, nil
}

func ValidateSkill(skill Skill) (Skill, error) {
	skill.ID = strings.ToLower(strings.TrimSpace(skill.ID))
	skill.Name = strings.TrimSpace(skill.Name)
	skill.Description = strings.TrimSpace(skill.Description)
	skill.Instructions = strings.TrimSpace(skill.Instructions)
	if skill.ID == "" || skill.Name == "" || skill.Instructions == "" {
		return Skill{}, fmt.Errorf("skill id, name, and instructions are required")
	}
	if len(skill.Name) > 120 || len(skill.Description) > 500 || len(skill.Instructions) > 20000 {
		return Skill{}, fmt.Errorf("skill content exceeds the supported size")
	}
	roles := make([]string, 0, len(skill.Roles))
	seen := map[string]struct{}{}
	for _, value := range skill.Roles {
		role, err := NormalizeRole(value)
		if err != nil {
			return Skill{}, err
		}
		if _, exists := seen[role]; exists {
			continue
		}
		seen[role] = struct{}{}
		roles = append(roles, role)
	}
	sort.Strings(roles)
	skill.Roles = roles
	return skill, nil
}

func Compose(version int, role, workingStandard, roleInstructions string, skills []Skill) (EffectiveProfile, error) {
	return ComposeWithProjectKnowledge(version, role, workingStandard, "", roleInstructions, skills)
}

// ComposeWithProjectKnowledge layers evidence-backed project guidance between the
// shared standard and manually maintained role instructions. This keeps learned
// knowledge project-specific while allowing explicit administrator edits and
// assigned skills to remain the highest-priority ProductCrew guidance.
func ComposeWithProjectKnowledge(version int, role, workingStandard, projectKnowledge, roleInstructions string, skills []Skill) (EffectiveProfile, error) {
	role, err := NormalizeRole(role)
	if err != nil {
		return EffectiveProfile{}, err
	}
	workingStandard = strings.TrimSpace(workingStandard)
	roleInstructions = strings.TrimSpace(roleInstructions)
	if workingStandard == "" || roleInstructions == "" {
		return EffectiveProfile{}, fmt.Errorf("working standard and role instructions are required")
	}
	if version < CurrentVersion {
		version = CurrentVersion
	}
	sections := []string{
		"# ProductCrew Effective Agent Profile",
		"The repository AGENTS.md guidance is loaded separately by the selected AI runtime. Follow it together with this ProductCrew profile.",
		"## Shared Working Standard\n" + workingStandard,
	}
	if projectKnowledge = strings.TrimSpace(projectKnowledge); projectKnowledge != "" {
		sections = append(sections, "## Learned Project Knowledge\n"+projectKnowledge)
	}
	sections = append(sections, "## Role Profile\n"+roleInstructions)
	applied := make([]string, 0)
	skillSections := make([]string, 0)
	for _, skill := range skills {
		if !contains(skill.Roles, role) {
			continue
		}
		validated, validateErr := ValidateSkill(skill)
		if validateErr != nil {
			return EffectiveProfile{}, fmt.Errorf("invalid skill %q: %w", skill.ID, validateErr)
		}
		applied = append(applied, validated.ID)
		skillSections = append(skillSections, "### "+validated.Name+"\n"+validated.Instructions)
	}
	if len(skillSections) > 0 {
		sections = append(sections, "## Assigned Skills\n"+strings.Join(skillSections, "\n\n"))
	}
	return EffectiveProfile{Role: role, Version: version, Skills: applied, Content: strings.Join(sections, "\n\n")}, nil
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}
