package kanban

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const IntakeSchemaVersion = 1

var intakeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)

type NormalizedIntake struct {
	WorkType           string
	DeliveryTarget     string
	RequiresUI         bool
	AcceptanceCriteria []string
}

func ValidateIntakeQuestionnaire(questionnaire IntakeQuestionnaire) error {
	if questionnaire.SchemaVersion != IntakeSchemaVersion {
		return fmt.Errorf("unsupported intake schema version %d", questionnaire.SchemaVersion)
	}
	questionnaire.Heading = strings.TrimSpace(questionnaire.Heading)
	questionnaire.Summary = strings.TrimSpace(questionnaire.Summary)
	if questionnaire.Heading == "" || len(questionnaire.Heading) > 100 {
		return fmt.Errorf("intake heading must contain 1 to 100 characters")
	}
	if len(questionnaire.Summary) < 10 || len(questionnaire.Summary) > 500 {
		return fmt.Errorf("intake summary must contain 10 to 500 characters")
	}
	if !BacklogItemType(questionnaire.WorkType).Valid() {
		return fmt.Errorf("intake work type must be feature, bug, or todo")
	}
	if len(questionnaire.Questions) < 2 || len(questionnaire.Questions) > 8 {
		return fmt.Errorf("intake must contain 2 to 8 questions")
	}

	ids := make(map[string]struct{}, len(questionnaire.Questions))
	for _, question := range questionnaire.Questions {
		if err := validateIntakeQuestion(question); err != nil {
			return err
		}
		if _, duplicated := ids[question.ID]; duplicated {
			return fmt.Errorf("duplicate intake question id %q", question.ID)
		}
		ids[question.ID] = struct{}{}
	}
	return nil
}

func validateIntakeQuestion(question IntakeQuestion) error {
	if !intakeIDPattern.MatchString(question.ID) {
		return fmt.Errorf("invalid intake question id %q", question.ID)
	}
	if strings.TrimSpace(question.Label) == "" || len(question.Label) > 120 || len(question.HelpText) > 240 {
		return fmt.Errorf("intake question %q has invalid copy", question.ID)
	}
	allowedTypes := map[string]bool{"toggle": true, "radio": true, "dropdown": true, "checkbox": true, "scale": true, "short_text": true, "info": true}
	allowedBindings := map[string]bool{"context": true, "requiresUI": true, "deliveryTarget": true, "acceptanceCriteria": true}
	if !allowedTypes[question.Type] || !allowedBindings[question.Binding] {
		return fmt.Errorf("intake question %q has an unsupported type or binding", question.ID)
	}
	if question.Layout.Span != 6 && question.Layout.Span != 12 {
		return fmt.Errorf("intake question %q layout span must be 6 or 12", question.ID)
	}
	if question.Type == "info" && (question.Required || question.Binding != "context") {
		return fmt.Errorf("intake info block %q cannot be required or bound", question.ID)
	}
	if question.Binding == "requiresUI" && question.Type != "toggle" {
		return fmt.Errorf("requiresUI question %q must be a toggle", question.ID)
	}
	if question.Binding == "deliveryTarget" && question.Type != "radio" && question.Type != "dropdown" {
		return fmt.Errorf("deliveryTarget question %q must be radio or dropdown", question.ID)
	}
	if question.Type == "short_text" || question.Type == "info" {
		if len(question.Options) != 0 {
			return fmt.Errorf("intake question %q must not define options", question.ID)
		}
		if question.Type == "info" && len(question.DefaultValues) != 0 {
			return fmt.Errorf("intake info block %q must not define defaults", question.ID)
		}
		return validateIntakeAnswers(question, question.DefaultValues)
	}
	if len(question.Options) < 2 || len(question.Options) > 10 {
		return fmt.Errorf("intake question %q must contain 2 to 10 options", question.ID)
	}
	values := make(map[string]struct{}, len(question.Options))
	for _, option := range question.Options {
		value := strings.TrimSpace(option.Value)
		if value == "" || len(value) > 80 || strings.TrimSpace(option.Label) == "" || len(option.Label) > 100 || len(option.Description) > 180 || len(option.AcceptanceCriterion) > 240 {
			return fmt.Errorf("intake question %q has an invalid option", question.ID)
		}
		if _, duplicated := values[value]; duplicated {
			return fmt.Errorf("intake question %q has duplicate option value %q", question.ID, value)
		}
		values[value] = struct{}{}
	}
	if question.Binding == "requiresUI" && (!hasValue(values, "true") || !hasValue(values, "false")) {
		return fmt.Errorf("requiresUI question %q must define true and false", question.ID)
	}
	if question.Binding == "deliveryTarget" {
		for value := range values {
			if value != "frontend" && value != "backend" && value != "fullstack" {
				return fmt.Errorf("deliveryTarget question %q has invalid value %q", question.ID, value)
			}
		}
	}
	for _, value := range question.DefaultValues {
		if _, exists := values[value]; !exists {
			return fmt.Errorf("intake question %q has an invalid default value", question.ID)
		}
	}
	return validateIntakeAnswers(question, question.DefaultValues)
}

func NormalizeIntake(submission IntakeSubmission) (NormalizedIntake, error) {
	if err := ValidateIntakeQuestionnaire(submission.Questionnaire); err != nil {
		return NormalizedIntake{}, err
	}
	result := NormalizedIntake{WorkType: submission.Questionnaire.WorkType, DeliveryTarget: "fullstack"}
	criteria := make([]string, 0)
	knownQuestions := make(map[string]struct{}, len(submission.Questionnaire.Questions))
	for _, question := range submission.Questionnaire.Questions {
		knownQuestions[question.ID] = struct{}{}
		values := append([]string{}, submission.Answers[question.ID]...)
		if len(values) == 0 {
			values = append(values, question.DefaultValues...)
		}
		if question.Required && question.Type != "info" && len(values) == 0 {
			return NormalizedIntake{}, fmt.Errorf("answer is required for %q", question.Label)
		}
		if err := validateIntakeAnswers(question, values); err != nil {
			return NormalizedIntake{}, err
		}
		switch question.Binding {
		case "requiresUI":
			result.RequiresUI = len(values) > 0 && values[0] == "true"
		case "deliveryTarget":
			if len(values) > 0 {
				result.DeliveryTarget = values[0]
			}
		case "acceptanceCriteria":
			for _, value := range values {
				for _, option := range question.Options {
					if option.Value == value {
						criterion := strings.TrimSpace(option.AcceptanceCriterion)
						if criterion == "" {
							criterion = strings.TrimSpace(option.Label)
						}
						criteria = append(criteria, criterion)
					}
				}
			}
		}
	}
	for questionID := range submission.Answers {
		if _, exists := knownQuestions[questionID]; !exists {
			return NormalizedIntake{}, fmt.Errorf("answer references unknown question %q", questionID)
		}
	}
	result.AcceptanceCriteria = uniqueSorted(criteria)
	if len(result.AcceptanceCriteria) == 0 {
		result.AcceptanceCriteria = []string{"The approved scope works end to end", "Critical behavior has regression coverage"}
	}
	return result, nil
}

func validateIntakeAnswers(question IntakeQuestion, values []string) error {
	if question.Type == "info" {
		return nil
	}
	if question.Type == "short_text" {
		if len(values) > 1 || (len(values) == 1 && len(strings.TrimSpace(values[0])) > 500) {
			return fmt.Errorf("answer for %q is invalid", question.Label)
		}
		return nil
	}
	if (question.Type == "toggle" || question.Type == "radio" || question.Type == "dropdown" || question.Type == "scale") && len(values) > 1 {
		return fmt.Errorf("answer for %q must contain one value", question.Label)
	}
	allowed := make(map[string]struct{}, len(question.Options))
	for _, option := range question.Options {
		allowed[option.Value] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := allowed[value]; !exists {
			return fmt.Errorf("answer for %q contains an unsupported value", question.Label)
		}
		if _, duplicated := seen[value]; duplicated {
			return fmt.Errorf("answer for %q contains a duplicate value", question.Label)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func hasValue(values map[string]struct{}, value string) bool {
	_, exists := values[value]
	return exists
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneIntakeSubmission(submission *IntakeSubmission) *IntakeSubmission {
	if submission == nil {
		return nil
	}
	clone := *submission
	clone.Questionnaire.Questions = append([]IntakeQuestion{}, submission.Questionnaire.Questions...)
	for index := range clone.Questionnaire.Questions {
		clone.Questionnaire.Questions[index].Options = append([]IntakeOption{}, submission.Questionnaire.Questions[index].Options...)
		clone.Questionnaire.Questions[index].DefaultValues = append([]string{}, submission.Questionnaire.Questions[index].DefaultValues...)
	}
	clone.Answers = make(map[string][]string, len(submission.Answers))
	for key, values := range submission.Answers {
		clone.Answers[key] = append([]string{}, values...)
	}
	return &clone
}
