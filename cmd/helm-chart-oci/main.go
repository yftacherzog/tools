// Command helm-chart-oci is a spike placeholder for the Go Helm chart OCI builder.
package main

import (
	"fmt"
	"os"

	"github.com/google/go-containerregistry/pkg/name"
	"helm.sh/helm/v3/pkg/chart"
	_ "oras.land/oras-go/v2/registry/remote"
	"gopkg.in/yaml.v3"
)

func main() {
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte("x: 1"), &parsed); err != nil {
		fmt.Fprintf(os.Stderr, "yaml: %v\n", err)
		os.Exit(1)
	}
	_ = chart.Metadata{Name: "hello"}
	_, err := name.ParseReference("example.com/repo:tag")
	if err != nil {
		fmt.Fprintf(os.Stderr, "name: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("helm-chart-oci: hello")
}
