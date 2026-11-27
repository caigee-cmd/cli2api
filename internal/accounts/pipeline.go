package accounts

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	PipelineVersion  = 1
	MaxPipelineDepth = 3
)

// RouteSeat is one routing unit. A member seat is one concrete model. A group
// seat is another aggregation that keeps its own strategy.
type RouteSeat struct {
	ID            string
	Kind          string
	Priority      int
	Weight        int
	Member        AggregationMember
	GroupID       string
	GroupStrategy AggregationStrategy
	GroupMembers  []AggregationMember
	GroupSeats    []RouteSeat
}

type pipelineDoc struct {
	Version int            `json:"version"`
	Steps   []pipelineStep `json:"steps"`
}

type pipelineClassifier struct {
	Provider string `json:"provider"`
	Region   string `json:"region,omitempty"`
	Model    string `json:"model"`
}

type pipelinePath struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	Keywords []string       `json:"keywords,omitempty"`
	Intent   string         `json:"intent,omitempty"`
	Fallback bool           `json:"fallback,omitempty"`
	Steps    []pipelineStep `json:"steps"`
}

type pipelineStep struct {
	ID            string              `json:"id"`
	Kind          string              `json:"kind"`
	Provider      string              `json:"provider,omitempty"`
	Region        string              `json:"region,omitempty"`
	Model         string              `json:"model,omitempty"`
	AccountID     string              `json:"account_id,omitempty"`
	Weight        int                 `json:"weight,omitempty"`
	AggregationID string              `json:"aggregation_id,omitempty"`
	Classifier    *pipelineClassifier `json:"classifier,omitempty"`
	Paths         []pipelinePath      `json:"paths,omitempty"`
	Fallback      []pipelineStep      `json:"fallback,omitempty"`
}

// ClassifierTarget is a concrete model that names the path for one branch.
type ClassifierTarget struct {
	Provider string
	Region   string
	Model    string
}

func (t ClassifierTarget) Key() string {
	return strings.ToLower(strings.TrimSpace(t.Provider)) + "|" + strings.ToLower(strings.TrimSpace(t.Region)) + "|" + strings.TrimSpace(t.Model)
}

// CollectPipeline validates a document and returns the canonical JSON plus
// every concrete member any path can reach. Members are what the catalog and
// provider-policy check see.
func CollectPipeline(id, document string, catalog map[string]Aggregation) (string, []AggregationMember, error) {
	doc, err := parsePipeline(document)
	if err != nil {
		return "", nil, err
	}
	var seq int
	members, err := collectSteps(id, doc.Steps, 0, map[string]bool{id: true}, catalog, &seq)
	if err != nil {
		return "", nil, err
	}
	members, err = normalizeAggregationMembers(members)
	if err != nil {
		return "", nil, err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", nil, err
	}
	return string(raw), members, nil
}

// CompilePipeline selects one path and builds the seats for a single request.
// labels maps ClassifierTarget.Key to the label a classifier returned. An
// empty label falls through to keywords and then the fallback path.
func CompilePipeline(agg Aggregation, catalog map[string]Aggregation, text string, labels map[string]string, allowed map[string]bool) (Aggregation, []RouteSeat, string, error) {
	if strings.TrimSpace(agg.Document) == "" {
		return agg, nil, "", nil
	}
	doc, err := parsePipeline(agg.Document)
	if err != nil {
		return Aggregation{}, nil, "", err
	}
	chosen, path := selectSteps(doc.Steps, text, labels)
	var seq int
	seats, members, err := seatSteps(agg.ID, chosen, 0, map[string]bool{agg.ID: true}, catalog, text, labels, &seq)
	if err != nil {
		return Aggregation{}, nil, "", err
	}
	members = filterMembers(members, allowed)
	seats = filterSeats(seats, allowed)
	if len(members) == 0 {
		return Aggregation{}, nil, "", fmt.Errorf("aggregation %s has no reachable members", agg.ID)
	}
	agg.Members = members
	if !hasGroup(seats) {
		return agg, nil, path, nil
	}
	return agg, seats, path, nil
}

// ClassifierTargets lists concrete classifier models referenced by this
// document and by nested documents, in walk order, without duplicates.
func ClassifierTargets(id, document string, catalog map[string]Aggregation) ([]ClassifierTarget, error) {
	if strings.TrimSpace(document) == "" {
		return nil, nil
	}
	doc, err := parsePipeline(document)
	if err != nil {
		return nil, err
	}
	var out []ClassifierTarget
	seen := map[string]bool{}
	if err := walkClassifiers(id, doc.Steps, 0, map[string]bool{id: true}, catalog, seen, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func parsePipeline(document string) (pipelineDoc, error) {
	var doc pipelineDoc
	if err := json.Unmarshal([]byte(document), &doc); err != nil {
		return pipelineDoc{}, fmt.Errorf("aggregation document: %w", err)
	}
	if doc.Version == 0 {
		doc.Version = PipelineVersion
	}
	if doc.Version != PipelineVersion {
		return pipelineDoc{}, fmt.Errorf("aggregation document version %d is not supported", doc.Version)
	}
	if len(doc.Steps) == 0 {
		return pipelineDoc{}, fmt.Errorf("aggregation document has no steps")
	}
	return doc, nil
}

func collectSteps(self string, steps []pipelineStep, depth int, stack map[string]bool, catalog map[string]Aggregation, seq *int) ([]AggregationMember, error) {
	var out []AggregationMember
	for _, step := range steps {
		switch step.Kind {
		case "member":
			member, err := leafMember(step, seq)
			if err != nil {
				return nil, err
			}
			out = append(out, member)
		case "group":
			nested, err := openGroup(self, step, depth, stack, catalog)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(nested.Document) == "" {
				for _, member := range nested.Members {
					member.Priority = *seq
					*seq++
					out = append(out, member)
				}
				continue
			}
			doc, err := parsePipeline(nested.Document)
			if err != nil {
				return nil, err
			}
			stack[nested.ID] = true
			inner, err := collectSteps(nested.ID, doc.Steps, depth+1, stack, catalog, seq)
			delete(stack, nested.ID)
			if err != nil {
				return nil, err
			}
			out = append(out, inner...)
		case "branch":
			if err := validateClassifier(step.Classifier, catalog); err != nil {
				return nil, err
			}
			paths := step.Paths
			if len(step.Fallback) > 0 {
				paths = append(paths, pipelinePath{ID: step.ID + "-fallback", Label: "Default", Fallback: true, Steps: step.Fallback})
			}
			if len(paths) == 0 {
				return nil, fmt.Errorf("branch %s has no paths", step.ID)
			}
			for _, path := range paths {
				inner, err := collectSteps(self, path.Steps, depth, stack, catalog, seq)
				if err != nil {
					return nil, err
				}
				out = append(out, inner...)
			}
		default:
			return nil, fmt.Errorf("unknown pipeline step %q", step.Kind)
		}
	}
	return out, nil
}

func seatSteps(self string, steps []pipelineStep, depth int, stack map[string]bool, catalog map[string]Aggregation, text string, labels map[string]string, seq *int) ([]RouteSeat, []AggregationMember, error) {
	var seats []RouteSeat
	var members []AggregationMember
	for _, step := range steps {
		switch step.Kind {
		case "member":
			member, err := leafMember(step, seq)
			if err != nil {
				return nil, nil, err
			}
			members = append(members, member)
			seats = append(seats, RouteSeat{ID: step.ID, Kind: "member", Priority: member.Priority, Weight: member.Weight, Member: member})
		case "group":
			nested, err := openGroup(self, step, depth, stack, catalog)
			if err != nil {
				return nil, nil, err
			}
			seat := RouteSeat{ID: step.ID, Kind: "group", Priority: *seq, Weight: weightOf(step.Weight), GroupID: nested.ID, GroupStrategy: nested.Strategy}
			if strings.TrimSpace(nested.Document) == "" {
				for _, member := range nested.Members {
					member.Priority = *seq
					*seq++
					seat.GroupMembers = append(seat.GroupMembers, member)
					members = append(members, member)
				}
			} else {
				doc, err := parsePipeline(nested.Document)
				if err != nil {
					return nil, nil, err
				}
				chosen, _ := selectSteps(doc.Steps, text, labels)
				stack[nested.ID] = true
				innerSeats, innerMembers, err := seatSteps(nested.ID, chosen, depth+1, stack, catalog, text, labels, seq)
				delete(stack, nested.ID)
				if err != nil {
					return nil, nil, err
				}
				if hasGroup(innerSeats) {
					seat.GroupSeats = innerSeats
				} else {
					seat.GroupMembers = innerMembers
				}
				members = append(members, innerMembers...)
			}
			if len(seat.GroupMembers) == 0 && len(seat.GroupSeats) == 0 {
				return nil, nil, fmt.Errorf("aggregation %s has no reachable members", nested.ID)
			}
			seats = append(seats, seat)
		case "branch":
			chosen, _ := selectSteps([]pipelineStep{step}, text, labels)
			innerSeats, innerMembers, err := seatSteps(self, chosen, depth, stack, catalog, text, labels, seq)
			if err != nil {
				return nil, nil, err
			}
			seats = append(seats, innerSeats...)
			members = append(members, innerMembers...)
		default:
			return nil, nil, fmt.Errorf("unknown pipeline step %q", step.Kind)
		}
	}
	return seats, members, nil
}

func selectSteps(steps []pipelineStep, text string, labels map[string]string) ([]pipelineStep, string) {
	var out []pipelineStep
	path := ""
	for _, step := range steps {
		if step.Kind != "branch" {
			out = append(out, step)
			continue
		}
		chosen, label := choosePath(step, text, labels)
		if path == "" {
			path = label
		}
		out = append(out, chosen...)
	}
	return out, path
}

func choosePath(step pipelineStep, text string, labels map[string]string) ([]pipelineStep, string) {
	paths := step.Paths
	if len(step.Fallback) > 0 {
		paths = append(paths, pipelinePath{Label: "Default", Fallback: true, Steps: step.Fallback})
	}
	if step.Classifier != nil && labels != nil {
		if label := strings.TrimSpace(labels[classifierKey(*step.Classifier)]); label != "" {
			for _, path := range paths {
				if path.Fallback {
					continue
				}
				if strings.EqualFold(path.Intent, label) || strings.EqualFold(path.Label, label) {
					return path.Steps, path.Label
				}
			}
		}
	}
	lower := strings.ToLower(text)
	for _, path := range paths {
		if path.Fallback {
			continue
		}
		for _, word := range path.Keywords {
			word = strings.ToLower(strings.TrimSpace(word))
			if word != "" && strings.Contains(lower, word) {
				return path.Steps, path.Label
			}
		}
	}
	for _, path := range paths {
		if path.Fallback {
			return path.Steps, path.Label
		}
	}
	if len(paths) > 0 {
		last := paths[len(paths)-1]
		return last.Steps, last.Label
	}
	return nil, ""
}

func openGroup(self string, step pipelineStep, depth int, stack map[string]bool, catalog map[string]Aggregation) (Aggregation, error) {
	if depth+1 > MaxPipelineDepth {
		return Aggregation{}, fmt.Errorf("aggregation nest is deeper than %d", MaxPipelineDepth)
	}
	id := strings.TrimSpace(step.AggregationID)
	if id == "" {
		return Aggregation{}, fmt.Errorf("group step %s needs an aggregation id", step.ID)
	}
	if id == self || stack[id] {
		return Aggregation{}, fmt.Errorf("aggregation %s refers to itself", id)
	}
	nested, ok := catalog[id]
	if !ok || !nested.Enabled {
		return Aggregation{}, fmt.Errorf("aggregation %s is not available", id)
	}
	return nested, nil
}

func leafMember(step pipelineStep, seq *int) (AggregationMember, error) {
	if strings.TrimSpace(step.Provider) == "" || strings.TrimSpace(step.Model) == "" {
		return AggregationMember{}, fmt.Errorf("member step %s needs a provider and a model", step.ID)
	}
	member := AggregationMember{
		Provider:  step.Provider,
		Region:    step.Region,
		Model:     step.Model,
		AccountID: strings.TrimSpace(step.AccountID),
		Priority:  *seq,
		Weight:    weightOf(step.Weight),
	}
	*seq++
	return member, nil
}

func weightOf(weight int) int {
	if weight <= 0 {
		return 100
	}
	return weight
}

func validateClassifier(c *pipelineClassifier, catalog map[string]Aggregation) error {
	if c == nil {
		return nil
	}
	provider := strings.ToLower(strings.TrimSpace(c.Provider))
	model := strings.TrimSpace(c.Model)
	if provider == "" || model == "" {
		return fmt.Errorf("classifier needs a provider and a model")
	}
	if provider == AggregationProvider {
		return fmt.Errorf("classifier must be a concrete model")
	}
	if _, ok := catalog[provider]; ok {
		return fmt.Errorf("classifier must be a concrete model")
	}
	c.Provider = provider
	c.Model = model
	c.Region = strings.ToLower(strings.TrimSpace(c.Region))
	return nil
}

func classifierKey(c pipelineClassifier) string {
	return ClassifierTarget{Provider: c.Provider, Region: c.Region, Model: c.Model}.Key()
}

func walkClassifiers(self string, steps []pipelineStep, depth int, stack map[string]bool, catalog map[string]Aggregation, seen map[string]bool, out *[]ClassifierTarget) error {
	for _, step := range steps {
		switch step.Kind {
		case "branch":
			if err := validateClassifier(step.Classifier, catalog); err != nil {
				return err
			}
			if step.Classifier != nil {
				target := ClassifierTarget{Provider: step.Classifier.Provider, Region: step.Classifier.Region, Model: step.Classifier.Model}
				if !seen[target.Key()] {
					seen[target.Key()] = true
					*out = append(*out, target)
				}
			}
			for _, path := range step.Paths {
				if err := walkClassifiers(self, path.Steps, depth, stack, catalog, seen, out); err != nil {
					return err
				}
			}
			if err := walkClassifiers(self, step.Fallback, depth, stack, catalog, seen, out); err != nil {
				return err
			}
		case "group":
			nested, err := openGroup(self, step, depth, stack, catalog)
			if err != nil {
				return err
			}
			if strings.TrimSpace(nested.Document) == "" {
				continue
			}
			doc, err := parsePipeline(nested.Document)
			if err != nil {
				return err
			}
			stack[nested.ID] = true
			err = walkClassifiers(nested.ID, doc.Steps, depth+1, stack, catalog, seen, out)
			delete(stack, nested.ID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func filterMembers(members []AggregationMember, allowed map[string]bool) []AggregationMember {
	if allowed == nil {
		return members
	}
	out := make([]AggregationMember, 0, len(members))
	for _, member := range members {
		if allowed[strings.ToLower(member.Provider)] {
			out = append(out, member)
		}
	}
	return out
}

func filterSeats(seats []RouteSeat, allowed map[string]bool) []RouteSeat {
	if allowed == nil {
		return seats
	}
	out := make([]RouteSeat, 0, len(seats))
	for _, seat := range seats {
		switch seat.Kind {
		case "member":
			if allowed[strings.ToLower(seat.Member.Provider)] {
				out = append(out, seat)
			}
		case "group":
			seat.GroupMembers = filterMembers(seat.GroupMembers, allowed)
			seat.GroupSeats = filterSeats(seat.GroupSeats, allowed)
			if len(seat.GroupMembers) > 0 || len(seat.GroupSeats) > 0 {
				out = append(out, seat)
			}
		}
	}
	return out
}

func hasGroup(seats []RouteSeat) bool {
	for _, seat := range seats {
		if seat.Kind == "group" || hasGroup(seat.GroupSeats) {
			return true
		}
	}
	return false
}
