package push

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/go-containerregistry/pkg/registry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/getter"
	helmregistry "helm.sh/helm/v4/pkg/registry"
)

const (
	testDepChartName    = "subchart"
	testDepChartVersion = "1.0.0"
)

func testHelmSettings() *cli.EnvSettings {
	settings := cli.New()
	cacheDir := GinkgoT().TempDir()
	settings.RepositoryConfig = filepath.Join(cacheDir, "repositories.yaml")
	settings.RepositoryCache = filepath.Join(cacheDir, "repository")
	settings.ContentCache = filepath.Join(cacheDir, "content")
	Expect(os.MkdirAll(settings.RepositoryCache, 0o755)).To(Succeed())
	Expect(os.MkdirAll(settings.ContentCache, 0o755)).To(Succeed())
	return settings
}

func packageTestChart(name, version string) []byte {
	chartDir := GinkgoT().TempDir()
	writeSubchart(chartDir, name, version)
	pkg := action.NewPackage()
	archive, err := pkg.Run(chartDir, nil)
	Expect(err).NotTo(HaveOccurred())
	data, err := os.ReadFile(archive)
	Expect(err).NotTo(HaveOccurred())
	return data
}

func chartArchiveName(name, version string) string {
	return fmt.Sprintf("%s-%s.tgz", name, version)
}

type httpChartRepo struct {
	URL      string
	Settings *cli.EnvSettings
	Cleanup  func()
}

type multiChartEntry struct {
	version string
	archive []byte
}

func startHTTPChartRepo(archive []byte, name, version string, useTLS bool) httpChartRepo {
	settings := testHelmSettings()
	var baseURL string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/index.yaml"):
			_, err := fmt.Fprintf(w, `apiVersion: v1
entries:
  %s:
  - name: %s
    version: %s
    urls:
    - %s/charts/%s
`, name, name, version, baseURL, chartArchiveName(name, version))
			Expect(err).NotTo(HaveOccurred())
		case strings.HasSuffix(r.URL.Path, "/"+chartArchiveName(name, version)):
			_, err := w.Write(archive)
			Expect(err).NotTo(HaveOccurred())
		default:
			http.NotFound(w, r)
		}
	})

	var srv *httptest.Server
	if useTLS {
		srv = httptest.NewTLSServer(handler)
	} else {
		srv = httptest.NewServer(handler)
	}
	baseURL = srv.URL

	return httpChartRepo{
		URL:      srv.URL,
		Settings: settings,
		Cleanup:  srv.Close,
	}
}

type sameHostChartEntry struct {
	pathPrefix string
	version    string
	archive    []byte
}

type sameHostHTTPServer struct {
	Settings *cli.EnvSettings
	BaseURL  string
	Cleanup  func()
}

func (s sameHostHTTPServer) RepoURL(pathPrefix string) string {
	return s.BaseURL + "/" + pathPrefix
}

func startSameHostMultiPathHTTPServer(charts map[string]sameHostChartEntry) sameHostHTTPServer {
	settings := testHelmSettings()
	var baseURL string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for chartName, chart := range charts {
			prefix := chart.pathPrefix
			indexPath := "/" + prefix + "/index.yaml"
			archiveName := chartArchiveName(chartName, chart.version)
			archivePath := "/" + prefix + "/charts/" + archiveName
			repoURL := baseURL + "/" + prefix

			switch {
			case r.URL.Path == indexPath:
				_, err := fmt.Fprintf(w, `apiVersion: v1
entries:
  %s:
  - name: %s
    version: %s
    urls:
    - %s/charts/%s
`, chartName, chartName, chart.version, repoURL, archiveName)
				Expect(err).NotTo(HaveOccurred())
				return
			case r.URL.Path == archivePath:
				_, err := w.Write(chart.archive)
				Expect(err).NotTo(HaveOccurred())
				return
			}
		}
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(handler)
	baseURL = srv.URL

	return sameHostHTTPServer{
		Settings: settings,
		BaseURL:  baseURL,
		Cleanup:  srv.Close,
	}
}

func startMultiChartHTTPRepo(charts map[string]multiChartEntry, useTLS bool) httpChartRepo {
	settings := testHelmSettings()
	var baseURL string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/index.yaml") {
			var entries strings.Builder
			for name, chart := range charts {
				archive := chartArchiveName(name, chart.version)
				_, err := fmt.Fprintf(&entries, `  %s:
  - name: %s
    version: %s
    urls:
    - %s/charts/%s
`, name, name, chart.version, baseURL, archive)
				Expect(err).NotTo(HaveOccurred())
			}
			_, err := fmt.Fprintf(w, "apiVersion: v1\nentries:\n%s", entries.String())
			Expect(err).NotTo(HaveOccurred())
			return
		}
		for name, chart := range charts {
			archive := chartArchiveName(name, chart.version)
			if strings.HasSuffix(r.URL.Path, "/"+archive) {
				_, err := w.Write(chart.archive)
				Expect(err).NotTo(HaveOccurred())
				return
			}
		}
		http.NotFound(w, r)
	})

	var srv *httptest.Server
	if useTLS {
		srv = httptest.NewTLSServer(handler)
	} else {
		srv = httptest.NewServer(handler)
	}
	baseURL = srv.URL

	return httpChartRepo{
		URL:      srv.URL,
		Settings: settings,
		Cleanup:  srv.Close,
	}
}

type ociChartRegistry struct {
	RepositoryURL string
	Settings      *cli.EnvSettings
	Registry      *helmregistry.Client
	Cleanup       func()
}

func startOCIChartRegistry(archive []byte, name, version string) ociChartRegistry {
	settings := testHelmSettings()
	srv := httptest.NewServer(registry.New())
	client, err := helmregistry.NewClient(helmregistry.ClientOptPlainHTTP())
	Expect(err).NotTo(HaveOccurred())

	host := srv.Listener.Addr().String()
	dest := fmt.Sprintf("oci://%s/charts/%s:%s", host, name, version)
	_, err = client.Push(archive, dest)
	Expect(err).NotTo(HaveOccurred())

	return ociChartRegistry{
		RepositoryURL: fmt.Sprintf("oci://%s/charts", host),
		Settings:      settings,
		Registry:      client,
		Cleanup:       srv.Close,
	}
}

// isolatedDependencyConfig uses production getters with a per-test Helm config dir so
// dependency builds do not read or update the developer's ~/.config/helm/repositories.yaml.
func isolatedDependencyConfig(settings *cli.EnvSettings) dependencyManagerConfig {
	return dependencyManagerConfig{settings: settings}
}

func dependencyConfigForHTTPRepo(repo httpChartRepo, useTLS bool) dependencyManagerConfig {
	var getters getter.Providers
	if useTLS {
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		getters = getter.All(repo.Settings, getter.WithTransport(transport))
	} else {
		getters = getter.All(repo.Settings)
	}
	return dependencyManagerConfig{
		settings: repo.Settings,
		getters:  getters,
		out:      io.Discard,
	}
}

func dependencyConfigForOCIRegistry(reg ociChartRegistry) dependencyManagerConfig {
	return dependencyManagerConfig{
		settings:       reg.Settings,
		getters:        getter.All(reg.Settings),
		registryClient: reg.Registry,
		out:            io.Discard,
	}
}
