package push

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/getter"
	"helm.sh/helm/v4/pkg/helmpath"
	repov1 "helm.sh/helm/v4/pkg/repo/v1"
)

// repositoryNameFromURL derives a stable Helm repository name from a URL. Host-only
// URLs keep build-helm-chart-oci-ta 0.3 naming; URLs with a path append a short hash
// so different paths on the same host do not overwrite each other.
func repositoryNameFromURL(repoURL string) string {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return repositoryHostName(repoURL)
	}

	hostName := strings.ReplaceAll(parsed.Host, ".", "_")
	path := strings.TrimSuffix(parsed.Path, "/")
	if path == "" {
		return hostName
	}

	return hostName + "_" + repositoryNameHashSuffix(repoURL)
}

func repositoryHostName(repoURL string) string {
	name := repoURL
	name = strings.TrimPrefix(name, "https://")
	name = strings.TrimPrefix(name, "http://")
	if idx := strings.Index(name, "/"); idx >= 0 {
		name = name[:idx]
	}
	return strings.ReplaceAll(name, ".", "_")
}

func repositoryNameHashSuffix(repoURL string) string {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		sum := sha256.Sum256([]byte(repoURL))
		return hex.EncodeToString(sum[:4])
	}

	path := strings.TrimSuffix(parsed.Path, "/")
	if path == "" {
		return parsed.Scheme
	}

	normalized := parsed.Scheme + "://" + parsed.Host + path
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:4])
}

func isHTTPRepositoryURL(repoURL string) bool {
	parsed, err := url.Parse(repoURL)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// repositoryNamesForURLs assigns stable Helm repository names for a set of URLs.
// Host-only URLs keep 0.3 naming when unique; colliding host-only URLs (for example
// http and https on the same host) receive a scheme suffix.
func repositoryNamesForURLs(urls []string) map[string]string {
	names := make(map[string]string, len(urls))
	byBase := make(map[string][]string)

	for _, repoURL := range urls {
		base := repositoryNameFromURL(repoURL)
		byBase[base] = append(byBase[base], repoURL)
	}

	for base, group := range byBase {
		if len(group) == 1 {
			names[group[0]] = base
			continue
		}
		for _, repoURL := range group {
			names[repoURL] = uniquifyRepositoryName(base, repoURL)
		}
	}
	return names
}

func uniquifyRepositoryName(baseName, repoURL string) string {
	return baseName + "_" + repositoryNameHashSuffix(repoURL)
}

func repositoryURLEqual(a, b string) bool {
	return a == b
}

func findRepositoryByURL(repoFile *repov1.File, repoURL string) *repov1.Entry {
	for _, entry := range repoFile.Repositories {
		if entry != nil && repositoryURLEqual(entry.URL, repoURL) {
			return entry
		}
	}
	return nil
}

// ensureRepositoryEntry mirrors helm repo add without --force-update: reuse an
// existing entry when the URL already matches, skip when name and URL match, and
// never overwrite a different URL registered under the same name.
func ensureRepositoryEntry(repoFile *repov1.File, name, repoURL string) (string, bool) {
	if existing := findRepositoryByURL(repoFile, repoURL); existing != nil {
		return existing.Name, false
	}

	candidate := name
	if existing := repoFile.Get(candidate); existing != nil {
		if repositoryURLEqual(existing.URL, repoURL) {
			return candidate, false
		}
		candidate = uniquifyRepositoryName(name, repoURL)
		for {
			existing = repoFile.Get(candidate)
			if existing == nil {
				break
			}
			if repositoryURLEqual(existing.URL, repoURL) {
				return candidate, false
			}
			sum := sha256.Sum256([]byte(candidate + repoURL))
			candidate = name + "_" + hex.EncodeToString(sum[:4])
		}
	}

	repoFile.Add(&repov1.Entry{
		Name: candidate,
		URL:  repoURL,
	})
	return candidate, true
}

func httpRepositoryURLsFromChart(meta chartYAML) []string {
	seen := make(map[string]struct{})
	var urls []string
	for _, dep := range meta.Dependencies {
		repo := strings.TrimSpace(dep.Repository)
		if repo == "" || !isHTTPRepositoryURL(repo) {
			continue
		}
		if _, ok := seen[repo]; ok {
			continue
		}
		seen[repo] = struct{}{}
		urls = append(urls, repo)
	}
	sort.Strings(urls)
	return urls
}

func chartDependencyRepositoryURLs(chartDir string) ([]string, error) {
	meta, err := readChartYAML(chartDir)
	if err != nil {
		return nil, err
	}
	return httpRepositoryURLsFromChart(meta), nil
}

// registerHTTPChartRepositories mirrors build-helm-chart-oci-ta 0.3: helm repo add
// for each unique HTTP(S) dependency repository, then helm repo update.
func registerHTTPChartRepositories(chartDir string, settings *cli.EnvSettings, getters getter.Providers) error {
	meta, err := readChartYAML(chartDir)
	if err != nil {
		return err
	}
	return registerHTTPChartRepositoriesFromChart(meta, settings, getters)
}

func registerHTTPChartRepositoriesFromChart(meta chartYAML, settings *cli.EnvSettings, getters getter.Providers) error {
	urls := httpRepositoryURLsFromChart(meta)
	if len(urls) == 0 {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(settings.RepositoryConfig), 0o755); err != nil {
		return fmt.Errorf("create repository config dir: %w", err)
	}

	var repoFile *repov1.File
	if _, err := os.Stat(settings.RepositoryConfig); err != nil {
		if os.IsNotExist(err) {
			repoFile = repov1.NewFile()
		} else {
			return fmt.Errorf("stat repositories file: %w", err)
		}
	} else {
		repoFile, err = repov1.LoadFile(settings.RepositoryConfig)
		if err != nil {
			return fmt.Errorf("load repositories file: %w", err)
		}
	}

	namesForURL := repositoryNamesForURLs(urls)
	repoNames := make(map[string]string, len(urls))
	modified := false
	for _, repoURL := range urls {
		name, changed := ensureRepositoryEntry(repoFile, namesForURL[repoURL], repoURL)
		repoNames[repoURL] = name
		if changed {
			modified = true
		}
	}

	if modified {
		if err := repoFile.WriteFile(settings.RepositoryConfig, 0o644); err != nil {
			return fmt.Errorf("write repositories file: %w", err)
		}
	}

	for _, repoURL := range urls {
		repoName := repoNames[repoURL]
		entry := repoFile.Get(repoName)
		if entry == nil {
			return fmt.Errorf("chart repository %q for %s missing after registration", repoName, repoURL)
		}
		chartRepo, err := repov1.NewChartRepository(entry, getters)
		if err != nil {
			return fmt.Errorf("create chart repository %q: %w", entry.Name, err)
		}
		chartRepo.CachePath = settings.RepositoryCache
		if _, err := chartRepo.DownloadIndexFile(); err != nil {
			return fmt.Errorf("update chart repository %q: %w", entry.Name, err)
		}
	}

	return nil
}

// repositoryIndexCachePath returns the Helm repository index cache path for a name.
func repositoryIndexCachePath(cacheDir, repoName string) string {
	return filepath.Join(cacheDir, helmpath.CacheIndexFile(repoName))
}
