package agentprofile

import (
	"strings"
	"testing"
)

func TestComposeIncludesSharedRoleAndOnlyAssignedSkills(t *testing.T) {
	profile, err := Compose(1, "developer", "Inspect first.", "Implement safely.", DefaultSkills())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Inspect first.", "Implement safely.", "Evidence-first investigation", "Minimal safe implementation"} {
		if !strings.Contains(profile.Content, expected) {
			t.Fatalf("effective profile is missing %q", expected)
		}
	}
	if strings.Contains(profile.Content, "Independent verification") {
		t.Fatal("developer profile must not include QA-only skills")
	}
}

func TestMergeDefaultsPreservesSkillCustomization(t *testing.T) {
	custom := Skill{ID: "evidence-first-investigation", Name: "Custom evidence", Instructions: "Custom workflow.", Roles: []string{"developer"}}
	merged := MergeDefaults([]Skill{custom})
	if merged[0].Name != "Custom evidence" || len(merged) != len(DefaultSkills()) {
		t.Fatalf("unexpected merged skills: %+v", merged)
	}
}

func TestMergeDefaultsUpgradesLegacyIndependentVerification(t *testing.T) {
	legacy := Skill{ID: "independent-verification", Name: "Independent verification", Instructions: legacyIndependentVerificationInstructions, Roles: []string{"qa"}}
	merged := MergeDefaults([]Skill{legacy})
	if len(merged) != len(DefaultSkills()) {
		t.Fatalf("unexpected merged skill count: %d", len(merged))
	}
	if merged[0].Instructions != focusedIndependentVerificationInstructions {
		t.Fatalf("legacy QA skill was not upgraded: %q", merged[0].Instructions)
	}
	if len(merged[0].Roles) != 1 || merged[0].Roles[0] != "qa" {
		t.Fatalf("QA skill routing changed during upgrade: %+v", merged[0].Roles)
	}
}

func TestValidateSkillNormalizesAndRejectsUnknownRoles(t *testing.T) {
	skill, err := ValidateSkill(Skill{ID: " TEST ", Name: " Test ", Instructions: " Do work. ", Roles: []string{"qa", "QA"}})
	if err != nil {
		t.Fatal(err)
	}
	if skill.ID != "test" || len(skill.Roles) != 1 || skill.Roles[0] != "qa" {
		t.Fatalf("unexpected normalized skill: %+v", skill)
	}
	if _, err := ValidateSkill(Skill{ID: "bad", Name: "Bad", Instructions: "Bad", Roles: []string{"admin"}}); err == nil {
		t.Fatal("expected unsupported role error")
	}
}

func TestComposeFeatureRadarIncludesAssignedCustomSkill(t *testing.T) {
	skills := []Skill{{
		ID: "product-discovery", Name: "Product discovery", Instructions: "Apply the custom opportunity rubric.",
		Roles: []string{"feature-radar"},
	}}
	profile, err := Compose(1, "feature-radar", "Inspect first.", "Discover feasible opportunities.", skills)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Role != "feature-radar" || len(profile.Skills) != 1 || profile.Skills[0] != "product-discovery" {
		t.Fatalf("unexpected Feature Radar profile: %+v", profile)
	}
	if !strings.Contains(profile.Content, "Apply the custom opportunity rubric.") {
		t.Fatalf("custom Feature Radar skill was not composed: %s", profile.Content)
	}
}

func TestComposeWithProjectKnowledgeKeepsManualRoleInstructionsHighestPriority(t *testing.T) {
	profile, err := ComposeWithProjectKnowledge(2, "developer", "Shared standard.", "Learned repository guidance.", "Manual project role instructions.", nil)
	if err != nil {
		t.Fatal(err)
	}
	shared := strings.Index(profile.Content, "Shared standard.")
	learned := strings.Index(profile.Content, "Learned repository guidance.")
	manual := strings.Index(profile.Content, "Manual project role instructions.")
	if shared < 0 || learned <= shared || manual <= learned {
		t.Fatalf("unexpected instruction precedence: %s", profile.Content)
	}
}
