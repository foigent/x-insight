package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foigent/x-insight/internal/config"
	"github.com/foigent/x-insight/internal/connectorcontrol"
)

func TestManagedURMachineLoadersAreSymmetricAndKeepPurposesSeparate(t *testing.T) {
	for _, key := range []string{config.URMachineInstallationEnv, config.URMachineSourceEnv, config.URMachineOrgEnv, config.URMachineCAEnv, config.URPolicyFileEnv, config.UREnrollFileEnv, connectorcontrol.UniRouterInstallOriginEnv, urPolicyOriginEnv, urPolicyInstallationEnv, urPolicySourceInstanceEnv, urPolicyOrgEnv, urPolicyCAFileEnv} {
		t.Setenv(key, "")
	}
	if c, _, err := loadDelegatedURPolicyClient(); err != nil || c != nil {
		t.Fatal("disabled policy", err)
	}
	if c, _, err := loadDelegatedUREnrollmentClient(); err != nil || c != nil {
		t.Fatal("disabled enrollment", err)
	}
	policy := filepath.Join(t.TempDir(), "policy.key")
	enroll := filepath.Join(t.TempDir(), "enroll.key")
	if err := os.WriteFile(policy, []byte("ur-xpm1-"+strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(enroll, []byte("ur-xse1-"+strings.Repeat("b", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(connectorcontrol.UniRouterInstallOriginEnv, "https://8.130.18.85")
	t.Setenv(config.URMachineInstallationEnv, "xci_AAAAAAAAAAAAAAAAAAAAAA")
	t.Setenv(config.URMachineSourceEnv, "source-instance")
	t.Setenv(config.URMachineOrgEnv, "2")
	t.Setenv(config.URPolicyFileEnv, policy)
	if c, id, err := loadDelegatedURPolicyClient(); err != nil || c == nil || id.OrgID != "2" {
		t.Fatal("policy", err)
	}
	if c, _, err := loadDelegatedUREnrollmentClient(); err != nil || c != nil {
		t.Fatal("absent enrollment purpose", err)
	}
	t.Setenv(config.UREnrollFileEnv, enroll)
	if c, id, err := loadDelegatedUREnrollmentClient(); err != nil || c == nil || id.SourceInstance != "source-instance" {
		t.Fatal("enrollment", err)
	}
	t.Setenv(config.URPolicyFileEnv, enroll)
	if _, _, err := loadDelegatedURPolicyClient(); err == nil {
		t.Fatal("wrong purpose accepted")
	}
	t.Setenv(config.URPolicyFileEnv, policy)
	t.Setenv(urPolicyOriginEnv, "https://other.example")
	if _, _, err := loadDelegatedURPolicyClient(); err == nil {
		t.Fatal("origin conflict accepted")
	}
	if _, _, err := loadDelegatedUREnrollmentClient(); err == nil {
		t.Fatal("enrollment ignored origin conflict")
	}
	t.Setenv(urPolicyOriginEnv, "")
	t.Setenv(config.URMachineOrgEnv, "")
	if _, _, err := loadDelegatedURPolicyClient(); err == nil {
		t.Fatal("policy accepted partial identity")
	}
	if _, _, err := loadDelegatedUREnrollmentClient(); err == nil {
		t.Fatal("enrollment accepted partial identity")
	}
}

func TestReviewBodyConfigCLIRejectsNonRootAndIncompleteArguments(t *testing.T) {
	if err := runReviewBodyConfig(nil, []string{"validate"}, false); err == nil {
		t.Fatal("nonroot accepted")
	}
	if err := runReviewBodyConfig(nil, []string{"unknown"}, true); err == nil {
		t.Fatal("unknown action accepted")
	}
}
