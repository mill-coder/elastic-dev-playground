package main

import (
	"encoding/json"
	"sort"
	"syscall/js"

	"github.com/breml/logstash-config/ast"
)

// completionContext describes where the cursor is in the Logstash config.
type completionContext struct {
	Kind        string         // "section", "plugin", "option", "codec", "none"
	SectionType ast.PluginType // valid when Kind is "plugin" or "option"
	PluginName  string         // valid when Kind is "option"
}

type completionOption struct {
	Label  string `json:"label"`
	Type   string `json:"type"`
	Detail string `json:"detail,omitempty"`
}

type completionResult struct {
	From    int                `json:"from"`
	Options []completionOption `json:"options"`
}

// frameKind describes what a brace-delimited block represents.
type frameKind int

const (
	frameSection     frameKind = iota // input { ... }
	framePlugin                       // grok { ... }
	frameConditional                  // if ... { ... }
	frameHash                         // match => { ... }
)

type frame struct {
	kind        frameKind
	sectionType ast.PluginType
	pluginName  string // only for framePlugin
}

// detectContext determines the completion context at the given cursor position.
func detectContext(source string, pos int) completionContext {
	if pos > len(source) {
		pos = len(source)
	}

	// Pass A: Check if we're in a value position (after =>).
	// Scan left from pos past the partial word, then past whitespace.
	p := pos - 1
	// Skip partial word
	for p >= 0 && isIdentChar(source[p]) {
		p--
	}
	// Skip whitespace
	for p >= 0 && (source[p] == ' ' || source[p] == '\t' || source[p] == '\n' || source[p] == '\r') {
		p--
	}
	// Check for =>
	if p >= 1 && source[p-1] == '=' && source[p] == '>' {
		// Extract the attribute name before =>
		ap := p - 2
		for ap >= 0 && (source[ap] == ' ' || source[ap] == '\t') {
			ap--
		}
		nameEnd := ap + 1
		for ap >= 0 && isIdentChar(source[ap]) {
			ap--
		}
		attrName := source[ap+1 : nameEnd]
		if attrName == "codec" {
			return completionContext{Kind: "codec"}
		}
		return completionContext{Kind: "none"}
	}

	// Pass B: Forward scan with brace-nesting stack.
	var stack []frame
	i := 0
	for i < pos {
		ch := source[i]

		// Skip comments — detect cursor inside comment
		if ch == '#' {
			for i < pos && source[i] != '\n' {
				i++
			}
			if i >= pos {
				return completionContext{Kind: "none"}
			}
			continue
		}

		// Skip double-quoted strings — detect cursor inside string
		if ch == '"' {
			i++
			for i < pos && source[i] != '"' {
				if source[i] == '\\' {
					i++ // skip escaped char
				}
				i++
			}
			if i >= pos {
				return completionContext{Kind: "none"}
			}
			i++ // skip closing quote
			continue
		}

		// Skip single-quoted strings — detect cursor inside string
		if ch == '\'' {
			i++
			for i < pos && source[i] != '\'' {
				if source[i] == '\\' {
					i++
				}
				i++
			}
			if i >= pos {
				return completionContext{Kind: "none"}
			}
			i++ // skip closing quote
			continue
		}

		// Opening brace not preceded by identifier or => (e.g. if-condition braces)
		if ch == '{' {
			sectionType := currentSectionType(stack)
			stack = append(stack, frame{kind: frameConditional, sectionType: sectionType})
			i++
			continue
		}

		// Closing brace
		if ch == '}' {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			i++
			continue
		}

		// Check for => { (hash value)
		if ch == '=' && i+1 < pos && source[i+1] == '>' {
			i += 2
			// Skip whitespace after =>
			for i < pos && (source[i] == ' ' || source[i] == '\t' || source[i] == '\n' || source[i] == '\r') {
				i++
			}
			if i < pos && source[i] == '{' {
				sectionType := currentSectionType(stack)
				stack = append(stack, frame{kind: frameHash, sectionType: sectionType})
				i++
			}
			continue
		}

		// Identifiers
		if isIdentStart(ch) {
			start := i
			for i < pos && isIdentChar(source[i]) {
				i++
			}
			ident := source[start:i]

			// Skip whitespace after identifier
			j := i
			for j < pos && (source[j] == ' ' || source[j] == '\t' || source[j] == '\n' || source[j] == '\r') {
				j++
			}

			// Check if followed by {
			if j < pos && source[j] == '{' {
				switch ident {
				case "input":
					stack = append(stack, frame{kind: frameSection, sectionType: ast.Input})
				case "filter":
					stack = append(stack, frame{kind: frameSection, sectionType: ast.Filter})
				case "output":
					stack = append(stack, frame{kind: frameSection, sectionType: ast.Output})
				case "if", "else":
					// Conditional: skip everything up to the opening brace
					sectionType := currentSectionType(stack)
					stack = append(stack, frame{kind: frameConditional, sectionType: sectionType})
				default:
					// Plugin name or other identifier followed by {
					sectionType := currentSectionType(stack)
					topKind := currentFrameKind(stack)
					if topKind == frameSection || topKind == frameConditional {
						stack = append(stack, frame{kind: framePlugin, sectionType: sectionType, pluginName: ident})
					} else {
						// Nested hash or unknown context
						stack = append(stack, frame{kind: frameHash, sectionType: sectionType})
					}
				}
				i = j + 1 // skip the {
				continue
			}

			continue
		}

		i++
	}

	// Determine context from stack
	if len(stack) == 0 {
		return completionContext{Kind: "section"}
	}

	top := stack[len(stack)-1]
	switch top.kind {
	case frameSection:
		return completionContext{Kind: "plugin", SectionType: top.sectionType}
	case framePlugin:
		return completionContext{Kind: "option", SectionType: top.sectionType, PluginName: top.pluginName}
	case frameConditional:
		return completionContext{Kind: "plugin", SectionType: top.sectionType}
	case frameHash:
		return completionContext{Kind: "none"}
	}

	return completionContext{Kind: "none"}
}

func currentSectionType(stack []frame) ast.PluginType {
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i].sectionType != 0 {
			return stack[i].sectionType
		}
	}
	return 0
}

func currentFrameKind(stack []frame) frameKind {
	if len(stack) == 0 {
		return -1
	}
	return stack[len(stack)-1].kind
}

func isIdentStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isIdentChar(ch byte) bool {
	return isIdentStart(ch) || (ch >= '0' && ch <= '9')
}

// buildCompletions generates completion options based on the detected context.
func buildCompletions(ctx completionContext) []completionOption {
	switch ctx.Kind {
	case "section":
		return []completionOption{
			{Label: "input", Type: "keyword", Detail: "section"},
			{Label: "filter", Type: "keyword", Detail: "section"},
			{Label: "output", Type: "keyword", Detail: "section"},
		}

	case "plugin":
		mu.RLock()
		plugins := knownPlugins[ctx.SectionType]
		mu.RUnlock()
		if plugins == nil {
			return nil
		}
		typeName := pluginTypeString(ctx.SectionType)
		opts := make([]completionOption, 0, len(plugins))
		for name := range plugins {
			opts = append(opts, completionOption{
				Label:  name,
				Type:   "type",
				Detail: typeName + " plugin",
			})
		}
		sort.Slice(opts, func(i, j int) bool { return opts[i].Label < opts[j].Label })
		return opts

	case "option":
		known := getPluginOptions(ctx.SectionType, ctx.PluginName)
		if known == nil {
			return nil
		}
		opts := make([]completionOption, 0, len(known))
		for name := range known {
			opts = append(opts, completionOption{
				Label:  name,
				Type:   "property",
				Detail: "option",
			})
		}
		sort.Slice(opts, func(i, j int) bool { return opts[i].Label < opts[j].Label })
		return opts

	case "codec":
		mu.RLock()
		codecs := knownCodecs
		mu.RUnlock()
		if codecs == nil {
			return nil
		}
		opts := make([]completionOption, 0, len(codecs))
		for name := range codecs {
			opts = append(opts, completionOption{
				Label:  name,
				Type:   "enum",
				Detail: "codec",
			})
		}
		sort.Slice(opts, func(i, j int) bool { return opts[i].Label < opts[j].Label })
		return opts
	}

	return nil
}

// detectStructuralContext determines the structural nesting context at pos,
// ignoring value positions, strings, and comments. Used by the sidebar
// to always show relevant plugin/option info regardless of cursor detail.
func detectStructuralContext(source string, pos int) completionContext {
	if pos > len(source) {
		pos = len(source)
	}

	// Only do the forward brace-nesting scan (Pass B from detectContext).
	var stack []frame
	i := 0
	for i < pos {
		ch := source[i]

		// Skip comments
		if ch == '#' {
			for i < len(source) && source[i] != '\n' {
				i++
			}
			continue
		}

		// Skip double-quoted strings
		if ch == '"' {
			i++
			for i < len(source) && source[i] != '"' {
				if source[i] == '\\' {
					i++
				}
				i++
			}
			if i < len(source) {
				i++ // skip closing quote
			}
			continue
		}

		// Skip single-quoted strings
		if ch == '\'' {
			i++
			for i < len(source) && source[i] != '\'' {
				if source[i] == '\\' {
					i++
				}
				i++
			}
			if i < len(source) {
				i++
			}
			continue
		}

		if ch == '{' {
			sectionType := currentSectionType(stack)
			stack = append(stack, frame{kind: frameConditional, sectionType: sectionType})
			i++
			continue
		}

		if ch == '}' {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			i++
			continue
		}

		if ch == '=' && i+1 < len(source) && source[i+1] == '>' {
			i += 2
			for i < len(source) && (source[i] == ' ' || source[i] == '\t' || source[i] == '\n' || source[i] == '\r') {
				i++
			}
			if i < len(source) && source[i] == '{' {
				sectionType := currentSectionType(stack)
				stack = append(stack, frame{kind: frameHash, sectionType: sectionType})
				i++
			}
			continue
		}

		if isIdentStart(ch) {
			start := i
			for i < len(source) && isIdentChar(source[i]) {
				i++
			}
			ident := source[start:i]

			j := i
			for j < len(source) && (source[j] == ' ' || source[j] == '\t' || source[j] == '\n' || source[j] == '\r') {
				j++
			}

			if j < len(source) && source[j] == '{' {
				switch ident {
				case "input":
					stack = append(stack, frame{kind: frameSection, sectionType: ast.Input})
				case "filter":
					stack = append(stack, frame{kind: frameSection, sectionType: ast.Filter})
				case "output":
					stack = append(stack, frame{kind: frameSection, sectionType: ast.Output})
				case "if", "else":
					sectionType := currentSectionType(stack)
					stack = append(stack, frame{kind: frameConditional, sectionType: sectionType})
				default:
					sectionType := currentSectionType(stack)
					topKind := currentFrameKind(stack)
					if topKind == frameSection || topKind == frameConditional {
						stack = append(stack, frame{kind: framePlugin, sectionType: sectionType, pluginName: ident})
					} else {
						stack = append(stack, frame{kind: frameHash, sectionType: sectionType})
					}
				}
				i = j + 1
				continue
			}
			continue
		}

		i++
	}

	if len(stack) == 0 {
		return completionContext{Kind: "section"}
	}

	top := stack[len(stack)-1]
	switch top.kind {
	case frameSection:
		return completionContext{Kind: "plugin", SectionType: top.sectionType}
	case framePlugin:
		return completionContext{Kind: "option", SectionType: top.sectionType, PluginName: top.pluginName}
	case frameConditional:
		return completionContext{Kind: "plugin", SectionType: top.sectionType}
	case frameHash:
		// For hash values, walk up the stack to find the enclosing plugin
		for si := len(stack) - 2; si >= 0; si-- {
			if stack[si].kind == framePlugin {
				return completionContext{Kind: "option", SectionType: stack[si].sectionType, PluginName: stack[si].pluginName}
			}
		}
		return completionContext{Kind: "none"}
	}

	return completionContext{Kind: "none"}
}

// getCompletions is the WASM entry point for code completion.
func getCompletions(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		b, _ := json.Marshal(completionResult{From: 0, Options: []completionOption{}})
		return string(b)
	}

	source := args[0].String()
	cursorPos := args[1].Int()

	// Compute 'from' by scanning left from cursorPos past identifier chars
	from := cursorPos
	for from > 0 && isIdentChar(source[from-1]) {
		from--
	}

	ctx := detectContext(source, cursorPos)
	options := buildCompletions(ctx)
	if options == nil {
		options = []completionOption{}
	}

	result := completionResult{
		From:    from,
		Options: options,
	}
	b, _ := json.Marshal(result)
	return string(b)
}
