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

// pluginDoc holds rich documentation for a plugin (populated in Phase B).
type pluginDoc struct {
	Description string                `json:"description,omitempty"`
	Options     map[string]*optionDoc `json:"options,omitempty"`
}

// optionDoc holds rich documentation for a single option (populated in Phase B).
type optionDoc struct {
	Type        string `json:"type,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
}

// registryData mirrors the JSON structure produced by the scraper.
type registryData struct {
	Version          string                        `json:"version"`
	Plugins          map[string][]string           `json:"plugins"`
	Codecs           []string                      `json:"codecs"`
	CommonOptions    map[string][]string           `json:"commonOptions"`
	PluginOptions    map[string][]string           `json:"pluginOptions"`
	PluginDocs       map[string]*pluginDoc         `json:"pluginDocs,omitempty"`
	CodecDocs        map[string]*pluginDoc         `json:"codecDocs,omitempty"`
	CommonOptionDocs map[string]map[string]*optionDoc `json:"commonOptionDocs,omitempty"`
}

var (
	mu               sync.RWMutex
	currentVersion   string
	knownPlugins     map[ast.PluginType]map[string]bool
	knownCodecs      map[string]bool
	commonOptions    map[ast.PluginType]map[string]bool
	pluginOptions    map[string]map[string]bool // key: "input/elasticsearch"
	pluginDocs       map[string]*pluginDoc      // key: "input/elasticsearch"
	codecDocs        map[string]*pluginDoc      // key: "json"
	commonOptionDocs map[string]map[string]*optionDoc // key: "input" -> option name -> doc
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

	// Build doc maps (gracefully handle missing — Phase B data)
	newPluginDocs := make(map[string]*pluginDoc, len(rd.PluginDocs))
	for k, v := range rd.PluginDocs {
		newPluginDocs[k] = v
	}
	newCodecDocs := make(map[string]*pluginDoc, len(rd.CodecDocs))
	for k, v := range rd.CodecDocs {
		newCodecDocs[k] = v
	}
	newCommonOptionDocs := make(map[string]map[string]*optionDoc, len(rd.CommonOptionDocs))
	for k, v := range rd.CommonOptionDocs {
		newCommonOptionDocs[k] = v
	}

	mu.Lock()
	defer mu.Unlock()
	currentVersion = version
	knownPlugins = newPlugins
	knownCodecs = newCodecs
	commonOptions = newCommon
	pluginOptions = newOptions
	pluginDocs = newPluginDocs
	codecDocs = newCodecDocs
	commonOptionDocs = newCommonOptionDocs

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

// getPluginDocInfo returns the plugin doc for a given section type and plugin name.
func getPluginDocInfo(sectionType, pluginName string) *pluginDoc {
	mu.RLock()
	defer mu.RUnlock()

	if sectionType == "codec" {
		return codecDocs[pluginName]
	}
	key := sectionType + "/" + pluginName
	return pluginDocs[key]
}

// getOptionDocInfo returns the option doc for a given plugin option.
// Checks plugin-specific docs first, then common option docs.
func getOptionDocInfo(sectionType, pluginName, optionName string) *optionDoc {
	mu.RLock()
	defer mu.RUnlock()

	// Check plugin-specific option docs
	key := sectionType + "/" + pluginName
	if pd, ok := pluginDocs[key]; ok && pd != nil && pd.Options != nil {
		if od, ok := pd.Options[optionName]; ok {
			return od
		}
	}

	// Check common option docs
	if commonDocs, ok := commonOptionDocs[sectionType]; ok {
		if od, ok := commonDocs[optionName]; ok {
			return od
		}
	}

	return nil
}
