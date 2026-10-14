package buildinfo

import (
	"encoding/json"
	"fmt"
	"io"
)

var (
	ProductVersion = "0.2.0-dev"
	Commit         = "unknown"
	BuildTime      = "unknown"
	GoVersion      = "unknown"
	ManualVersion  = "unreleased/manual-r0"
)

const (
	ProductName         = "x-insight"
	ConfigSchemaVersion = "xinsight.config.v1"
	DatabaseSchemaMin   = "23"
	DatabaseSchemaMax   = "23"
	PostgresMajorMin    = "16"
	PostgresMajorMax    = "16"
	APIVersion          = "xinsight.investigation.v1"
	AccessAPIVersion    = "xinsight.access.v1"
	EvidenceVersion     = "m0.1"
	ReceiptVersion      = "xinsight.import-receipt.v1"
)

type Info struct {
	ProductName         string `json:"product_name"`
	ProductVersion      string `json:"product_version"`
	Commit              string `json:"commit"`
	BuildTime           string `json:"build_time"`
	GoVersion           string `json:"go_version"`
	DatabaseSchemaMin   string `json:"database_schema_min"`
	DatabaseSchemaMax   string `json:"database_schema_max"`
	PostgresMajorMin    string `json:"postgres_major_min"`
	PostgresMajorMax    string `json:"postgres_major_max"`
	ConfigSchemaVersion string `json:"config_schema_version"`
	APIVersion          string `json:"api_version"`
	AccessAPIVersion    string `json:"access_api_version"`
	EvidenceSchema      string `json:"evidence_schema_version"`
	ReceiptSchema       string `json:"receipt_schema_version"`
	ManualVersion       string `json:"manual_version"`
}

func Current() Info {
	return Info{
		ProductName:         ProductName,
		ProductVersion:      ProductVersion,
		Commit:              Commit,
		BuildTime:           BuildTime,
		GoVersion:           GoVersion,
		DatabaseSchemaMin:   DatabaseSchemaMin,
		DatabaseSchemaMax:   DatabaseSchemaMax,
		PostgresMajorMin:    PostgresMajorMin,
		PostgresMajorMax:    PostgresMajorMax,
		ConfigSchemaVersion: ConfigSchemaVersion,
		APIVersion:          APIVersion,
		AccessAPIVersion:    AccessAPIVersion,
		EvidenceSchema:      EvidenceVersion,
		ReceiptSchema:       ReceiptVersion,
		ManualVersion:       ManualVersion,
	}
}

func WriteText(writer io.Writer) error {
	info := Current()
	_, err := fmt.Fprintf(
		writer,
		"%s %s commit=%s build_time=%s go=%s db_schema=%s..%s postgres=%s..%s config=%s api=%s access_api=%s evidence=%s receipt=%s manual=%s\n",
		info.ProductName,
		info.ProductVersion,
		info.Commit,
		info.BuildTime,
		info.GoVersion,
		info.DatabaseSchemaMin,
		info.DatabaseSchemaMax,
		info.PostgresMajorMin,
		info.PostgresMajorMax,
		info.ConfigSchemaVersion,
		info.APIVersion,
		info.AccessAPIVersion,
		info.EvidenceSchema,
		info.ReceiptSchema,
		info.ManualVersion,
	)
	return err
}

func WriteJSON(writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(Current())
}

func HandleArgs(args []string, writer io.Writer) (bool, error) {
	if len(args) == 1 && args[0] == "--version" {
		return true, WriteText(writer)
	}
	if len(args) == 1 && args[0] == "version" {
		return true, WriteText(writer)
	}
	if len(args) == 2 && args[0] == "version" && args[1] == "--json" {
		return true, WriteJSON(writer)
	}
	if len(args) > 0 && (args[0] == "version" || args[0] == "--version") {
		return true, fmt.Errorf("usage: version [--json]")
	}
	return false, nil
}
