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

// OptionDoc holds rich documentation for a single config option.
type OptionDoc struct {
	Type        string `json:"type,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
	Deprecated  string `json:"deprecated,omitempty"`
}

// PluginDoc holds rich documentation for a plugin.
type PluginDoc struct {
	Description string                `json:"description,omitempty"`
	Options     map[string]*OptionDoc `json:"options,omitempty"`
}

// RegistryData is the output JSON structure.
type RegistryData struct {
	Version          string                           `json:"version"`
	Plugins          map[string][]string              `json:"plugins"`
	Codecs           []string                         `json:"codecs"`
	CommonOptions    map[string][]string              `json:"commonOptions"`
	PluginOptions    map[string][]string              `json:"pluginOptions"`
	PluginDocs       map[string]*PluginDoc            `json:"pluginDocs,omitempty"`
	CodecDocs        map[string]*PluginDoc            `json:"codecDocs,omitempty"`
	CommonOptionDocs map[string]map[string]*OptionDoc `json:"commonOptionDocs,omitempty"`
}

type gemInfo struct {
	repo    string // e.g. "logstash-input-beats"
	typ     string // input, filter, output, codec
	name    string // e.g. "beats"
	version string // gem version
}

// richOption is an option with its rich metadata, used during extraction.
type richOption struct {
	Name string
	Doc  OptionDoc
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

	// Rich extraction regexes
	validateSymbolRegex = regexp.MustCompile(`:validate\s*=>\s*:(\w+)`)
	validateArrayRegex  = regexp.MustCompile(`:validate\s*=>\s*(?:%w[(\[]([^)\]]*)[)\]]|\[([^\]]*)\])`)
	requiredRegex       = regexp.MustCompile(`:required\s*=>\s*true`)
	defaultRegex        = regexp.MustCompile(`:default\s*=>\s*(.+?)(?:\s*,\s*:|$)`)
	listRegex           = regexp.MustCompile(`:list\s*=>\s*true`)
	obsoleteRegex       = regexp.MustCompile(`:obsolete\s*=>`)
	deprecatedRegex     = regexp.MustCompile(`:deprecated\s*=>\s*["'](.+?)["']`)
	classRegex          = regexp.MustCompile(`class\s+LogStash::`)

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

	// Build plugin lists and extract options (with rich data)
	plugins := map[string][]string{
		"input":  {},
		"filter": {},
		"output": {},
	}
	var codecs []string
	pluginOptions := map[string][]string{}
	pluginDocs := map[string]*PluginDoc{}
	codecDocs := map[string]*PluginDoc{}

	for key, g := range standalone {
		switch g.typ {
		case "codec":
			codecs = append(codecs, g.name)
		case "input", "filter", "output":
			plugins[g.typ] = append(plugins[g.typ], g.name)
		}

		// Phase 3: extract config options with rich data
		richOpts, pluginDesc, err := extractRichOptions(g)
		if err != nil {
			log.Printf("WARNING: failed to extract options for %s: %v", key, err)
			continue
		}

		// Build name-only list (backward compat)
		if len(richOpts) > 0 {
			names := make([]string, len(richOpts))
			for i, o := range richOpts {
				names[i] = o.Name
			}
			pluginOptions[key] = names
		}

		// Build plugin doc with option docs
		doc := &PluginDoc{Description: pluginDesc}
		if len(richOpts) > 0 {
			doc.Options = make(map[string]*OptionDoc, len(richOpts))
			for _, o := range richOpts {
				optDoc := o.Doc // copy
				doc.Options[o.Name] = &optDoc
			}
		}
		if doc.Description != "" || len(doc.Options) > 0 {
			if g.typ == "codec" {
				codecDocs[g.name] = doc
			} else {
				pluginDocs[key] = doc
			}
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

	// Common option docs (hardcoded descriptions for well-known base class options)
	commonOptionDocs := buildCommonOptionDocs()

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
		PluginOptions:    pluginOptions,
		PluginDocs:       pluginDocs,
		CodecDocs:        codecDocs,
		CommonOptionDocs: commonOptionDocs,
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
	docsWithDesc := 0
	for _, d := range pluginDocs {
		if d.Description != "" {
			docsWithDesc++
		}
	}
	for _, d := range codecDocs {
		if d.Description != "" {
			docsWithDesc++
		}
	}
	log.Printf("  plugins with descriptions: %d", docsWithDesc)
}

// buildCommonOptionDocs returns hardcoded docs for base class options.
func buildCommonOptionDocs() map[string]map[string]*OptionDoc {
	return map[string]map[string]*OptionDoc{
		"input": {
			"add_field":     {Type: "hash", Description: "Add a field to an event."},
			"codec":         {Type: "codec", Default: "plain", Description: "The codec used for input data."},
			"enable_metric": {Type: "boolean", Default: "true", Description: "Enable or disable metric logging."},
			"id":            {Type: "string", Description: "Add a unique ID to the plugin configuration."},
			"tags":          {Type: "array", Description: "Add any number of arbitrary tags to your event."},
			"type":          {Type: "string", Description: "Add a type field to all events handled by this input."},
		},
		"filter": {
			"add_field":      {Type: "hash", Description: "Add a field to an event if the filter is successful."},
			"add_tag":        {Type: "array", Description: "Add tags to an event if the filter is successful."},
			"enable_metric":  {Type: "boolean", Default: "true", Description: "Enable or disable metric logging."},
			"id":             {Type: "string", Description: "Add a unique ID to the plugin configuration."},
			"periodic_flush": {Type: "boolean", Default: "false", Description: "Call the filter flush method at regular interval."},
			"remove_field":   {Type: "array", Description: "Remove fields from an event if the filter is successful."},
			"remove_tag":     {Type: "array", Description: "Remove tags from an event if the filter is successful."},
		},
		"output": {
			"codec":         {Type: "codec", Default: "plain", Description: "The codec used for output data."},
			"enable_metric": {Type: "boolean", Default: "true", Description: "Enable or disable metric logging."},
			"id":            {Type: "string", Description: "Add a unique ID to the plugin configuration."},
			"workers":       {Type: "number", Default: "1", Description: "Number of workers to use for this output."},
		},
	}
}

// extractRichOptions fetches a plugin's Ruby source and extracts config options with rich metadata.
// Returns the options, plugin description, and any error.
func extractRichOptions(g gemInfo) ([]richOption, string, error) {
	typePlural := g.typ + "s"
	url := fmt.Sprintf("https://raw.githubusercontent.com/logstash-plugins/%s/v%s/lib/logstash/%s/%s.rb",
		g.repo, g.version, typePlural, g.name)

	body, err := fetchRaw(url)
	if err != nil {
		return nil, "", err
	}

	source := string(body)
	pluginDesc := extractPluginDescription(source)
	opts := parseRichConfigOptions(source)

	// Extract mixin options by following require statements (API-free)
	mixinOpts := extractMixinRichOptions(g, source)
	opts = append(opts, mixinOpts...)

	// Fallback: try tree API for additional mixins
	treeOpts := extractMixinRichOptionsFromTree(g)
	opts = append(opts, treeOpts...)

	// Deduplicate (keep first occurrence which has the primary source's data)
	seen := map[string]bool{}
	var unique []richOption
	for _, o := range opts {
		if !seen[o.Name] {
			seen[o.Name] = true
			unique = append(unique, o)
		}
	}
	return unique, pluginDesc, nil
}

// extractPluginDescription extracts the description comment block before the class declaration.
func extractPluginDescription(source string) string {
	lines := strings.Split(source, "\n")
	classLine := -1
	for i, line := range lines {
		if classRegex.MatchString(line) {
			classLine = i
			break
		}
	}
	if classLine < 0 {
		return ""
	}

	// Collect comment block immediately preceding the class line
	var commentLines []string
	for i := classLine - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			// Empty line between comments and class — still part of the block
			if i > 0 && strings.HasPrefix(strings.TrimSpace(lines[i-1]), "#") {
				commentLines = append(commentLines, "")
				continue
			}
			break
		}
		if !strings.HasPrefix(line, "#") {
			break
		}
		// Strip the leading # and optional space
		text := strings.TrimPrefix(line, "#")
		if len(text) > 0 && text[0] == ' ' {
			text = text[1:]
		}
		commentLines = append(commentLines, text)
	}

	if len(commentLines) == 0 {
		return ""
	}

	// Reverse (we collected bottom-up)
	for i, j := 0, len(commentLines)-1; i < j; i, j = i+1, j-1 {
		commentLines[i], commentLines[j] = commentLines[j], commentLines[i]
	}

	// Extract just the first paragraph as the short description
	// Stop at first blank line, AsciiDoc section header (====), or code block marker
	var desc []string
	for _, line := range commentLines {
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "====") || strings.HasPrefix(line, "[source") ||
			strings.HasPrefix(line, "---") || strings.HasPrefix(line, "NOTE:") ||
			strings.HasPrefix(line, ".") && len(line) > 1 && line[1] != ' ' {
			break
		}
		desc = append(desc, line)
	}

	result := strings.Join(desc, " ")
	// Clean up AsciiDoc link syntax: https://url[text] -> text
	asciidocLinkRegex := regexp.MustCompile(`https?://[^\[]+\[([^\]]+)\]`)
	result = asciidocLinkRegex.ReplaceAllString(result, "$1")
	return strings.TrimSpace(result)
}

// parseRichConfigOptions extracts config options with rich metadata from Ruby source.
func parseRichConfigOptions(source string) []richOption {
	var opts []richOption
	lines := strings.Split(source, "\n")
	inConfigParams := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		// Skip pure comment lines when not collecting descriptions
		if commentLine.MatchString(line) && !inConfigParams {
			continue
		}

		// Detect CONFIG_PARAMS hash block
		if strings.Contains(line, "CONFIG_PARAMS") && strings.Contains(line, "{") {
			inConfigParams = true
		}
		if inConfigParams {
			if m := configParamsRegex.FindStringSubmatch(line); m != nil {
				optName := m[1]
				// Extract rich data from the hash value
				doc := extractOptionDocFromLine(line)
				// Look for preceding comment in CONFIG_PARAMS block
				if desc := extractPrecedingComment(lines, i); desc != "" {
					doc.Description = desc
				}
				opts = append(opts, richOption{Name: optName, Doc: doc})
			}
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

		// Join continuation lines (config that spans multiple lines)
		fullLine := line
		for j := i + 1; j < len(lines) && !strings.HasSuffix(strings.TrimSpace(fullLine), ",") == false; j++ {
			nextTrimmed := strings.TrimSpace(lines[j])
			if nextTrimmed == "" || strings.HasPrefix(nextTrimmed, "#") || configRegex.MatchString(lines[j]) {
				break
			}
			// Check if it looks like a continuation (starts with : or whitespace-heavy)
			if strings.HasPrefix(nextTrimmed, ":") || strings.HasPrefix(nextTrimmed, "#") {
				fullLine += " " + nextTrimmed
				i = j
			} else {
				break
			}
		}

		// Skip obsolete options
		if obsoleteRegex.MatchString(fullLine) {
			continue
		}

		doc := extractOptionDocFromLine(fullLine)

		// Extract preceding comment block as description
		if desc := extractPrecedingComment(lines, findConfigLineIndex(lines, i)); desc != "" {
			doc.Description = desc
		}

		opts = append(opts, richOption{Name: name, Doc: doc})
	}
	return opts
}

// findConfigLineIndex walks backward from line i to find the actual config line
// (skipping any continuation lines we may have joined).
func findConfigLineIndex(lines []string, i int) int {
	// The config line is at index i or earlier if we skipped continuations
	for j := i; j >= 0; j-- {
		if configRegex.MatchString(lines[j]) {
			return j
		}
	}
	return i
}

// extractOptionDocFromLine extracts type, required, default from a config line.
func extractOptionDocFromLine(line string) OptionDoc {
	var doc OptionDoc

	// Type from :validate => :symbol
	if m := validateSymbolRegex.FindStringSubmatch(line); m != nil {
		doc.Type = m[1]
	} else if validateArrayRegex.MatchString(line) {
		// Enum type — list allowed values
		doc.Type = "string, one of"
		if m := validateArrayRegex.FindStringSubmatch(line); m != nil {
			vals := m[1]
			if vals == "" {
				vals = m[2]
			}
			// Clean up the values
			vals = strings.ReplaceAll(vals, "'", "")
			vals = strings.ReplaceAll(vals, "\"", "")
			fields := strings.Fields(vals)
			if len(fields) > 0 {
				// For %w format, fields are space-separated
				// For array format, they may be comma-separated
				if strings.Contains(vals, ",") {
					parts := strings.Split(vals, ",")
					var cleaned []string
					for _, p := range parts {
						p = strings.TrimSpace(p)
						if p != "" {
							cleaned = append(cleaned, p)
						}
					}
					doc.Type = "string, one of: " + strings.Join(cleaned, ", ")
				} else {
					doc.Type = "string, one of: " + strings.Join(fields, ", ")
				}
			}
		}
	}

	// :list => true modifier
	if listRegex.MatchString(line) && doc.Type != "" {
		doc.Type = "list of " + doc.Type
	}

	// Required
	if requiredRegex.MatchString(line) {
		doc.Required = true
	}

	// Default
	if m := defaultRegex.FindStringSubmatch(line); m != nil {
		def := strings.TrimSpace(m[1])
		// Clean up trailing commas and whitespace
		def = strings.TrimRight(def, ", ")
		// Only store simple/readable defaults
		if !strings.Contains(def, "::") && !strings.Contains(def, ".new") &&
			!strings.Contains(def, "lambda") && len(def) < 80 {
			// Clean up Ruby syntax
			def = strings.TrimPrefix(def, "\"")
			def = strings.TrimSuffix(def, "\"")
			def = strings.TrimPrefix(def, "'")
			def = strings.TrimSuffix(def, "'")
			doc.Default = def
		}
	}

	// Deprecated
	if m := deprecatedRegex.FindStringSubmatch(line); m != nil {
		doc.Deprecated = m[1]
	}

	return doc
}

// extractPrecedingComment collects the # comment block immediately before line i.
func extractPrecedingComment(lines []string, i int) string {
	var commentLines []string
	for j := i - 1; j >= 0; j-- {
		line := strings.TrimSpace(lines[j])
		if line == "" {
			// Skip single blank line between comment and config
			if len(commentLines) == 0 {
				continue
			}
			break
		}
		if !strings.HasPrefix(line, "#") {
			break
		}
		text := strings.TrimPrefix(line, "#")
		if len(text) > 0 && text[0] == ' ' {
			text = text[1:]
		}
		commentLines = append(commentLines, text)
	}

	if len(commentLines) == 0 {
		return ""
	}

	// Reverse
	for i, j := 0, len(commentLines)-1; i < j; i, j = i+1, j-1 {
		commentLines[i], commentLines[j] = commentLines[j], commentLines[i]
	}

	// Take first paragraph only (stop at blank, code block, section header)
	var desc []string
	for _, line := range commentLines {
		if line == "" {
			if len(desc) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(line, "====") || strings.HasPrefix(line, "[source") ||
			strings.HasPrefix(line, "---") {
			break
		}
		desc = append(desc, line)
	}

	result := strings.Join(desc, " ")
	// Clean up AsciiDoc link syntax
	asciidocLinkRegex := regexp.MustCompile(`https?://[^\[]+\[([^\]]+)\]`)
	result = asciidocLinkRegex.ReplaceAllString(result, "$1")
	return strings.TrimSpace(result)
}

// extractMixinRichOptions extracts rich options from mixin files.
func extractMixinRichOptions(g gemInfo, source string) []richOption {
	matches := requireMixinRegex.FindAllStringSubmatch(source, -1)
	if len(matches) == 0 {
		return nil
	}

	var allOpts []richOption
	fetched := map[string]bool{}
	for _, m := range matches {
		path := m[1]
		rbPath := "lib/logstash/plugin_mixins/" + path + ".rb"
		if fetched[rbPath] {
			continue
		}
		fetched[rbPath] = true

		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/logstash-plugins/%s/v%s/%s",
			g.repo, g.version, rbPath)
		rb, err := fetchRaw(rawURL)
		if err != nil {
			continue
		}

		mixinSource := string(rb)
		allOpts = append(allOpts, parseRichConfigOptions(mixinSource)...)

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
			allOpts = append(allOpts, parseRichConfigOptions(string(subRb))...)
		}
	}
	return allOpts
}

// extractMixinRichOptionsFromTree uses the tree API as a fallback.
func extractMixinRichOptionsFromTree(g gemInfo) []richOption {
	tree, err := getRepoTree(g.repo, g.version)
	if err != nil {
		return nil
	}

	prefix := "lib/logstash/plugin_mixins/"
	var allOpts []richOption
	for _, entry := range tree {
		if entry.Type != "blob" {
			continue
		}
		if !strings.HasPrefix(entry.Path, prefix) || !strings.HasSuffix(entry.Path, ".rb") {
			continue
		}

		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/logstash-plugins/%s/v%s/%s",
			g.repo, g.version, entry.Path)
		rb, err := fetchRaw(rawURL)
		if err != nil {
			continue
		}
		allOpts = append(allOpts, parseRichConfigOptions(string(rb))...)
	}
	return allOpts
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

var requireMixinRegex = regexp.MustCompile(`require\s+['"]logstash/plugin_mixins/([^'"]+)['"]`)

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
