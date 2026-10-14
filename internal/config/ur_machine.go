package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Both purposes share one paired identity and the Connector's single origin.
const (
	URMachineInstallationEnv = "XINSIGHT_UR_MACHINE_INSTALLATION_ID"
	URMachineSourceEnv       = "XINSIGHT_UR_MACHINE_SOURCE_INSTANCE"
	URMachineOrgEnv          = "XINSIGHT_UR_MACHINE_ORG_ID"
	URMachineCAEnv           = "XINSIGHT_UR_MACHINE_CA_FILE"
	URPolicyFileEnv          = "XINSIGHT_UR_POLICY_BEARER_FILE"
	UREnrollFileEnv          = "XINSIGHT_UR_ENROLL_BEARER_FILE"
)

var machineTokenPattern = regexp.MustCompile(`^ur-(xpm1|xse1)-[a-f0-9]{64}$`)

func ValidateURMachineToken(token, purpose string) error {
	if !machineTokenPattern.MatchString(token) || !strings.HasPrefix(token, "ur-"+purpose+"-") {
		return fmt.Errorf("machine credential has an invalid purpose or format")
	}
	return nil
}

// References are unquoted systemd assignments, not arbitrary shell text.
func ValidateManagedReference(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, " \t\r\n\\\"'%$") {
		return fmt.Errorf("managed reference must be an absolute clean literal path")
	}
	return nil
}
