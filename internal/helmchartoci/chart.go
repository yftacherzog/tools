package helmchartoci

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type chartMetadata struct {
	Name string `yaml:"name"`
}

// ChartNameFromFile reads the chart name from Chart.yaml without modifying the file.
func ChartNameFromFile(chartDir string) (string, error) {
	path := filepath.Join(chartDir, "Chart.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read Chart.yaml: %w", err)
	}

	var meta chartMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return "", fmt.Errorf("parse Chart.yaml: %w", err)
	}
	if meta.Name == "" {
		return "", fmt.Errorf("parse Chart.yaml: name field not found")
	}
	return meta.Name, nil
}

// OverwriteChartNameEnabled returns whether Chart.yaml name should be rewritten
// from IMAGE. Nil defaults to true to preserve build-helm-chart-oci-ta 0.3 behavior.
func OverwriteChartNameEnabled(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

// ParsePushChartToImageRepository interprets Tekton-style boolean strings. An
// empty value defaults to false.
func ParsePushChartToImageRepository(value string) (bool, error) {
	return parseBoolParam(value, false, "PUSH_CHART_TO_IMAGE_REPOSITORY")
}

// EffectivePushChartToImageRepository applies the Tekton task contract:
// PUSH_CHART_TO_IMAGE_REPOSITORY is honored only when OVERWRITE_CHART_NAME is
// false. When overwrite is enabled, a requested push-to-image-repository is
// ignored with a warning on stderr.
func EffectivePushChartToImageRepository(pushToImageRepo bool, overwriteChartName *bool) bool {
	if !pushToImageRepo {
		return false
	}
	if OverwriteChartNameEnabled(overwriteChartName) {
		fmt.Fprintln(os.Stderr, "PUSH_CHART_TO_IMAGE_REPOSITORY is ignored when OVERWRITE_CHART_NAME is true")
		return false
	}
	return true
}

// ParseOverwriteChartName interprets Tekton-style boolean strings. An empty value
// defaults to overwrite=true to preserve build-helm-chart-oci-ta 0.3 behavior.
func ParseOverwriteChartName(value string) (bool, error) {
	return parseBoolParam(value, true, "OVERWRITE_CHART_NAME")
}

func parseBoolParam(value string, defaultValue bool, paramName string) (bool, error) {
	if value == "" {
		return defaultValue, nil
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s value %q", paramName, value)
	}
}

// ResolveChartName returns the chart name used for packaging and OCI push. When
// overwrite is true, Chart.yaml name is rewritten from the IMAGE repo basename
// (0.3 behavior). When false, the existing Chart.yaml name is preserved.
func ResolveChartName(chartDir, image string, overwrite bool) (string, error) {
	if overwrite {
		name, err := ChartNameFromImage(image)
		if err != nil {
			return "", err
		}
		if err := SetChartName(chartDir, name); err != nil {
			return "", err
		}
		return name, nil
	}
	return ChartNameFromFile(chartDir)
}

// SetChartName rewrites Chart.yaml name to match the output OCI repository.
func SetChartName(chartDir, name string) error {
	path := filepath.Join(chartDir, "Chart.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Chart.yaml: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse Chart.yaml: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("parse Chart.yaml: empty document")
	}

	if !setMappingValue(root.Content[0], "name", name) {
		return fmt.Errorf("parse Chart.yaml: name field not found")
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func setMappingValue(node *yaml.Node, key, value string) bool {
	if node == nil || node.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			node.Content[i+1].SetString(value)
			return true
		}
	}
	return false
}

// ResolveAppVersion returns APP_VERSION when set, otherwise commitSHA.
func ResolveAppVersion(appVersion, commitSHA string) string {
	if appVersion != "" {
		return appVersion
	}
	return commitSHA
}
