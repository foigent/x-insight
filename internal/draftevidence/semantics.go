package draftevidence

import (
	"errors"
	"fmt"
	"strings"
)

func validateFixtureSemantics(repositoryRoot string, manifest Manifest, fixture map[string]any) error {
	origin, _ := fixture["origin"].(map[string]any)
	if origin["kind"] == "source_derived" {
		if err := validateSourceDerivedOrigin(manifest, origin); err != nil {
			return err
		}
	}
	evidenceValue, ok := fixture["evidence"].(map[string]any)
	if !ok {
		return errors.New("evidence object is required")
	}
	if evidenceValue["source_authority_status"] != manifest.AuthorityStatus {
		return errors.New("fixture Source authority status does not match the fixed manifest")
	}
	operation, _ := evidenceValue["operation"].(map[string]any)
	attempts, _ := evidenceValue["attempts"].([]any)
	seenAttempts := map[string]struct{}{}
	for index, raw := range attempts {
		attempt := raw.(map[string]any)
		ordinal := int(attempt["ordinal"].(float64))
		if ordinal != index+1 {
			return errors.New("attempt ordinals must be contiguous and ordered from one")
		}
		id := attempt["attempt_id"].(string)
		if _, duplicate := seenAttempts[id]; duplicate {
			return errors.New("attempt identity is duplicated")
		}
		seenAttempts[id] = struct{}{}
	}

	facts, _ := evidenceValue["facts"].([]any)
	factByID := map[string]map[string]any{}
	terminalCount := 0
	predispatchCount := 0
	usageFacts := []map[string]any{}
	for index, raw := range facts {
		fact := raw.(map[string]any)
		id := fact["fact_id"].(string)
		if _, duplicate := factByID[id]; duplicate {
			return errors.New("fact identity is duplicated")
		}
		factByID[id] = fact
		if int(fact["sequence"].(float64)) != index+1 {
			return errors.New("fact sequence must be contiguous and ordered from one")
		}
		kind := fact["kind"].(string)
		if kind == "terminal_outcome" {
			terminalCount++
		}
		if kind == "predispatch_outcome" {
			predispatchCount++
		}
		if kind == "usage" {
			usageFacts = append(usageFacts, fact)
		}
		if kind == "gap" {
			if _, hasValue := fact["value"]; hasValue || strings.TrimSpace(stringField(fact, "unknown_reason")) == "" {
				return errors.New("gap facts require unknown_reason and must not carry value")
			}
		} else if _, hasValue := fact["value"]; !hasValue {
			return fmt.Errorf("fact %s requires value", id)
		}
		if containsSensitiveKey(fact["value"]) {
			return fmt.Errorf("fact %s contains prohibited sensitive/raw payload field", id)
		}
	}
	if predispatchCount == 1 {
		if terminalCount != 0 || len(attempts) != 0 || operation["family"] != "predispatch" {
			return errors.New("predispatch denial must have no provider attempt or terminal outcome")
		}
	} else if predispatchCount != 0 || terminalCount != 1 {
		return errors.New("candidate must contain exactly one predispatch or terminal outcome fact")
	}
	if len(usageFacts) > 1 && !validUsageRevision(usageFacts, factByID) {
		return errors.New("usage cardinality exceeds one without same-Source revision semantics")
	}

	for _, raw := range facts {
		fact := raw.(map[string]any)
		kind := fact["kind"].(string)
		lifecycle := fact["lifecycle"].(string)
		relations, _ := fact["relationships"].([]any)
		if (kind == "refund" || kind == "reconciliation") && lifecycle != "post_terminal" {
			return fmt.Errorf("%s must be post_terminal", kind)
		}
		for _, relationRaw := range relations {
			relation := relationRaw.(map[string]any)
			targetID := relation["target_fact_id"].(string)
			target, exists := factByID[targetID]
			if !exists {
				return fmt.Errorf("relationship target %s does not exist", targetID)
			}
			typeName := relation["type"].(string)
			sourceDeclared, _ := relation["source_declared"].(bool)
			sourceEventIdentity, _ := relation["source_event_identity"].(string)
			targetProvenance := target["provenance"].(map[string]any)
			targetSourceIdentity := targetProvenance["source_fact_identity"].(string)
			factProvenance := fact["provenance"].(map[string]any)
			switch typeName {
			case "caused_by":
				if !sourceDeclared || sourceEventIdentity != targetSourceIdentity || factProvenance["mode"] != "source_declared" {
					return errors.New("caused_by requires explicit Source declaration and exact parent event identity")
				}
			case "derived_from":
				if factProvenance["mode"] == "source_declared" && (!sourceDeclared || sourceEventIdentity != targetSourceIdentity) {
					return errors.New("Source derived_from requires exact upstream provenance")
				}
				if factProvenance["mode"] == "xinsight_test_derived" {
					algorithm, ok := factProvenance["algorithm"].(map[string]any)
					if !ok {
						return errors.New("X-Insight derived fact requires algorithm identity")
					}
					inputs, _ := algorithm["inputs"].([]any)
					if !containsString(inputs, targetSourceIdentity) {
						return errors.New("X-Insight derived fact algorithm inputs must bind the exact upstream provenance")
					}
				}
			case "supersedes":
				if kind != "correction" || lifecycle != "correction" || !sourceDeclared || strings.TrimSpace(stringField(relation, "reason")) == "" {
					return errors.New("supersedes is reserved for explicit correction facts")
				}
				if target["kind"] == "terminal_outcome" {
					return errors.New("correction must not erase a terminal outcome")
				}
			case "revision_of":
				if lifecycle != "correction" || !sourceDeclared || sourceEventIdentity != targetSourceIdentity ||
					factProvenance["source_fact_identity"] != targetSourceIdentity {
					return errors.New("revision_of requires the same Source fact identity and explicit Source revision")
				}
			}
			if (kind == "refund" || kind == "reconciliation") && typeName != "relates_to" {
				return fmt.Errorf("%s must relate_to rather than replace the terminal lifecycle", kind)
			}
		}
	}

	delivery := evidenceValue["delivery"].(map[string]any)
	deliveryCount := int(delivery["delivery_count"].(float64))
	businessFactCount := int(delivery["business_fact_count"].(float64))
	if businessFactCount != len(facts) {
		return errors.New("business_fact_count must equal the unique business facts in the fixture")
	}
	if deliveryCount > 1 && businessFactCount != 1 {
		return errors.New("duplicate delivery must not mint a second business fact")
	}
	_ = repositoryRoot
	return nil
}

func containsString(values []any, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func validateSourceDerivedOrigin(manifest Manifest, origin map[string]any) error {
	source, ok := origin["source"].(map[string]any)
	if !ok {
		return errors.New("source_derived fixture requires exact Source identity")
	}
	if source["repository"] != manifest.SourceContext.Repository ||
		source["commit"] != manifest.SourceContext.Commit || source["tree"] != manifest.SourceContext.Tree {
		return errors.New("source_derived fixture repository/commit/tree is not the fixed Source identity")
	}
	if source["ref"] != manifest.SourceContext.Commit {
		return errors.New("source_derived fixture ref is not the fixed Source commit")
	}
	path, _ := source["path"].(string)
	digest, _ := source["content_sha256"].(string)
	version, _ := source["version"].(string)
	type sourceIdentity struct {
		digest  string
		version string
	}
	allowed := map[string]sourceIdentity{}
	for _, observation := range manifest.SourceContext.Observations {
		allowed[observation.Path] = sourceIdentity{
			digest: observation.ContentSHA256, version: observation.Version,
		}
	}
	for _, fixture := range manifest.CompletedExecutionV1.Fixtures {
		allowed[fixture.Path] = sourceIdentity{
			digest: fixture.ContentSHA256, version: "unirouter.completed-execution.v1",
		}
	}
	identity, exists := allowed[path]
	if !exists || identity.digest != digest {
		return errors.New("source_derived fixture path/content SHA is not fixed by the manifest")
	}
	if identity.version != version {
		return errors.New("source_derived fixture version is not fixed by the manifest")
	}
	return nil
}

func validUsageRevision(facts []map[string]any, factByID map[string]map[string]any) bool {
	if len(facts) != 2 {
		return false
	}
	second := facts[1]
	secondProvenance := second["provenance"].(map[string]any)
	relations, _ := second["relationships"].([]any)
	for _, raw := range relations {
		relation := raw.(map[string]any)
		if relation["type"] != "revision_of" {
			continue
		}
		target := factByID[relation["target_fact_id"].(string)]
		if target == nil {
			return false
		}
		targetProvenance := target["provenance"].(map[string]any)
		return secondProvenance["source_fact_identity"] == targetProvenance["source_fact_identity"]
	}
	return false
}

func containsSensitiveKey(value any) bool {
	forbidden := map[string]struct{}{
		"raw_prompt": {}, "raw_response": {}, "prompt": {}, "response": {},
		"api_key": {}, "authorization": {}, "secret": {}, "password": {},
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, nested := range typed {
			if _, rejected := forbidden[strings.ToLower(key)]; rejected {
				return true
			}
			if containsSensitiveKey(nested) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsSensitiveKey(nested) {
				return true
			}
		}
	}
	return false
}

func stringField(value map[string]any, key string) string {
	text, _ := value[key].(string)
	return text
}
