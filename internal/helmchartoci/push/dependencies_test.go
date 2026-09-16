package push

import (
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"helm.sh/helm/v4/pkg/getter"
	repov1 "helm.sh/helm/v4/pkg/repo/v1"
)

var _ = Describe("HTTP chart repositories", func() {
	DescribeTable("repositoryNameFromURL",
		func(repoURL, want string) {
			Expect(repositoryNameFromURL(repoURL)).To(Equal(want))
		},
		Entry("jetstack", "https://charts.jetstack.io", "charts_jetstack_io"),
		Entry("http repo", "http://127.0.0.1:8080", "127_0_0_1:8080"),
	)

	DescribeTable("repositoryHostName",
		func(repoURL, want string) {
			Expect(repositoryHostName(repoURL)).To(Equal(want))
		},
		Entry("https jetstack", "https://charts.jetstack.io", "charts_jetstack_io"),
		Entry("http with port and path", "http://127.0.0.1:8080/stable", "127_0_0_1:8080"),
	)

	It("falls back to host naming when the repository URL cannot be parsed", func() {
		Expect(repositoryNameFromURL("http://[::1")).To(Equal("[::1"))
	})

	It("derives the same repository name for paths that differ only by a trailing slash", func() {
		withSlash := repositoryNameFromURL("https://charts.example.com/stable/")
		withoutSlash := repositoryNameFromURL("https://charts.example.com/stable")
		Expect(withSlash).To(Equal(withoutSlash))
	})

	It("assigns distinct repository names for http and https on the same host", func() {
		httpURL := "http://charts.example.com"
		httpsURL := "https://charts.example.com"

		names := repositoryNamesForURLs([]string{httpURL, httpsURL})
		Expect(names[httpURL]).To(Equal("charts_example_com_http"))
		Expect(names[httpsURL]).To(Equal("charts_example_com_https"))
	})

	It("assigns distinct repository names for different paths on the same host", func() {
		stable := "https://charts.example.com/stable"
		experimental := "https://charts.example.com/experimental"

		stableName := repositoryNameFromURL(stable)
		experimentalName := repositoryNameFromURL(experimental)

		Expect(stableName).To(HavePrefix("charts_example_com_"))
		Expect(experimentalName).To(HavePrefix("charts_example_com_"))
		Expect(stableName).NotTo(Equal(experimentalName))
		Expect(repositoryNameFromURL(stable)).To(Equal(stableName))
	})

	It("registers multiple repositories on the same host without overwriting entries", func() {
		archive := packageTestChart(testDepChartName, testDepChartVersion)
		server := startSameHostMultiPathHTTPServer(map[string]sameHostChartEntry{
			"stable-chart": {
				pathPrefix: "stable",
				version:    testDepChartVersion,
				archive:    archive,
			},
			"other-chart": {
				pathPrefix: "experimental",
				version:    "2.0.0",
				archive:    packageTestChart("other-chart", "2.0.0"),
			},
		})
		defer server.Cleanup()

		chartDir := GinkgoT().TempDir()
		writeParentWithDistinctHTTPDependencies(chartDir,
			httpChartDependencyWithRepo{Name: "stable-chart", Version: testDepChartVersion, Repository: server.RepoURL("stable")},
			httpChartDependencyWithRepo{Name: "other-chart", Version: "2.0.0", Repository: server.RepoURL("experimental")},
		)

		Expect(registerHTTPChartRepositories(chartDir, server.Settings, getter.All(server.Settings))).To(Succeed())

		repoFile, err := repov1.LoadFile(server.Settings.RepositoryConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(repoFile.Repositories).To(HaveLen(2))
		Expect(repoFile.Get(repositoryNameFromURL(server.RepoURL("stable"))).URL).To(Equal(server.RepoURL("stable")))
		Expect(repoFile.Get(repositoryNameFromURL(server.RepoURL("experimental"))).URL).To(Equal(server.RepoURL("experimental")))
	})

	It("collects unique http(s) repository URLs from Chart.yaml", func() {
		chartDir := GinkgoT().TempDir()
		writeParentWithHTTPDependencies(chartDir, "https://charts.jetstack.io",
			httpChartDependency{Name: "cert-manager", Version: "v1.21.1"},
			httpChartDependency{Name: "trust-manager", Version: "v0.24.0"},
		)

		urls, err := chartDependencyRepositoryURLs(chartDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(urls).To(Equal([]string{"https://charts.jetstack.io"}))
	})

	It("skips file and oci repositories", func() {
		chartDir := GinkgoT().TempDir()
		writeParentWithFileDependency(chartDir)

		urls, err := chartDependencyRepositoryURLs(chartDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(urls).To(BeEmpty())
	})

	It("returns an error when Chart.yaml is missing", func() {
		_, err := chartDependencyRepositoryURLs(GinkgoT().TempDir())
		Expect(err).To(MatchError(ContainSubstring("read Chart.yaml")))
	})

	It("returns an error when Chart.yaml is invalid", func() {
		chartDir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("{"), 0o644)).To(Succeed())

		_, err := chartDependencyRepositoryURLs(chartDir)
		Expect(err).To(MatchError(ContainSubstring("parse Chart.yaml")))
	})

	It("keeps http(s) URLs that differ only by a trailing slash, matching 0.3 sort -u", func() {
		chartDir := GinkgoT().TempDir()
		chartYAML := `apiVersion: v2
name: parent
version: 0.1.0
dependencies:
  - name: first
    version: 1.0.0
    repository: https://charts.example.com/stable
  - name: second
    version: 2.0.0
    repository: https://charts.example.com/stable/
`
		Expect(os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(chartYAML), 0o644)).To(Succeed())

		urls, err := chartDependencyRepositoryURLs(chartDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(urls).To(Equal([]string{
			"https://charts.example.com/stable",
			"https://charts.example.com/stable/",
		}))
	})

	It("preserves credentials and query strings in repository URLs", func() {
		chartDir := GinkgoT().TempDir()
		chartYAML := `apiVersion: v2
name: parent
version: 0.1.0
dependencies:
  - name: authed
    version: 1.0.0
    repository: https://user:pass@charts.example.com/stable
  - name: tokenized
    version: 2.0.0
    repository: https://charts.example.com/stable?token=abc
`
		Expect(os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(chartYAML), 0o644)).To(Succeed())

		urls, err := chartDependencyRepositoryURLs(chartDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(urls).To(Equal([]string{
			"https://charts.example.com/stable?token=abc",
			"https://user:pass@charts.example.com/stable",
		}))
	})

	It("does not reuse repository entries when URLs differ only by credentials or query", func() {
		repoFile := repov1.NewFile()
		authedURL := "https://user:pass@charts.example.com/stable"
		tokenURL := "https://charts.example.com/stable?token=abc"
		plainURL := "https://charts.example.com/stable"

		authedName, authedChanged := ensureRepositoryEntry(repoFile, "charts_example_com", plainURL)
		Expect(authedChanged).To(BeTrue())
		Expect(repoFile.Get(authedName).URL).To(Equal(plainURL))

		tokenName, tokenChanged := ensureRepositoryEntry(repoFile, "charts_example_com", tokenURL)
		Expect(tokenChanged).To(BeTrue())
		Expect(tokenName).NotTo(Equal(authedName))
		Expect(repoFile.Get(tokenName).URL).To(Equal(tokenURL))

		authName, authChanged := ensureRepositoryEntry(repoFile, "charts_example_com", authedURL)
		Expect(authChanged).To(BeTrue())
		Expect(authName).NotTo(Equal(authedName))
		Expect(authName).NotTo(Equal(tokenName))
		Expect(repoFile.Get(authName).URL).To(Equal(authedURL))
		Expect(findRepositoryByURL(repoFile, plainURL).URL).To(Equal(plainURL))
	})

	It("skips empty repository entries and deduplicates http(s) URLs", func() {
		chartDir := GinkgoT().TempDir()
		chartYAML := `apiVersion: v2
name: parent
version: 0.1.0
dependencies:
  - name: empty
    version: 1.0.0
    repository: ""
  - name: oci
    version: 1.0.0
    repository: oci://registry.example/charts
  - name: first
    version: 1.0.0
    repository: https://charts.example.com
  - name: second
    version: 2.0.0
    repository: https://charts.example.com
`
		Expect(os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(chartYAML), 0o644)).To(Succeed())

		urls, err := chartDependencyRepositoryURLs(chartDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(urls).To(Equal([]string{"https://charts.example.com"}))
	})

	It("returns early when there are no http(s) repositories to register", func() {
		chartDir := GinkgoT().TempDir()
		writeParentWithFileDependency(chartDir)
		settings := testHelmSettings()

		Expect(registerHTTPChartRepositories(chartDir, settings, getter.All(settings))).To(Succeed())
		Expect(settings.RepositoryConfig).NotTo(BeAnExistingFile())
	})

	It("registers repositories and updates an existing repositories file", func() {
		archive := packageTestChart(testDepChartName, testDepChartVersion)
		repo := startHTTPChartRepo(archive, testDepChartName, testDepChartVersion, false)
		defer repo.Cleanup()

		chartDir := GinkgoT().TempDir()
		writeParentChart(chartDir, testDepChartName, testDepChartVersion, repo.URL)
		getters := getter.All(repo.Settings)

		Expect(registerHTTPChartRepositories(chartDir, repo.Settings, getters)).To(Succeed())
		Expect(repo.Settings.RepositoryConfig).To(BeAnExistingFile())

		Expect(registerHTTPChartRepositories(chartDir, repo.Settings, getters)).To(Succeed())
	})

	It("writes repository indexes under settings.RepositoryCache", func() {
		archive := packageTestChart(testDepChartName, testDepChartVersion)
		repo := startHTTPChartRepo(archive, testDepChartName, testDepChartVersion, false)
		defer repo.Cleanup()

		chartDir := GinkgoT().TempDir()
		writeParentChart(chartDir, testDepChartName, testDepChartVersion, repo.URL)

		Expect(registerHTTPChartRepositories(chartDir, repo.Settings, getter.All(repo.Settings))).To(Succeed())

		repoName := repositoryNameFromURL(repo.URL)
		Expect(repositoryIndexCachePath(repo.Settings.RepositoryCache, repoName)).To(BeAnExistingFile())
	})

	It("preserves TLS settings on pre-existing repository entries", func() {
		archive := packageTestChart(testDepChartName, testDepChartVersion)
		repo := startHTTPChartRepo(archive, testDepChartName, testDepChartVersion, false)
		defer repo.Cleanup()

		repoName := repositoryNameFromURL(repo.URL)
		existing := repov1.NewFile()
		existing.Add(&repov1.Entry{
			Name:     repoName,
			URL:      repo.URL,
			Username: "helm-user",
			Password: "helm-pass",
		})
		Expect(existing.WriteFile(repo.Settings.RepositoryConfig, 0o644)).To(Succeed())

		chartDir := GinkgoT().TempDir()
		writeParentChart(chartDir, testDepChartName, testDepChartVersion, repo.URL)

		Expect(registerHTTPChartRepositories(chartDir, repo.Settings, getter.All(repo.Settings))).To(Succeed())

		repoFile, err := repov1.LoadFile(repo.Settings.RepositoryConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(repoFile.Repositories).To(HaveLen(1))
		entry := repoFile.Get(repoName)
		Expect(entry.Username).To(Equal("helm-user"))
		Expect(entry.Password).To(Equal("helm-pass"))
	})

	It("does not clobber a pre-existing repository entry with a different URL", func() {
		archive := packageTestChart(testDepChartName, testDepChartVersion)
		repo := startHTTPChartRepo(archive, testDepChartName, testDepChartVersion, false)
		defer repo.Cleanup()

		repoName := repositoryNameFromURL(repo.URL)
		mirrorURL := "https://mirror.example.com/charts"
		existing := repov1.NewFile()
		existing.Add(&repov1.Entry{
			Name: repoName,
			URL:  mirrorURL,
		})
		Expect(existing.WriteFile(repo.Settings.RepositoryConfig, 0o644)).To(Succeed())

		chartDir := GinkgoT().TempDir()
		writeParentChart(chartDir, testDepChartName, testDepChartVersion, repo.URL)

		Expect(registerHTTPChartRepositories(chartDir, repo.Settings, getter.All(repo.Settings))).To(Succeed())

		repoFile, err := repov1.LoadFile(repo.Settings.RepositoryConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(repoFile.Get(repoName).URL).To(Equal(mirrorURL))
		Expect(findRepositoryByURL(repoFile, repo.URL)).NotTo(BeNil())
		Expect(findRepositoryByURL(repoFile, repo.URL).Name).NotTo(Equal(repoName))
	})

	It("registers http and https repositories on the same host without overwriting", func() {
		archive := packageTestChart(testDepChartName, testDepChartVersion)
		httpRepo := startHTTPChartRepo(archive, testDepChartName, testDepChartVersion, false)
		defer httpRepo.Cleanup()
		httpsRepo := startHTTPChartRepo(archive, testDepChartName, testDepChartVersion, true)
		defer httpsRepo.Cleanup()

		settings := testHelmSettings()
		chartDir := GinkgoT().TempDir()
		writeParentWithDistinctHTTPDependencies(chartDir,
			httpChartDependencyWithRepo{Name: testDepChartName, Version: testDepChartVersion, Repository: httpRepo.URL},
			httpChartDependencyWithRepo{Name: testDepChartName, Version: testDepChartVersion, Repository: httpsRepo.URL},
		)

		transport := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		getters := getter.All(settings, getter.WithTransport(transport))

		Expect(registerHTTPChartRepositories(chartDir, settings, getters)).To(Succeed())

		names := repositoryNamesForURLs([]string{httpRepo.URL, httpsRepo.URL})

		repoFile, err := repov1.LoadFile(settings.RepositoryConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(repoFile.Repositories).To(HaveLen(2))
		Expect(repoFile.Get(names[httpRepo.URL]).URL).To(Equal(httpRepo.URL))
		Expect(repoFile.Get(names[httpsRepo.URL]).URL).To(Equal(httpsRepo.URL))
		Expect(repositoryIndexCachePath(settings.RepositoryCache, names[httpRepo.URL])).To(BeAnExistingFile())
		Expect(repositoryIndexCachePath(settings.RepositoryCache, names[httpsRepo.URL])).To(BeAnExistingFile())
	})

	It("returns an error when the repository index cannot be downloaded", func() {
		chartDir := GinkgoT().TempDir()
		writeParentChart(chartDir, testDepChartName, testDepChartVersion, "http://127.0.0.1:1")
		settings := testHelmSettings()

		err := registerHTTPChartRepositories(chartDir, settings, getter.All(settings))
		Expect(err).To(MatchError(ContainSubstring("update chart repository")))
	})

	It("returns an error when the repositories file cannot be loaded", func() {
		archive := packageTestChart(testDepChartName, testDepChartVersion)
		repo := startHTTPChartRepo(archive, testDepChartName, testDepChartVersion, false)
		defer repo.Cleanup()

		chartDir := GinkgoT().TempDir()
		writeParentChart(chartDir, testDepChartName, testDepChartVersion, repo.URL)
		Expect(os.WriteFile(repo.Settings.RepositoryConfig, []byte("not yaml"), 0o644)).To(Succeed())

		err := registerHTTPChartRepositories(chartDir, repo.Settings, getter.All(repo.Settings))
		Expect(err).To(MatchError(ContainSubstring("load repositories file")))
	})

	It("returns an error when the repositories file cannot be written", func() {
		chartDir := GinkgoT().TempDir()
		writeParentChart(chartDir, testDepChartName, testDepChartVersion, "https://charts.example.com")
		settings := testHelmSettings()
		parent := filepath.Join(GinkgoT().TempDir(), "helm")
		Expect(os.Mkdir(parent, 0o555)).To(Succeed())
		settings.RepositoryConfig = filepath.Join(parent, "repositories.yaml")

		err := registerHTTPChartRepositories(chartDir, settings, getter.All(settings))
		Expect(err).To(MatchError(ContainSubstring("write repositories file")))
	})

	It("propagates Chart.yaml errors from repository registration", func() {
		settings := testHelmSettings()
		err := registerHTTPChartRepositories(GinkgoT().TempDir(), settings, getter.All(settings))
		Expect(err).To(MatchError(ContainSubstring("read Chart.yaml")))
	})

	It("returns an error when the repository config directory cannot be created", func() {
		chartDir := GinkgoT().TempDir()
		writeParentChart(chartDir, testDepChartName, testDepChartVersion, "https://charts.example.com")
		settings := testHelmSettings()
		settings.RepositoryConfig = "/root/helm-chart-oci-coverage/repositories.yaml"

		err := registerHTTPChartRepositories(chartDir, settings, getter.All(settings))
		Expect(err).To(MatchError(ContainSubstring("create repository config dir")))
	})

	It("propagates repository registration errors from buildDependenciesWith", func() {
		chartDir := GinkgoT().TempDir()
		writeParentChart(chartDir, testDepChartName, testDepChartVersion, "http://127.0.0.1:1")
		settings := testHelmSettings()

		err := buildDependenciesWith(chartDir, dependencyManagerConfig{
			settings: settings,
			getters:  getter.All(settings),
			out:      os.Stderr,
		})
		Expect(err).To(MatchError(ContainSubstring("update chart repository")))
	})
})
