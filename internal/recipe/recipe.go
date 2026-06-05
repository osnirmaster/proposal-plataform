package recipe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"proposalplatform/internal/mapping"

	"gopkg.in/yaml.v3"
)

type Recipe struct {
	JourneyType    string                 `json:"journeyType" yaml:"journeyType"`
	JourneyVersion string                 `json:"journeyVersion" yaml:"journeyVersion"`
	Version        string                 `json:"version" yaml:"version"`
	InitialStep    string                 `json:"initialStep" yaml:"initialStep"`
	Steps          []StepDefinition       `json:"steps" yaml:"steps"`
	Pipeline       []PipelineStep         `json:"pipeline" yaml:"pipeline"`
	Endpoints      map[string]Endpoint    `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
	ProposalSchema map[string]interface{} `json:"proposalSchema,omitempty" yaml:"proposalSchema,omitempty"`
	StateModel     StateModel             `json:"stateModel,omitempty" yaml:"stateModel,omitempty"`
}

type StateModel struct {
	Initial     string            `json:"initial,omitempty" yaml:"initial,omitempty"`
	Transitions []StateTransition `json:"transitions,omitempty" yaml:"transitions,omitempty"`
}

type StateTransition struct {
	From string `json:"from" yaml:"from"`
	To   string `json:"to" yaml:"to"`
}

type StepDefinition struct {
	Name           string                 `json:"name" yaml:"name"`
	Execution      string                 `json:"execution" yaml:"execution"`
	HookName       string                 `json:"hookName,omitempty" yaml:"hookName,omitempty"`
	TimeoutSeconds int                    `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
	Input          map[string]interface{} `json:"input,omitempty" yaml:"input,omitempty"`
	Transitions    map[string]Transition  `json:"transitions" yaml:"transitions"`
}

type PipelineStep struct {
	Step                  string                `json:"step" yaml:"step"`
	When                  string                `json:"when" yaml:"when"`
	ProposalStatusOnStart string                `json:"proposalStatusOnStart,omitempty" yaml:"proposalStatusOnStart,omitempty"`
	Action                Action                `json:"action" yaml:"action"`
	TimeoutSeconds        int                   `json:"timeoutSeconds,omitempty" yaml:"timeoutSeconds,omitempty"`
	Retry                 Retry                 `json:"retry,omitempty" yaml:"retry,omitempty"`
	Transitions           map[string]Transition `json:"transitions,omitempty" yaml:"transitions,omitempty"`
}

type Action struct {
	Type        string      `json:"type" yaml:"type"`
	Topic       string      `json:"topic,omitempty" yaml:"topic,omitempty"`
	HookName    string      `json:"hookName,omitempty" yaml:"hookName,omitempty"`
	TypeDetails TypeDetails `json:"typeDetails,omitempty" yaml:"typeDetails,omitempty"`
}

type TypeDetails struct {
	Mode            string            `json:"mode,omitempty" yaml:"mode,omitempty"`
	EndpointRef     string            `json:"endpointRef,omitempty" yaml:"endpointRef,omitempty"`
	Endpoint        Endpoint          `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	RequestMapping  map[string]string `json:"requestMapping,omitempty" yaml:"requestMapping,omitempty"`
	ResponseMapping map[string]string `json:"responseMapping,omitempty" yaml:"responseMapping,omitempty"`
	ResponseTarget  string            `json:"responseTarget,omitempty" yaml:"responseTarget,omitempty"`
}

type Retry struct {
	MaxAttempts    int `json:"maxAttempts,omitempty" yaml:"maxAttempts,omitempty"`
	BackoffSeconds int `json:"backoffSeconds,omitempty" yaml:"backoffSeconds,omitempty"`
}

type Endpoint struct {
	URL     string            `json:"url" yaml:"url"`
	Method  string            `json:"method,omitempty" yaml:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

type Transition struct {
	NextStep       string   `json:"nextStep,omitempty" yaml:"nextStep,omitempty"`
	ProposalStatus string   `json:"proposalStatus,omitempty" yaml:"proposalStatus,omitempty"`
	TerminalStatus string   `json:"terminalStatus,omitempty" yaml:"terminalStatus,omitempty"`
	ReasonCodes    []string `json:"reasonCodes,omitempty" yaml:"reasonCodes,omitempty"`
}

type StepExecution struct {
	Name                  string                 `json:"name"`
	HookName              string                 `json:"hookName,omitempty"`
	TimeoutSeconds        int                    `json:"timeoutSeconds,omitempty"`
	Input                 map[string]interface{} `json:"input,omitempty"`
	ProposalStatusOnStart string                 `json:"proposalStatusOnStart,omitempty"`
	Action                Action                 `json:"action,omitempty"`
	Retry                 Retry                  `json:"retry,omitempty"`
}

type Decision struct {
	Type           string
	Step           *StepExecution
	ProposalStatus string
	TerminalStatus string
	ReasonCodes    []string
}

type CompletedStep struct {
	Name    string
	State   string
	Outcome string
}

func Load(journeyType, journeyVersion string) (*Recipe, error) {
	if journeyType == "" {
		return nil, errors.New("journeyType is required")
	}
	if journeyVersion == "" {
		journeyVersion = "v1"
	}

	data, err := readRecipeFile(journeyType, journeyVersion)
	if err != nil {
		return nil, err
	}

	var r Recipe
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	if r.JourneyType == "" {
		r.JourneyType = journeyType
	}
	if r.JourneyVersion == "" {
		r.JourneyVersion = firstNonEmpty(r.Version, journeyVersion)
	}
	return &r, r.Validate()
}

func (r Recipe) Resolve(completed []CompletedStep, proposalContext ...map[string]interface{}) (Decision, error) {
	if len(r.Pipeline) > 0 {
		var context map[string]interface{}
		if len(proposalContext) > 0 {
			context = proposalContext[0]
		}
		return r.resolvePipeline(completed, context)
	}

	stepByName := make(map[string]StepDefinition, len(r.Steps))
	for _, step := range r.Steps {
		stepByName[strings.ToUpper(step.Name)] = step
	}

	outcomeByStep := make(map[string]string, len(completed))
	for _, step := range completed {
		if step.Name == "" || step.Outcome == "" {
			continue
		}
		outcomeByStep[strings.ToUpper(step.Name)] = strings.ToUpper(step.Outcome)
	}

	nextName := r.InitialStep
	visited := map[string]bool{}
	for {
		key := strings.ToUpper(nextName)
		step, ok := stepByName[key]
		if !ok {
			return Decision{}, fmt.Errorf("step %q not found in recipe", nextName)
		}
		if visited[key] {
			return Decision{}, fmt.Errorf("cycle detected while resolving step %q", nextName)
		}
		visited[key] = true

		outcome, done := outcomeByStep[key]
		if !done {
			return Decision{
				Type: strings.ToUpper(step.Execution),
				Step: &StepExecution{
					Name:           step.Name,
					HookName:       step.HookName,
					TimeoutSeconds: step.TimeoutSeconds,
					Input:          step.Input,
				},
			}, nil
		}

		transition, ok := findTransition(step.Transitions, outcome)
		if !ok {
			return Decision{}, fmt.Errorf("outcome %q is not mapped for step %q", outcome, step.Name)
		}
		if transition.TerminalStatus != "" {
			return Decision{
				Type:           "TERMINAL",
				TerminalStatus: transition.TerminalStatus,
				ReasonCodes:    transition.ReasonCodes,
			}, nil
		}
		if transition.NextStep == "" {
			return Decision{}, fmt.Errorf("transition for step %q outcome %q has no nextStep or terminalStatus", step.Name, outcome)
		}
		nextName = transition.NextStep
	}
}

func (r Recipe) CanTransition(from, to string) bool {
	if to == "" || strings.EqualFold(from, to) {
		return true
	}
	if from == "" {
		return strings.EqualFold(r.StateModel.Initial, to)
	}
	for _, transition := range r.StateModel.Transitions {
		if strings.EqualFold(transition.From, from) && strings.EqualFold(transition.To, to) {
			return true
		}
	}
	return len(r.StateModel.Transitions) == 0
}

func (r Recipe) Validate() error {
	if len(r.Pipeline) > 0 {
		return r.validatePipeline()
	}

	if r.InitialStep == "" {
		return errors.New("initialStep is required")
	}
	if len(r.Steps) == 0 {
		return errors.New("at least one step is required")
	}

	seen := map[string]bool{}
	for _, step := range r.Steps {
		if step.Name == "" {
			return errors.New("step name is required")
		}
		execution := strings.ToUpper(step.Execution)
		if execution != "SYNC" && execution != "ASYNC" {
			return fmt.Errorf("step %q execution must be SYNC or ASYNC", step.Name)
		}
		seen[strings.ToUpper(step.Name)] = true
	}
	if !seen[strings.ToUpper(r.InitialStep)] {
		return fmt.Errorf("initialStep %q not found in steps", r.InitialStep)
	}
	for _, step := range r.Steps {
		for outcome, transition := range step.Transitions {
			if outcome == "" {
				return fmt.Errorf("step %q has an empty outcome transition", step.Name)
			}
			if transition.NextStep != "" && !seen[strings.ToUpper(transition.NextStep)] {
				return fmt.Errorf("step %q points to unknown nextStep %q", step.Name, transition.NextStep)
			}
			if transition.NextStep != "" && transition.TerminalStatus != "" {
				return fmt.Errorf("step %q outcome %q cannot define both nextStep and terminalStatus", step.Name, outcome)
			}
		}
	}
	return nil
}

func (r Recipe) resolvePipeline(completed []CompletedStep, proposalContext map[string]interface{}) (Decision, error) {
	completedByStep := make(map[string]CompletedStep, len(completed))
	for _, step := range completed {
		if step.Name == "" {
			continue
		}
		completedByStep[strings.ToUpper(step.Name)] = step
	}

	var proposalStatus string
	for _, step := range r.Pipeline {
		completedStep := completedByStep[strings.ToUpper(step.Step)]
		if isCompleted(completedStep) {
			transition, ok := findTransition(step.Transitions, completedStep.Outcome)
			if !ok {
				return Decision{}, fmt.Errorf("outcome %q is not mapped for step %q", completedStep.Outcome, step.Step)
			}
			if transition.TerminalStatus != "" {
				return Decision{
					Type:           "TERMINAL",
					ProposalStatus: firstNonEmpty(transition.ProposalStatus, transition.TerminalStatus),
					TerminalStatus: transition.TerminalStatus,
					ReasonCodes:    transition.ReasonCodes,
				}, nil
			}
			proposalStatus = transition.ProposalStatus
			continue
		}
		matches, err := evaluateWhen(step.When, completedByStep, proposalContext)
		if err != nil {
			return Decision{}, fmt.Errorf("evaluate when for step %q: %w", step.Step, err)
		}
		if !matches {
			continue
		}

		action := step.Action
		if endpoint, ok := r.Endpoints[action.TypeDetails.EndpointRef]; ok {
			action.TypeDetails.Endpoint = endpoint
		}
		decisionType := "SYNC"
		if strings.EqualFold(step.Action.Type, "EVENT_HOOK") {
			decisionType = "ASYNC"
		}
		return Decision{
			Type:           decisionType,
			ProposalStatus: proposalStatus,
			Step: &StepExecution{
				Name:                  step.Step,
				HookName:              action.HookName,
				TimeoutSeconds:        step.TimeoutSeconds,
				ProposalStatusOnStart: step.ProposalStatusOnStart,
				Action:                action,
				Retry:                 step.Retry,
			},
		}, nil
	}

	return Decision{Type: "TERMINAL", TerminalStatus: "APPROVED"}, nil
}

func (r Recipe) validatePipeline() error {
	for ref, endpoint := range r.Endpoints {
		if ref == "" || endpoint.URL == "" {
			return errors.New("endpoints must define non-empty refs and urls")
		}
	}
	seen := map[string]bool{}
	for _, step := range r.Pipeline {
		if step.Step == "" {
			return errors.New("pipeline step is required")
		}
		seen[strings.ToUpper(step.Step)] = true
		actionType := strings.ToUpper(step.Action.Type)
		if actionType != "EVENT_HOOK" && actionType != "INTEGRATION" {
			return fmt.Errorf("step %q action.type must be EVENT_HOOK or INTEGRATION", step.Step)
		}
		if actionType == "INTEGRATION" {
			if step.Action.TypeDetails.Mode == "" {
				return fmt.Errorf("step %q integration mode is required", step.Step)
			}
			if step.Action.TypeDetails.EndpointRef == "" {
				return fmt.Errorf("step %q endpointRef is required", step.Step)
			}
			if _, ok := r.Endpoints[step.Action.TypeDetails.EndpointRef]; !ok && len(r.Endpoints) > 0 {
				return fmt.Errorf("step %q references unknown endpoint %q", step.Step, step.Action.TypeDetails.EndpointRef)
			}
		}
	}
	for _, step := range r.Pipeline {
		for outcome, transition := range step.Transitions {
			if outcome == "" {
				return fmt.Errorf("step %q has an empty outcome transition", step.Step)
			}
			if transition.NextStep != "" && !seen[strings.ToUpper(transition.NextStep)] {
				return fmt.Errorf("step %q points to unknown nextStep %q", step.Step, transition.NextStep)
			}
			if transition.NextStep != "" && transition.TerminalStatus != "" {
				return fmt.Errorf("step %q outcome %q cannot define both nextStep and terminalStatus", step.Step, outcome)
			}
		}
	}
	return nil
}

func readRecipeFile(journeyType, journeyVersion string) ([]byte, error) {
	var lastErr error
	for _, path := range candidatePaths(journeyType, journeyVersion) {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func candidatePaths(journeyType, journeyVersion string) []string {
	baseNames := []string{
		sanitize(journeyType),
		snake(journeyType),
		strings.ReplaceAll(snake(journeyType), "_opening", "_open"),
		strings.ReplaceAll(sanitize(journeyType), "-opening", "-open"),
	}
	versions := []string{sanitize(journeyVersion), snake(journeyVersion)}
	exts := []string{".yaml", ".yml", ".json"}

	var paths []string
	for _, baseName := range baseNames {
		for _, version := range versions {
			if version == "" {
				continue
			}
			for _, ext := range exts {
				paths = append(paths, filepath.Join(recipeDir(), baseName+"-"+version+ext))
			}
		}
		for _, ext := range exts {
			paths = append(paths, filepath.Join(recipeDir(), baseName+ext))
		}
	}
	return compact(paths)
}

func recipeDir() string {
	if dir := os.Getenv("RECIPE_DIR"); dir != "" {
		return dir
	}
	return "recipes"
}

func sanitize(value string) string {
	return strings.ToLower(strings.ReplaceAll(value, "_", "-"))
}

func snake(value string) string {
	return strings.ToLower(strings.ReplaceAll(value, "-", "_"))
}

func compact(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func findTransition(transitions map[string]Transition, outcome string) (Transition, bool) {
	for key, transition := range transitions {
		if strings.EqualFold(key, outcome) {
			return transition, true
		}
	}
	return Transition{}, false
}

func isCompleted(step CompletedStep) bool {
	return strings.EqualFold(step.State, "COMPLETED") || step.Outcome != ""
}

func evaluateWhen(expression string, steps map[string]CompletedStep, proposalContext map[string]interface{}) (bool, error) {
	if strings.TrimSpace(expression) == "" {
		return true, nil
	}

	left, right, ok := splitEquals(expression)
	if !ok {
		return false, fmt.Errorf("unsupported expression %q", expression)
	}
	actual, err := valueFor(left, steps, proposalContext)
	if err != nil {
		return false, err
	}
	return fmt.Sprint(actual) == right, nil
}

func splitEquals(expression string) (string, string, bool) {
	parts := strings.SplitN(expression, "==", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	left := strings.TrimSpace(parts[0])
	right := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
	return left, right, true
}

func valueFor(expression string, steps map[string]CompletedStep, proposalContext map[string]interface{}) (interface{}, error) {
	if strings.HasPrefix(expression, "steps.") {
		stepName := strings.TrimPrefix(expression, "steps.")
		step := steps[strings.ToUpper(stepName)]
		if step.State != "" {
			return step.State, nil
		}
		if step.Outcome != "" {
			return step.Outcome, nil
		}
		return "", nil
	}
	if strings.HasPrefix(expression, "context.") {
		source := map[string]interface{}{"context": proposalContext}
		return mapping.Search(source, "$."+expression)
	}
	if regexp.MustCompile(`^[A-Za-z0-9_.]+$`).MatchString(expression) {
		source := map[string]interface{}{"context": proposalContext}
		return mapping.Search(source, expression)
	}
	return nil, fmt.Errorf("unsupported left expression %q", expression)
}
