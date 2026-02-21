// scrape-registry fetches Logstash plugin metadata from GitHub and generates
// a versioned JSON registry file for the WASM parser.
//
// Usage:
//
//	go run ./tools/scrape-registry -version 8.19 -out go/registrydata/8.19.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// RegistryData is the output JSON structure.
type RegistryData struct {
	Version       string              `json:"version"`
	Plugins       map[string][]string `json:"plugins"`
	Codecs        []string            `json:"codecs"`
	CommonOptions map[string][]string `json:"commonOptions"`
	PluginOptions map[string][]string `json:"pluginOptions"`
}

type gemInfo struct {
	repo    string // e.g. "logstash-input-beats"
	typ     string // input, filter, output, codec
	name    string // e.g. "beats"
	version string // gem version
}

// treeEntry represents one item from the GitHub git/trees API.
type treeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" or "tree"
}

var (
	gemRegex    = regexp.MustCompile(`^\s{4}(logstash-(input|filter|output|codec|integration)-([\w-]+))\s+\(([\d.]+)(?:-java)?\)`)
	configRegex = regexp.MustCompile(`^\s*config\s+:(\w+)`)
	// Matches CONFIG_PARAMS hash entries like `:hosts => { ... }`
	configParamsRegex = regexp.MustCompile(`^\s+:(\w+)\s*=>`)
	commentLine       = regexp.MustCompile(`^\s*#`)

	token       string
	apiDelay    = 100 * time.Millisecond
	lastAPICall time.Time

	// Cache repo trees to avoid duplicate API calls for the same repo+version.
	treeCache = map[string][]treeEntry{}
)

func main() {
	version := flag.String("version", "", "Logstash version to scrape (e.g. 8.19)")
	out := flag.String("out", "", "Output JSON file path")
	tokenFlag := flag.String("token", "", "GitHub token (or use GITHUB_TOKEN env)")
	flag.Parse()

	if *version == "" || *out == "" {
		flag.Usage()
		os.Exit(1)
	}

	token = *tokenFlag
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token != "" {
		apiDelay = 20 * time.Millisecond // faster with auth
	}

	log.Printf("Scraping Logstash %s plugin registry...", *version)

	// Phase 1: fetch lockfile and parse gems
	gems, err := fetchGems(*version)
	if err != nil {
		log.Fatalf("Failed to fetch lockfile: %v", err)
	}
	log.Printf("Found %d gems in lockfile", len(gems))

	// Separate integration gems from standalone
	var integrations []gemInfo
	standalone := make(map[string]gemInfo) // key: "type/name"
	for _, g := range gems {
		if g.typ == "integration" {
			integrations = append(integrations, g)
		} else {
			key := g.typ + "/" + g.name
			standalone[key] = g
		}
	}
	log.Printf("Standalone plugins: %d, Integration gems: %d", len(standalone), len(integrations))

	// Phase 2: resolve integration plugins using tree API (1 API call per integration)
	for _, ig := range integrations {
		subs, err := resolveIntegration(ig)
		if err != nil {
			log.Printf("WARNING: failed to resolve integration %s: %v", ig.repo, err)
			continue
		}
		for _, sub := range subs {
			key := sub.typ + "/" + sub.name
			if _, exists := standalone[key]; !exists {
				standalone[key] = sub
			}
		}
	}
	log.Printf("Total plugins after integration resolution: %d", len(standalone))

	// Build plugin lists and extract options
	plugins := map[string][]string{
		"input":  {},
		"filter": {},
		"output": {},
	}
	var codecs []string
	pluginOptions := map[string][]string{}

	for key, g := range standalone {
		switch g.typ {
		case "codec":
			codecs = append(codecs, g.name)
		case "input", "filter", "output":
			plugins[g.typ] = append(plugins[g.typ], g.name)
		}

		// Phase 3: extract config options
		opts, err := extractOptions(g)
		if err != nil {
			log.Printf("WARNING: failed to extract options for %s: %v", key, err)
			continue
		}
		if len(opts) > 0 {
			pluginOptions[key] = opts
		}
	}

	// Sort everything
	for typ := range plugins {
		sort.Strings(plugins[typ])
	}
	sort.Strings(codecs)
	for key := range pluginOptions {
		sort.Strings(pluginOptions[key])
	}

	// Phase 4: write JSON
	data := RegistryData{
		Version: *version,
		Plugins: plugins,
		Codecs:  codecs,
		CommonOptions: map[string][]string{
			"input":  {"add_field", "codec", "enable_metric", "id", "tags", "type"},
			"filter": {"add_field", "add_tag", "enable_metric", "id", "periodic_flush", "remove_field", "remove_tag"},
			"output": {"codec", "enable_metric", "id", "workers"},
		},
		PluginOptions: pluginOptions,
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal JSON: %v", err)
	}
	b = append(b, '\n')

	if err := os.WriteFile(*out, b, 0o644); err != nil {
		log.Fatalf("Failed to write %s: %v", *out, err)
	}

	log.Printf("Wrote %s (%d bytes)", *out, len(b))
	log.Printf("  inputs: %d, filters: %d, outputs: %d, codecs: %d",
		len(plugins["input"]), len(plugins["filter"]), len(plugins["output"]), len(codecs))
	log.Printf("  plugin option schemas: %d", len(pluginOptions))
}

// fetchGems fetches the Gemfile lockfile and parses gem entries.
func fetchGems(version string) ([]gemInfo, error) {
	lockfileNames := []string{
		"Gemfile.jruby-3.1.lock.release",
		"Gemfile.jruby-3.4.lock.release",
		"Gemfile.jruby-2.6.lock.release",
		"Gemfile.jruby-2.5.lock.release",
	}

	// Try version as-is and with .0 suffix (tags may be v8.19 or v8.19.0)
	versions := []string{version}
	if strings.Count(version, ".") < 2 {
		versions = append(versions, version+".0")
	}

	var body string
	var lastErr error
	for _, ver := range versions {
		for _, name := range lockfileNames {
			url := fmt.Sprintf("https://raw.githubusercontent.com/elastic/logstash/v%s/%s", ver, name)
			b, err := fetchRaw(url)
			if err != nil {
				lastErr = err
				continue
			}
			body = string(b)
			log.Printf("Using lockfile: v%s/%s", ver, name)
			break
		}
		if body != "" {
			break
		}
	}
	if body == "" {
		return nil, fmt.Errorf("could not fetch any lockfile variant: %v", lastErr)
	}

	var gems []gemInfo
	for _, line := range strings.Split(body, "\n") {
		m := gemRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		gems = append(gems, gemInfo{
			repo:    m[1],
			typ:     m[2],
			name:    m[3],
			version: m[4],
		})
	}
	return gems, nil
}

// getRepoTree fetches the full recursive file tree for a repo at a given tag.
// Uses a single GitHub API call and caches the result.
func getRepoTree(repo, version string) ([]treeEntry, error) {
	cacheKey := repo + "@" + version
	if cached, ok := treeCache[cacheKey]; ok {
		return cached, nil
	}

	url := fmt.Sprintf("https://api.github.com/repos/logstash-plugins/%s/git/trees/v%s?recursive=1", repo, version)
	body, err := fetchAPI(url)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Tree []treeEntry `json:"tree"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	treeCache[cacheKey] = resp.Tree
	return resp.Tree, nil
}

// Matches "integration_plugins" => "plugin1,plugin2" format
var integrationQuotedRegex = regexp.MustCompile(`"integration_plugins"\s*=>\s*"([^"]+)"`)

// Matches "integration_plugins" => %w(...).join(",") format (multiline)
var integrationPercentWRegex = regexp.MustCompile(`(?s)"integration_plugins"\s*=>\s*%w\((.*?)\)`)

var pluginGemRegex = regexp.MustCompile(`^logstash-(input|filter|output|codec)-(.+)$`)

// resolveIntegration finds sub-plugins within an integration gem.
// First tries the gemspec (API-free via raw.githubusercontent.com),
// then falls back to the tree API.
func resolveIntegration(ig gemInfo) ([]gemInfo, error) {
	subs, err := resolveIntegrationFromGemspec(ig)
	if err == nil && len(subs) > 0 {
		return subs, nil
	}

	return resolveIntegrationFromTree(ig)
}

// resolveIntegrationFromGemspec parses the gemspec's integration_plugins metadata.
// Handles both quoted string and %w() array formats.
func resolveIntegrationFromGemspec(ig gemInfo) ([]gemInfo, error) {
	url := fmt.Sprintf("https://raw.githubusercontent.com/logstash-plugins/%s/v%s/%s.gemspec",
		ig.repo, ig.version, ig.repo)
	body, err := fetchRaw(url)
	if err != nil {
		return nil, err
	}

	content := string(body)
	var pluginNames []string

	// Try quoted string format: "plugin1,plugin2"
	if m := integrationQuotedRegex.FindStringSubmatch(content); m != nil {
		for _, part := range strings.Split(m[1], ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				pluginNames = append(pluginNames, part)
			}
		}
	}

	// Try %w() format: %w(plugin1 plugin2 ...)
	if len(pluginNames) == 0 {
		if m := integrationPercentWRegex.FindStringSubmatch(content); m != nil {
			for _, part := range strings.Fields(m[1]) {
				part = strings.TrimSpace(part)
				if part != "" {
					pluginNames = append(pluginNames, part)
				}
			}
		}
	}

	if len(pluginNames) == 0 {
		return nil, fmt.Errorf("no integration_plugins found in gemspec")
	}

	var subs []gemInfo
	for _, name := range pluginNames {
		pm := pluginGemRegex.FindStringSubmatch(name)
		if pm == nil {
			continue
		}
		subs = append(subs, gemInfo{
			repo:    ig.repo,
			typ:     pm[1],
			name:    pm[2],
			version: ig.version,
		})
	}
	return subs, nil
}

// resolveIntegrationFromTree uses the tree API to find sub-plugins.
func resolveIntegrationFromTree(ig gemInfo) ([]gemInfo, error) {
	tree, err := getRepoTree(ig.repo, ig.version)
	if err != nil {
		return nil, err
	}

	var subs []gemInfo
	for _, typ := range []string{"inputs", "filters", "outputs", "codecs"} {
		prefix := "lib/logstash/" + typ + "/"
		singularType := strings.TrimSuffix(typ, "s")

		for _, entry := range tree {
			if entry.Type != "blob" {
				continue
			}
			if !strings.HasPrefix(entry.Path, prefix) {
				continue
			}
			rest := strings.TrimPrefix(entry.Path, prefix)
			if strings.Contains(rest, "/") {
				continue
			}
			if !strings.HasSuffix(rest, ".rb") {
				continue
			}
			stem := strings.TrimSuffix(rest, ".rb")
			if shouldSkipFile(stem) {
				continue
			}

			subs = append(subs, gemInfo{
				repo:    ig.repo,
				typ:     singularType,
				name:    stem,
				version: ig.version,
			})
		}
	}
	return subs, nil
}

func shouldSkipFile(stem string) bool {
	skip := []string{"patch", "util", "helper", "base", "mixin", "support", "common"}
	lower := strings.ToLower(stem)
	for _, s := range skip {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

var requireMixinRegex = regexp.MustCompile(`require\s+['"]logstash/plugin_mixins/([^'"]+)['"]`)

// extractOptions fetches a plugin's Ruby source and extracts config option names.
// Also extracts options from mixin files discovered via require statements.
func extractOptions(g gemInfo) ([]string, error) {
	typePlural := g.typ + "s"
	url := fmt.Sprintf("https://raw.githubusercontent.com/logstash-plugins/%s/v%s/lib/logstash/%s/%s.rb",
		g.repo, g.version, typePlural, g.name)

	body, err := fetchRaw(url)
	if err != nil {
		return nil, err
	}

	source := string(body)
	opts := parseConfigOptions(source)

	// Extract mixin options by following require statements (API-free)
	mixinOpts := extractMixinOptionsFromRequires(g, source)
	opts = append(opts, mixinOpts...)

	// Fallback: try tree API for additional mixins not found via requires
	treeOpts := extractMixinOptionsFromTree(g)
	opts = append(opts, treeOpts...)

	// Deduplicate
	seen := map[string]bool{}
	var unique []string
	for _, o := range opts {
		if !seen[o] {
			seen[o] = true
			unique = append(unique, o)
		}
	}
	return unique, nil
}

// extractMixinOptionsFromRequires parses require statements from the plugin source
// to find mixin files, then fetches them via raw URLs (no API needed).
func extractMixinOptionsFromRequires(g gemInfo, source string) []string {
	matches := requireMixinRegex.FindAllStringSubmatch(source, -1)
	if len(matches) == 0 {
		return nil
	}

	var allOpts []string
	fetched := map[string]bool{}
	for _, m := range matches {
		path := m[1] // e.g. "elasticsearch/api_configs" or "ecs_compatibility_support"

		// Only fetch mixins from the same repo (in-repo mixins)
		rbPath := "lib/logstash/plugin_mixins/" + path + ".rb"
		if fetched[rbPath] {
			continue
		}
		fetched[rbPath] = true

		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/logstash-plugins/%s/v%s/%s",
			g.repo, g.version, rbPath)
		rb, err := fetchRaw(rawURL)
		if err != nil {
			continue // mixin might be in a different gem
		}

		mixinSource := string(rb)
		allOpts = append(allOpts, parseConfigOptions(mixinSource)...)

		// Recursively follow require statements in mixin files
		for _, sub := range requireMixinRegex.FindAllStringSubmatch(mixinSource, -1) {
			subPath := "lib/logstash/plugin_mixins/" + sub[1] + ".rb"
			if fetched[subPath] {
				continue
			}
			fetched[subPath] = true

			subURL := fmt.Sprintf("https://raw.githubusercontent.com/logstash-plugins/%s/v%s/%s",
				g.repo, g.version, subPath)
			subRb, err := fetchRaw(subURL)
			if err != nil {
				continue
			}
			allOpts = append(allOpts, parseConfigOptions(string(subRb))...)
		}
	}
	return allOpts
}

// extractMixinOptionsFromTree uses the tree API as a fallback to find additional
// mixin files that weren't discovered via require statements.
func extractMixinOptionsFromTree(g gemInfo) []string {
	tree, err := getRepoTree(g.repo, g.version)
	if err != nil {
		return nil
	}

	prefix := "lib/logstash/plugin_mixins/"
	var allOpts []string
	for _, entry := range tree {
		if entry.Type != "blob" {
			continue
		}
		if !strings.HasPrefix(entry.Path, prefix) {
			continue
		}
		if !strings.HasSuffix(entry.Path, ".rb") {
			continue
		}

		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/logstash-plugins/%s/v%s/%s",
			g.repo, g.version, entry.Path)
		rb, err := fetchRaw(rawURL)
		if err != nil {
			continue
		}
		allOpts = append(allOpts, parseConfigOptions(string(rb))...)
	}
	return allOpts
}

// parseConfigOptions extracts config option names from Ruby source.
// Matches both `config :name` declarations and `CONFIG_PARAMS = { :name => ... }` hashes.
func parseConfigOptions(source string) []string {
	var opts []string
	inConfigParams := false
	for _, line := range strings.Split(source, "\n") {
		if commentLine.MatchString(line) {
			continue
		}

		// Detect CONFIG_PARAMS hash block
		if strings.Contains(line, "CONFIG_PARAMS") && strings.Contains(line, "{") {
			inConfigParams = true
		}
		if inConfigParams {
			// End of CONFIG_PARAMS hash
			if m := configParamsRegex.FindStringSubmatch(line); m != nil {
				opts = append(opts, m[1])
			}
			// Check for closing - but CONFIG_PARAMS blocks use }.freeze or similar
			trimmed := strings.TrimSpace(line)
			if trimmed == "}" || strings.HasPrefix(trimmed, "}.") {
				inConfigParams = false
			}
			continue
		}

		// Standard `config :name` format
		m := configRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		if name == "config_name" {
			continue
		}
		opts = append(opts, name)
	}
	return opts
}

// fetchRaw fetches from raw.githubusercontent.com (no API rate limit).
func fetchRaw(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// fetchAPI fetches from the GitHub API with rate limiting.
func fetchAPI(url string) ([]byte, error) {
	since := time.Since(lastAPICall)
	if since < apiDelay {
		time.Sleep(apiDelay - since)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if token != "" {
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	lastAPICall = time.Now()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "rate limit") {
			return nil, fmt.Errorf("GitHub API rate limit exceeded. Set GITHUB_TOKEN env var or use -token flag")
		}
		return nil, fmt.Errorf("HTTP 403 for %s: %s", url, string(body))
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
