package main

import (
	"encoding/json"
	"sort"
	"syscall/js"

	"github.com/breml/logstash-config/ast"
)

// contextInfoResult is the structured response for the sidebar.
type contextInfoResult struct {
	Kind        string       `json:"kind"`                  // "top-level", "section", "plugin", "codec", "none"
	SectionType string       `json:"sectionType,omitempty"` // "input", "filter", "output"
	PluginName  string       `json:"pluginName,omitempty"`
	PluginDoc   *pluginDoc   `json:"pluginDoc,omitempty"`
	OptionName  string       `json:"optionName,omitempty"`
	OptionDoc   *optionDoc   `json:"optionDoc,omitempty"`
	Plugins     []pluginInfo `json:"plugins,omitempty"`
	Options     []optionInfo `json:"options,omitempty"`
}

type pluginInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type optionInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description,omitempty"`
}

// extractWordAtPos returns the identifier word at/around the given cursor position.
func extractWordAtPos(source string, pos int) string {
	if pos > len(source) {
		pos = len(source)
	}

	// Find start: scan left from pos
	start := pos
	for start > 0 && isIdentChar(source[start-1]) {
		start--
	}

	// Find end: scan right from pos
	end := pos
	for end < len(source) && isIdentChar(source[end]) {
		end++
	}

	if start == end {
		return ""
	}
	return source[start:end]
}

// buildContextInfo creates the sidebar context info from a completion context.
func buildContextInfo(ctx completionContext, source string, pos int) contextInfoResult {
	switch ctx.Kind {
	case "section":
		// detectContext returns "section" when at top level (no nesting)
		// AND when inside a section block. We differentiate by checking SectionType.
		if ctx.SectionType == 0 {
			// Truly at top level — no section type set
			return contextInfoResult{Kind: "top-level"}
		}
		// Inside a section block — list available plugins
		sectionName := pluginTypeString(ctx.SectionType)
		return contextInfoResult{
			Kind:        "section",
			SectionType: sectionName,
			Plugins:     getPluginList(ctx.SectionType),
		}

	case "plugin":
		// Inside a section — list available plugins
		sectionName := pluginTypeString(ctx.SectionType)
		return contextInfoResult{
			Kind:        "section",
			SectionType: sectionName,
			Plugins:     getPluginList(ctx.SectionType),
		}

	case "option":
		// Inside a plugin block — list options
		sectionName := pluginTypeString(ctx.SectionType)
		word := extractWordAtPos(source, pos)
		result := contextInfoResult{
			Kind:        "plugin",
			SectionType: sectionName,
			PluginName:  ctx.PluginName,
			PluginDoc:   getPluginDocInfo(sectionName, ctx.PluginName),
			OptionName:  word,
			Options:     getOptionList(ctx.SectionType, ctx.PluginName),
		}
		if word != "" {
			result.OptionDoc = getOptionDocInfo(sectionName, ctx.PluginName, word)
		}
		return result

	case "codec":
		return contextInfoResult{
			Kind:    "codec",
			Plugins: getCodecList(),
		}
	}

	return contextInfoResult{Kind: "none"}
}

// getPluginList returns a sorted list of plugins for a section type.
func getPluginList(pt ast.PluginType) []pluginInfo {
	mu.RLock()
	plugins := knownPlugins[pt]
	mu.RUnlock()

	if plugins == nil {
		return nil
	}

	sectionName := pluginTypeString(pt)
	list := make([]pluginInfo, 0, len(plugins))
	for name := range plugins {
		info := pluginInfo{Name: name}
		if doc := getPluginDocInfo(sectionName, name); doc != nil {
			info.Description = doc.Description
		}
		list = append(list, info)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// getCodecList returns a sorted list of available codecs.
func getCodecList() []pluginInfo {
	mu.RLock()
	codecs := knownCodecs
	mu.RUnlock()

	if codecs == nil {
		return nil
	}

	list := make([]pluginInfo, 0, len(codecs))
	for name := range codecs {
		info := pluginInfo{Name: name}
		if doc := getPluginDocInfo("codec", name); doc != nil {
			info.Description = doc.Description
		}
		list = append(list, info)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// getOptionList returns a sorted list of options for a plugin.
func getOptionList(pt ast.PluginType, pluginName string) []optionInfo {
	known := getPluginOptions(pt, pluginName)
	if known == nil {
		return nil
	}

	sectionName := pluginTypeString(pt)
	list := make([]optionInfo, 0, len(known))
	for name := range known {
		info := optionInfo{Name: name}
		if doc := getOptionDocInfo(sectionName, pluginName, name); doc != nil {
			info.Type = doc.Type
			info.Required = doc.Required
			info.Default = doc.Default
			info.Description = doc.Description
		}
		list = append(list, info)
	}
	sort.Slice(list, func(i, j int) bool {
		// Required options first, then alphabetical
		if list[i].Required != list[j].Required {
			return list[i].Required
		}
		return list[i].Name < list[j].Name
	})
	return list
}

// getContextInfo is the WASM entry point for the context sidebar.
func getContextInfo(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		b, _ := json.Marshal(contextInfoResult{Kind: "none"})
		return string(b)
	}

	source := args[0].String()
	pos := args[1].Int()

	ctx := detectStructuralContext(source, pos)
	result := buildContextInfo(ctx, source, pos)

	b, _ := json.Marshal(result)
	return string(b)
}
