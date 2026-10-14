package draftevidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// validateV1Differential is a test-only comparator. It does not project,
// persist, register, or generate a runtime fact. It proves that the candidate
// references the same V1 execution, terminal, usage, cost, and pricing
// finality rather than duplicating or reinterpreting them.
func validateV1Differential(repositoryRoot string, fixture map[string]any) error {
	differential := fixture["v1_differential"].(map[string]any)
	sourcePath := differential["source_path"].(string)
	expectedDigest := differential["source_content_sha256"].(string)
	origin := fixture["origin"].(map[string]any)
	sourceIdentity := origin["source"].(map[string]any)
	if sourceIdentity["path"] != sourcePath || sourceIdentity["content_sha256"] != expectedDigest {
		return errors.New("V1 differential input is not the fixture Source identity")
	}
	body, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(sourcePath)))
	if err != nil {
		return fmt.Errorf("read V1 differential input: %w", err)
	}
	digest := sha256.Sum256(body)
	if actual := "sha256:" + hex.EncodeToString(digest[:]); actual != expectedDigest {
		return fmt.Errorf("V1 differential input digest=%s want=%s", actual, expectedDigest)
	}
	var source struct {
		RequestID string `json:"request_id"`
		Terminal  struct {
			Status string `json:"status"`
		} `json:"terminal"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
		Money struct {
			ChargedCostMicros string `json:"charged_cost_micros"`
			PricingVersion    string `json:"pricing_version"`
		} `json:"money"`
	}
	if err := json.Unmarshal(body, &source); err != nil {
		return fmt.Errorf("decode V1 differential input: %w", err)
	}
	evidenceValue := fixture["evidence"].(map[string]any)
	identity := evidenceValue["execution_identity"].(map[string]any)
	if identity["execution_id"] != source.RequestID {
		return errors.New("V1 differential duplicated or changed execution identity")
	}
	executionSourceIdentity := "unirouter.completed-execution.v1:" + source.RequestID
	if identity["source_fact_identity"] != executionSourceIdentity ||
		identity["idempotency_key"] != executionSourceIdentity {
		return errors.New("V1 differential changed Source execution or idempotency identity")
	}
	facts := evidenceValue["facts"].([]any)
	var terminal, usage, cost map[string]any
	for _, raw := range facts {
		fact := raw.(map[string]any)
		switch fact["kind"] {
		case "terminal_outcome":
			if terminal != nil {
				return errors.New("V1 differential duplicated terminal outcome")
			}
			terminal = fact
		case "usage":
			if usage != nil {
				return errors.New("V1 differential duplicated usage")
			}
			usage = fact
		case "cost":
			if cost != nil {
				return errors.New("V1 differential duplicated cost")
			}
			cost = fact
		}
	}
	if terminal == nil || usage == nil || cost == nil {
		return errors.New("V1 differential requires one terminal, usage, and cost reference")
	}
	terminalValue := terminal["value"].(map[string]any)
	usageValue := usage["value"].(map[string]any)
	costValue := cost["value"].(map[string]any)
	terminalProvenance := terminal["provenance"].(map[string]any)
	usageProvenance := usage["provenance"].(map[string]any)
	costProvenance := cost["provenance"].(map[string]any)
	if terminalProvenance["source_fact_identity"] != executionSourceIdentity ||
		usageProvenance["source_fact_identity"] != executionSourceIdentity+":usage" ||
		costProvenance["source_fact_identity"] != executionSourceIdentity+":money" {
		return errors.New("V1 differential duplicated terminal, usage, or cost Source identity")
	}
	if terminalValue["status"] != source.Terminal.Status ||
		int64(usageValue["prompt_tokens"].(float64)) != source.Usage.PromptTokens ||
		int64(usageValue["completion_tokens"].(float64)) != source.Usage.CompletionTokens ||
		costValue["charged_cost_micros"] != source.Money.ChargedCostMicros ||
		costValue["pricing_version"] != source.Money.PricingVersion ||
		costValue["pricing_finality"] != "source_final" {
		return errors.New("V1 differential changed terminal, usage, cost, or pricing finality")
	}
	return nil
}
