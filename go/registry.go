package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/breml/logstash-config/ast"
)

//go:embed registrydata/*.json
var registryFS embed.FS

// registryData mirrors the JSON structure produced by the scraper.
type registryData struct {
	Version       string              `json:"version"`
	Plugins       map[string][]string `json:"plugins"`
	Codecs        []string            `json:"codecs"`
	CommonOptions map[string][]string `json:"commonOptions"`
	PluginOptions map[string][]string `json:"pluginOptions"`
}

var (
	mu             sync.RWMutex
	currentVersion string
	knownPlugins   map[ast.PluginType]map[string]bool
	knownCodecs    map[string]bool
	commonOptions  map[ast.PluginType]map[string]bool
	pluginOptions  map[string]map[string]bool // key: "input/elasticsearch"
)

var pluginTypeMap = map[string]ast.PluginType{
	"input":  ast.Input,
	"filter": ast.Filter,
	"output": ast.Output,
}

// initRegistry loads the highest available version as the default.
func initRegistry() {
	versions := availableVersions()
	if len(versions) == 0 {
		// Fallback: empty registry
		knownPlugins = map[ast.PluginType]map[string]bool{}
		knownCodecs = map[string]bool{}
		commonOptions = map[ast.PluginType]map[string]bool{}
		pluginOptions = map[string]map[string]bool{}
		return
	}
	// Load the highest version (last after sort)
	v := versions[len(versions)-1]
	if err := loadVersion(v); err != nil {
		// Fallback: empty registry
		knownPlugins = map[ast.PluginType]map[string]bool{}
		knownCodecs = map[string]bool{}
		commonOptions = map[ast.PluginType]map[string]bool{}
		pluginOptions = map[string]map[string]bool{}
	}
}

// availableVersions returns sorted list of embedded registry versions.
func availableVersions() []string {
	entries, err := registryFS.ReadDir("registrydata")
	if err != nil {
		return nil
	}
	var versions []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		v := strings.TrimSuffix(e.Name(), ".json")
		versions = append(versions, v)
	}
	sort.Strings(versions)
	return versions
}

// loadVersion reads the JSON for a given version and rebuilds all internal maps.
func loadVersion(version string) error {
	filename := filepath.Join("registrydata", version+".json")
	data, err := registryFS.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("registry version %q not found", version)
	}

	var rd registryData
	if err := json.Unmarshal(data, &rd); err != nil {
		return fmt.Errorf("failed to parse registry %q: %w", version, err)
	}

	// Build knownPlugins
	newPlugins := map[ast.PluginType]map[string]bool{}
	for typeName, names := range rd.Plugins {
		pt, ok := pluginTypeMap[typeName]
		if !ok {
			continue
		}
		m := make(map[string]bool, len(names))
		for _, n := range names {
			m[n] = true
		}
		newPlugins[pt] = m
	}

	// Build knownCodecs
	newCodecs := make(map[string]bool, len(rd.Codecs))
	for _, c := range rd.Codecs {
		newCodecs[c] = true
	}

	// Build commonOptions
	newCommon := map[ast.PluginType]map[string]bool{}
	for typeName, opts := range rd.CommonOptions {
		pt, ok := pluginTypeMap[typeName]
		if !ok {
			continue
		}
		m := make(map[string]bool, len(opts))
		for _, o := range opts {
			m[o] = true
		}
		newCommon[pt] = m
	}

	// Build pluginOptions (type-qualified keys like "input/elasticsearch")
	newOptions := make(map[string]map[string]bool, len(rd.PluginOptions))
	for key, opts := range rd.PluginOptions {
		m := make(map[string]bool, len(opts))
		for _, o := range opts {
			m[o] = true
		}
		newOptions[key] = m
	}

	mu.Lock()
	defer mu.Unlock()
	currentVersion = version
	knownPlugins = newPlugins
	knownCodecs = newCodecs
	commonOptions = newCommon
	pluginOptions = newOptions

	return nil
}

func pluginTypeString(pt ast.PluginType) string {
	switch pt {
	case ast.Input:
		return "input"
	case ast.Filter:
		return "filter"
	case ast.Output:
		return "output"
	default:
		return ""
	}
}

// getPluginOptions returns the set of known options for a plugin.
// It merges common options for the section type with plugin-specific options.
// Returns nil if the plugin is unknown (no option checking should be done).
func getPluginOptions(pluginType ast.PluginType, pluginName string) map[string]bool {
	mu.RLock()
	defer mu.RUnlock()

	// Check if plugin is known at all
	if plugins, ok := knownPlugins[pluginType]; ok {
		if !plugins[pluginName] {
			return nil // unknown plugin, skip option checking
		}
	}

	common := commonOptions[pluginType]
	key := pluginTypeString(pluginType) + "/" + pluginName
	specific := pluginOptions[key]

	// If we have no specific schema, only check common options
	if specific == nil {
		return common
	}

	merged := make(map[string]bool, len(common)+len(specific))
	for k := range common {
		merged[k] = true
	}
	for k := range specific {
		merged[k] = true
	}
	return merged
}
