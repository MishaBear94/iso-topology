// Package yamledit performs comment-preserving text surgery on the topology
// YAML/d2 source. Studio edits must never reflow the user's document, so this
// operates on the raw text (line/regex surgery) rather than a yaml.Node round-
// trip (which reformats indentation, flow spacing and comment positions). Each
// exported op takes the source string and returns the edited string, touching
// only the lines it must.
package yamledit

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// seqBlockEnd locates the end of the YAML sequence under a `key:` at keyIndent,
// and the column its items sit at. YAML permits a sequence item at the SAME
// column as its key (`- x` directly under the key) as well as indented deeper;
// BOTH are inside the block. Only a shallower line, or a non-item line at
// keyIndent (a sibling key), ends it. Returns (itemIndent, end): end is the
// first line past the block; itemIndent is the existing items' column, or
// keyIndent+2 when the block is still empty. Shared by AddPart / AddConnector so
// the same-column case can't regress in just one of them.
func seqBlockEnd(lines []string, keyLine, keyIndent int) (itemIndent, end int) {
	itemIndent = keyIndent + 2
	foundItem := false
	end = len(lines)
	for i := keyLine + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		ind := indentOf(lines[i])
		isSeqItem := strings.HasPrefix(strings.TrimLeft(lines[i], " "), "-")
		if ind < keyIndent || (ind == keyIndent && !isSeqItem) {
			end = i
			break
		}
		if isSeqItem && !foundItem {
			itemIndent, foundItem = ind, true
		}
		end = i + 1
	}
	return itemIndent, end
}

// FreezeLayoutText strips the layout that drives root-part positions so
// a frozen scene renders purely from explicit offsets: the scene-root
// `layout:` (inline or block, ANY mode) and every root part's `place:`
// (inline or block). Nested-group layouts and child place are left
// intact — only the root parts the user drags are detached from the
// engine. Identifies "root" by indentation: the shallowest `- ` item
// depth under the parts: list.
func FreezeLayoutText(src string) string {
	lines := strings.Split(src, "\n")
	keyBlock := func(re *regexp.Regexp) {
		inlineRe := regexp.MustCompile(re.String() + `\s*\{`)
		out := make([]string, 0, len(lines))
		for i := 0; i < len(lines); i++ {
			if re.MatchString(lines[i]) {
				if inlineRe.MatchString(lines[i]) || strings.Contains(lines[i], "{") {
					continue // inline form: drop the one line
				}
				// block form: drop this line + its deeper-indented body
				ind := indentOf(lines[i])
				j := i + 1
				for j < len(lines) && (strings.TrimSpace(lines[j]) == "" || indentOf(lines[j]) > ind) {
					j++
				}
				i = j - 1
				continue
			}
			out = append(out, lines[i])
		}
		lines = out
	}
	// The scene's own keys (shape/layout/parts) share one indent: the
	// SHALLOWEST `parts:`. Strip only the layout: at that indent (the
	// scene root) — nested group layouts sit deeper and must survive.
	rootIndent := -1
	partsRe := regexp.MustCompile(`^( *)parts:\s*$`)
	for _, l := range lines {
		if m := partsRe.FindStringSubmatch(l); m != nil {
			if rootIndent < 0 || len(m[1]) < rootIndent {
				rootIndent = len(m[1])
			}
		}
	}
	if rootIndent >= 0 {
		keyBlock(regexp.MustCompile(`^ {` + itoa(rootIndent) + `}layout:`))
		// Root-part `place:` sits one list level under parts: — at the
		// root part's key indent (rootIndent+4 in block form). Strip place
		// only at root-part depth; nested children's place is deeper.
		keyBlock(regexp.MustCompile(`^ {` + itoa(rootIndent+4) + `}place:`))
	}
	// Flow-form root parts carry place inline on the `- { … }` line;
	// strip an inline place: { … } from those (root depth = rootIndent+2).
	if rootIndent >= 0 {
		inlinePlace := regexp.MustCompile(`,?\s*place:\s*\{[^}]*\}`)
		dash := regexp.MustCompile(`^ {` + itoa(rootIndent+2) + `}- \{`)
		for i, l := range lines {
			if dash.MatchString(l) {
				lines[i] = inlinePlace.ReplaceAllString(l, "")
			}
		}
	}
	return strings.Join(lines, "\n")
}

func itoa(n int) string { return strconv.Itoa(n) }

// UpsertInlineKey replaces or inserts an inline `key: { wx: X, wy: Y }`
// line inside the YAML block that begins at startLine (an `id:` line or
// a connector `- ` item). Block ends at the next line whose indent is
// ≤ the start line's indent. Preserves all other formatting/comments —
// the Studio drag must not reflow the user's YAML. Returns (newSrc, ok).
//
// wz is written only when non-zero (so 2D scenes stay byte-identical and a
// node already carrying a wz keeps it through a 2D drag).
func offsetVal(key string, wx, wy, wz float64) string {
	// Preserve precision: %.0f rounded resolved coordinates to integers, so
	// freezing a layout-solved scene (the first drag, or even a zero-delta move)
	// shifted every node up to ~0.5px off where it was rendered. FormatFloat with
	// precision -1 emits the shortest exact form — integers stay integers
	// (`20`, not `20.0`), fractions survive.
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	if wz != 0 {
		return fmt.Sprintf("%s: { wx: %s, wy: %s, wz: %s }", key, f(wx), f(wy), f(wz))
	}
	return fmt.Sprintf("%s: { wx: %s, wy: %s }", key, f(wx), f(wy))
}

// collapseFlowMap checks whether lines[startLine] opens a multi-line YAML
// flow map (has "{" but not the matching "}"). If so it joins the lines into
// a single string and returns it together with the index of the last line of
// the map. The caller can replace lines[startLine..lastLine] with the joined
// string and then apply single-line flow-map edit logic.
// Returns ("", -1) when the flow map is already on one line or the block is
// not a flow map.
func collapseFlowMap(lines []string, startLine int) (string, int) {
	l0 := lines[startLine]
	if !strings.Contains(l0, "{") || strings.Contains(l0, "}") {
		return "", -1 // single-line or not a flow map
	}
	// Count brace depth (simple, ignores braces inside quoted strings — safe
	// for the connector YAML patterns used in this codebase).
	depth := 0
	var buf strings.Builder
	for i := startLine; i < len(lines); i++ {
		if i > startLine {
			buf.WriteByte(' ')
			buf.WriteString(strings.TrimLeft(lines[i], " \t"))
		} else {
			buf.WriteString(l0)
		}
		for _, ch := range lines[i] {
			switch ch {
			case '{':
				depth++
			case '}':
				depth--
			}
		}
		if depth == 0 {
			return buf.String(), i
		}
	}
	return "", -1 // unclosed — leave as-is
}

// ReadInlineOffset reads the AUTHORED offset of the part block starting at
// startLine — the literal wx/wy/wz in the source, before any layout/autosize
// resolution rewrites it. Handles the flow-map form (`- { … offset: { wx: N,
// wy: N } … }`) and a block child `offset:` at the item's direct-child indent
// (inline `{…}` or nested wx/wy/wz lines). ok is false when the part has no
// authored offset, so callers can fall back to resolving its laid-out position.
func ReadInlineOffset(src string, startLine int) (wx, wy, wz float64, ok bool) {
	lines := strings.Split(src, "\n")
	if startLine < 0 || startLine >= len(lines) {
		return 0, 0, 0, false
	}
	itemIndent := indentOf(lines[startLine])
	childIndent := itemIndent + 2
	blockEnd := len(lines)
	for i := startLine + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if indentOf(lines[i]) <= itemIndent {
			blockEnd = i
			break
		}
	}
	comp := func(s, name string) (float64, bool) {
		m := regexp.MustCompile(name + `:\s*(-?[0-9]*\.?[0-9]+)`).FindStringSubmatch(s)
		if m == nil {
			return 0, false
		}
		v, err := strconv.ParseFloat(m[1], 64)
		return v, err == nil
	}
	parse := func(body string) (float64, float64, float64, bool) {
		x, okx := comp(body, "wx")
		y, oky := comp(body, "wy")
		z, _ := comp(body, "wz")
		if !okx && !oky {
			return 0, 0, 0, false
		}
		return x, y, z, true
	}
	offsetBraces := regexp.MustCompile(`offset:\s*\{([^}]*)\}`)
	// Flow-map item: read the offset out of the (single- or multi-line) flow form.
	// collapseFlowMap returns "" for an already-single-line map, so handle that
	// directly off the start line.
	flowStr := ""
	if strings.Contains(lines[startLine], "{") && strings.Contains(lines[startLine], "}") {
		flowStr = lines[startLine]
	} else if collapsed, _ := collapseFlowMap(lines, startLine); collapsed != "" {
		flowStr = collapsed
	}
	if flowStr != "" {
		if m := offsetBraces.FindStringSubmatch(flowStr); m != nil {
			return parse(m[1])
		}
		return 0, 0, 0, false
	}
	// Block item: find `offset:` at the item's direct-child indent.
	offsetKey := regexp.MustCompile(`^\s*offset:`)
	for i := startLine + 1; i < blockEnd; i++ {
		if indentOf(lines[i]) != childIndent || !offsetKey.MatchString(lines[i]) {
			continue
		}
		if m := offsetBraces.FindStringSubmatch(lines[i]); m != nil {
			return parse(m[1]) // inline `offset: { … }`
		}
		var body strings.Builder // block form: gather deeper wx/wy/wz lines
		for j := i + 1; j < blockEnd && (strings.TrimSpace(lines[j]) == "" || indentOf(lines[j]) > childIndent); j++ {
			body.WriteString(" ")
			body.WriteString(lines[j])
		}
		return parse(body.String())
	}
	return 0, 0, 0, false
}

func UpsertInlineKey(src string, startLine int, key string, wx, wy, wz float64) (string, bool) {
	lines := strings.Split(src, "\n")
	if startLine < 0 || startLine >= len(lines) {
		return src, false
	}
	itemIndent := indentOf(lines[startLine])
	childIndent := itemIndent + 2
	blockEnd := len(lines)
	for i := startLine + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if indentOf(lines[i]) <= itemIndent {
			blockEnd = i
			break
		}
	}
	// Flow-map form (e.g. `- { id: c, … }`), possibly spanning multiple lines
	// when keys are long. Collapse to a single string before editing so the
	// logic below always applies.
	isSingleLine := strings.Contains(lines[startLine], "{") && strings.Contains(lines[startLine], "}")
	if collapsed, lastLine := collapseFlowMap(lines, startLine); collapsed != "" {
		l := collapsed
		l = regexp.MustCompile(`,?\s*`+regexp.QuoteMeta(key)+`:\s*\{[^}]*\}`).ReplaceAllString(l, "")
		val := offsetVal(key, wx, wy, wz) + ", "
		if i := strings.Index(l, "{"); i >= 0 {
			l = l[:i+1] + " " + val + strings.TrimLeft(l[i+1:], " ")
		}
		l = regexp.MustCompile(`\{\s*,`).ReplaceAllString(l, "{ ")
		l = regexp.MustCompile(`,\s*,`).ReplaceAllString(l, ", ")
		l = regexp.MustCompile(`,\s*\}`).ReplaceAllString(l, " }")
		out := append([]string{}, lines[:startLine]...)
		out = append(out, l)
		out = append(out, lines[lastLine+1:]...)
		return strings.Join(out, "\n"), true
	}
	if isSingleLine {
		l := lines[startLine]
		// drop an existing top-level offset/bend (value has no nested braces)
		l = regexp.MustCompile(`,?\s*`+regexp.QuoteMeta(key)+`:\s*\{[^}]*\}`).ReplaceAllString(l, "")
		val := offsetVal(key, wx, wy, wz) + ", "
		if i := strings.Index(l, "{"); i >= 0 {
			l = l[:i+1] + " " + val + strings.TrimLeft(l[i+1:], " ")
		}
		// Normalize commas the removal/insert can leave behind ("{ ,",
		// ", ,", ", }") — a stray double comma is invalid YAML and made
		// re-dragging a frozen flow-form node silently fail to render.
		l = regexp.MustCompile(`\{\s*,`).ReplaceAllString(l, "{ ")
		l = regexp.MustCompile(`,\s*,`).ReplaceAllString(l, ", ")
		l = regexp.MustCompile(`,\s*\}`).ReplaceAllString(l, " }")
		lines[startLine] = l
		return strings.Join(lines, "\n"), true
	}
	newLine := strings.Repeat(" ", childIndent) + offsetVal(key, wx, wy, wz)
	keyRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `:`)
	for i := startLine + 1; i < blockEnd; i++ {
		// Only match the key at THIS item's direct-child indent — a deeper
		// `key:` belongs to a nested part, not to the item at startLine.
		// Replacing a grandchild's offset with a line at childIndent mis-
		// indents it and corrupts the YAML.
		if indentOf(lines[i]) == childIndent && keyRe.MatchString(lines[i]) {
			// drop a block-form value's deeper-indented sub-lines too
			j := i + 1
			for j < blockEnd && strings.TrimSpace(lines[j]) != "" && indentOf(lines[j]) > indentOf(lines[i]) {
				j++
			}
			out := append([]string{}, lines[:i]...)
			out = append(out, newLine)
			out = append(out, lines[j:]...)
			return strings.Join(out, "\n"), true
		}
	}
	out := append([]string{}, lines[:startLine+1]...)
	out = append(out, newLine)
	out = append(out, lines[startLine+1:]...)
	return strings.Join(out, "\n"), true
}

// UpsertInlineList replaces or inserts an inline `key: [ { wx, wy }, … ]`
// on the connector block at startLine, and removes any existing `bend`
// (waypoints supersede a single-corner bend). An empty pts list removes the
// key entirely (reverts to the auto route). Values are always written inline
// (one line), so replacement just swaps single child lines — mirroring
// UpsertInlineKey's comment-preserving, flow/block-aware surgery.
func UpsertInlineList(src string, startLine int, key string, pts [][2]float64) (string, bool) {
	lines := strings.Split(src, "\n")
	if startLine < 0 || startLine >= len(lines) {
		return src, false
	}
	var vb strings.Builder
	vb.WriteString("[")
	for i, p := range pts {
		if i > 0 {
			vb.WriteString(", ")
		}
		fmt.Fprintf(&vb, "{ wx: %.0f, wy: %.0f }", p[0], p[1])
	}
	vb.WriteString("]")
	listVal := vb.String()

	itemIndent := indentOf(lines[startLine])
	childIndent := itemIndent + 2

	// Flow-map form: connector is `- { from: a, to: b, … }`.
	// This may span multiple lines when keys are long; collapse to one string
	// first so the single-line edit logic always applies.
	isSingleLine := strings.Contains(lines[startLine], "{") && strings.Contains(lines[startLine], "}")
	if collapsed, lastLine := collapseFlowMap(lines, startLine); collapsed != "" {
		// Multi-line flow map → collapse, edit, replace the span with one line.
		l := collapsed
		l = regexp.MustCompile(`,?\s*bend:\s*\{[^}]*\}`).ReplaceAllString(l, "")
		l = regexp.MustCompile(`,?\s*`+regexp.QuoteMeta(key)+`:\s*\[[^\]]*\]`).ReplaceAllString(l, "")
		if len(pts) > 0 {
			val := fmt.Sprintf("%s: %s, ", key, listVal)
			if i := strings.Index(l, "{"); i >= 0 {
				l = l[:i+1] + " " + val + strings.TrimLeft(l[i+1:], " ")
			}
		}
		l = regexp.MustCompile(`\{\s*,`).ReplaceAllString(l, "{ ")
		l = regexp.MustCompile(`,\s*,`).ReplaceAllString(l, ", ")
		l = regexp.MustCompile(`,\s*\}`).ReplaceAllString(l, " }")
		out := append([]string{}, lines[:startLine]...)
		out = append(out, l)
		out = append(out, lines[lastLine+1:]...)
		return strings.Join(out, "\n"), true
	}
	if isSingleLine {
		l := lines[startLine]
		l = regexp.MustCompile(`,?\s*bend:\s*\{[^}]*\}`).ReplaceAllString(l, "")
		l = regexp.MustCompile(`,?\s*`+regexp.QuoteMeta(key)+`:\s*\[[^\]]*\]`).ReplaceAllString(l, "")
		if len(pts) > 0 {
			val := fmt.Sprintf("%s: %s, ", key, listVal)
			if i := strings.Index(l, "{"); i >= 0 {
				l = l[:i+1] + " " + val + strings.TrimLeft(l[i+1:], " ")
			}
		}
		l = regexp.MustCompile(`\{\s*,`).ReplaceAllString(l, "{ ")
		l = regexp.MustCompile(`,\s*,`).ReplaceAllString(l, ", ")
		l = regexp.MustCompile(`,\s*\}`).ReplaceAllString(l, " }")
		lines[startLine] = l
		return strings.Join(lines, "\n"), true
	}

	// Block form: drop existing single-line `bend:`/`waypoints:` children,
	// then insert the new inline list (if any) as the first child line.
	dropRe := regexp.MustCompile(`^\s*(?:bend|` + regexp.QuoteMeta(key) + `):`)
	kept := make([]string, 0, len(lines))
	for i, l := range lines {
		if i > startLine && indentOf(l) > itemIndent && dropRe.MatchString(l) {
			continue
		}
		kept = append(kept, l)
	}
	lines = kept
	if len(pts) > 0 {
		newLine := strings.Repeat(" ", childIndent) + key + ": " + listVal
		out := append([]string{}, lines[:startLine+1]...)
		out = append(out, newLine)
		out = append(out, lines[startLine+1:]...)
		lines = out
	}
	return strings.Join(lines, "\n"), true
}

// splitTopCommas splits a YAML flow-map body on top-level commas, respecting
// nested {}/[] and double-quoted strings.
func splitTopCommas(s string) []string {
	var out []string
	depth, inStr, esc := 0, false, false
	var b strings.Builder
	for _, r := range s {
		if esc { // previous char was a backslash inside a string: take literally
			esc = false
			b.WriteRune(r)
			continue
		}
		switch r {
		case '\\':
			if inStr {
				esc = true
			}
		case '"':
			inStr = !inStr
		case '{', '[':
			if !inStr {
				depth++
			}
		case '}', ']':
			if !inStr {
				depth--
			}
		case ',':
			if !inStr && depth == 0 {
				out = append(out, b.String())
				b.Reset()
				continue
			}
		}
		b.WriteRune(r)
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, b.String())
	}
	return out
}

func unquoteYAML(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		if uq, err := strconv.Unquote(v); err == nil {
			return uq
		}
		return v[1 : len(v)-1]
	}
	return v
}

// stringifyYAML renders a parsed YAML scalar as the string the form shows.
func stringifyYAML(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// ReadPath walks a dotted path (e.g. "style.palette.top") through a parsed
// YAML map and returns the scalar at the leaf as a string ("" if absent).
func ReadPath(m map[string]interface{}, path string) string {
	var cur interface{} = m
	for _, seg := range strings.Split(path, ".") {
		mm, ok := cur.(map[string]interface{})
		if !ok {
			return ""
		}
		cur, ok = mm[seg]
		if !ok {
			return ""
		}
	}
	if _, isMap := cur.(map[string]interface{}); isMap {
		return ""
	}
	return stringifyYAML(cur)
}

// FindNodeMap locates a node/part subtree by id anywhere in the parsed doc:
// a top-level node keyed by name, or any nested part whose `id` matches.
func FindNodeMap(root map[string]interface{}, id string) map[string]interface{} {
	if nodes, ok := root["nodes"].(map[string]interface{}); ok {
		if n, ok := nodes[id].(map[string]interface{}); ok {
			return n
		}
	}
	var walk func(v interface{}) map[string]interface{}
	walk = func(v interface{}) map[string]interface{} {
		switch t := v.(type) {
		case map[string]interface{}:
			if s, _ := t["id"].(string); s == id {
				return t
			}
			for _, vv := range t {
				if r := walk(vv); r != nil {
					return r
				}
			}
		case []interface{}:
			for _, vv := range t {
				if r := walk(vv); r != nil {
					return r
				}
			}
		}
		return nil
	}
	return walk(root)
}

// FindConnectors returns the first `connectors` list in the parsed doc
// (matching FindConnectorLine's "first connectors block" convention).
func FindConnectors(root map[string]interface{}) []interface{} {
	if nodes, ok := root["nodes"].(map[string]interface{}); ok {
		if scene, ok := nodes["scene"].(map[string]interface{}); ok {
			if c, ok := scene["connectors"].([]interface{}); ok {
				return c
			}
		}
	}
	var walk func(v interface{}) []interface{}
	walk = func(v interface{}) []interface{} {
		switch t := v.(type) {
		case map[string]interface{}:
			if c, ok := t["connectors"].([]interface{}); ok {
				return c
			}
			for _, vv := range t {
				if r := walk(vv); r != nil {
					return r
				}
			}
		case []interface{}:
			for _, vv := range t {
				if r := walk(vv); r != nil {
					return r
				}
			}
		}
		return nil
	}
	return walk(root)
}

// buildChain renders a nested inline map "{ k1: { k2: … : v } }" for a
// dotted path that doesn't yet exist in the source.
// scalarOf renders a scalar for write-back: quoted via yamlScalar normally, or
// VERBATIM when bare — used for typed booleans the field schema knows are NOT
// strings, so `faceSplit: true` is written as the bool true, not the string
// "true" (which yamlScalar would quote and break *bool parsing).
func scalarOf(v string, bare bool) string {
	if bare {
		return v
	}
	return yamlScalar(v)
}

func buildChain(keys []string, v string, bare bool) string {
	if len(keys) == 1 {
		return "{ " + keys[0] + ": " + scalarOf(v, bare) + " }"
	}
	return "{ " + keys[0] + ": " + buildChain(keys[1:], v, bare) + " }"
}

// setInInlineMap sets a (possibly nested) path inside a YAML flow map string
// "{ … }", creating intermediate maps as needed; empty value removes the leaf.
func setInInlineMap(s string, path []string, value string, bare bool) string {
	open := strings.Index(s, "{")
	close := strings.LastIndex(s, "}")
	if open < 0 || close <= open {
		return s
	}
	pre, post := s[:open], s[close+1:]
	inner := s[open+1 : close]
	entries := splitTopCommas(inner)
	idx := -1
	for i, e := range entries {
		k := strings.TrimSpace(strings.SplitN(e, ":", 2)[0])
		if k == path[0] {
			idx = i
			break
		}
	}
	if idx >= 0 {
		if len(path) == 1 {
			if value == "" {
				entries = append(entries[:idx], entries[idx+1:]...)
			} else {
				entries[idx] = path[0] + ": " + scalarOf(value, bare)
			}
		} else {
			cur := strings.TrimSpace(strings.SplitN(entries[idx], ":", 2)[1])
			if strings.HasPrefix(cur, "{") {
				entries[idx] = path[0] + ": " + setInInlineMap(cur, path[1:], value, bare)
			} else {
				entries[idx] = path[0] + ": " + buildChain(path[1:], value, bare)
			}
		}
	} else if value != "" {
		if len(path) == 1 {
			entries = append(entries, path[0]+": "+scalarOf(value, bare))
		} else {
			entries = append(entries, path[0]+": "+buildChain(path[1:], value, bare))
		}
	}
	// trim blanks
	out := entries[:0]
	for _, e := range entries {
		if strings.TrimSpace(e) != "" {
			out = append(out, strings.TrimSpace(e))
		}
	}
	if len(out) == 0 {
		return pre + "{}" + post
	}
	return pre + "{ " + strings.Join(out, ", ") + " }" + post
}

// SetField writes a dotted path into the node/edge block at startLine,
// preserving comments. Flow-form items edit inside their braces; block-form
// items recurse into inline child maps or deeper blocks, creating inline maps
// for missing intermediates. Empty value removes a scalar leaf.
func SetField(src string, startLine int, path []string, value string) (string, bool) {
	return setFieldImpl(src, startLine, path, value, false)
}

// SetFieldRaw is SetField for a value the caller KNOWS is a non-string scalar
// (typed boolean such as effects.faceSplit / wireframe): the value is written
// verbatim, so `faceSplit: true` lands as the bool true rather than the quoted
// string "true" yamlScalar would emit for a reserved word.
func SetFieldRaw(src string, startLine int, path []string, value string) (string, bool) {
	return setFieldImpl(src, startLine, path, value, true)
}

func setFieldImpl(src string, startLine int, path []string, value string, bare bool) (string, bool) {
	lines := strings.Split(src, "\n")
	if startLine < 0 || startLine >= len(lines) {
		return src, false
	}
	line := lines[startLine]
	// Flow-form item: the whole element lives in `{ … }`.
	// The braces may span multiple lines (e.g. a connector with a long label).
	// Collapse to a single string, edit, then replace the line span.
	if open := strings.Index(line, "{"); open >= 0 {
		if close := strings.LastIndex(line, "}"); close > open {
			// Single-line flow map.
			lines[startLine] = line[:open] + setInInlineMap(line[open:close+1], path, value, bare) + line[close+1:]
			return strings.Join(lines, "\n"), true
		}
		// Multi-line flow map: { on startLine, } on a later line.
		if collapsed, lastLine := collapseFlowMap(lines, startLine); collapsed != "" {
			co := strings.Index(collapsed, "{")
			cc := strings.LastIndex(collapsed, "}")
			if co >= 0 && cc > co {
				edited := collapsed[:co] + setInInlineMap(collapsed[co:cc+1], path, value, bare) + collapsed[cc+1:]
				out := append([]string{}, lines[:startLine]...)
				out = append(out, edited)
				out = append(out, lines[lastLine+1:]...)
				return strings.Join(out, "\n"), true
			}
		}
	}
	if len(path) == 1 {
		return upsertScalar(src, startLine, path[0], value, bare)
	}
	// Block-form item: descend into path[0].
	itemIndent := indentOf(line)
	childIndent := itemIndent + 2
	blockEnd := len(lines)
	for k := startLine + 1; k < len(lines); k++ {
		if strings.TrimSpace(lines[k]) == "" {
			continue
		}
		if indentOf(lines[k]) <= itemIndent {
			blockEnd = k
			break
		}
	}
	keyRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(path[0]) + `:`)
	for k := startLine + 1; k < blockEnd; k++ {
		if indentOf(lines[k]) != childIndent || !keyRe.MatchString(lines[k]) {
			continue
		}
		rest := strings.TrimSpace(lines[k][strings.Index(lines[k], ":")+1:])
		switch {
		case strings.HasPrefix(rest, "{"):
			open := strings.Index(lines[k], "{")
			close := strings.LastIndex(lines[k], "}")
			lines[k] = lines[k][:open] + setInInlineMap(lines[k][open:close+1], path[1:], value, bare) + lines[k][close+1:]
			return strings.Join(lines, "\n"), true
		case rest == "" || strings.HasPrefix(rest, "&"):
			return setFieldImpl(strings.Join(lines, "\n"), k, path[1:], value, bare) // recurse into sub-block
		default:
			pre := lines[k][:strings.Index(lines[k], ":")+1]
			lines[k] = pre + " " + buildChain(path[1:], value, bare)
			return strings.Join(lines, "\n"), true
		}
	}
	if value == "" {
		return strings.Join(lines, "\n"), true
	}
	newLine := strings.Repeat(" ", childIndent) + path[0] + ": " + buildChain(path[1:], value, bare)
	out := append([]string{}, lines[:startLine+1]...)
	out = append(out, newLine)
	out = append(out, lines[startLine+1:]...)
	return strings.Join(out, "\n"), true
}

// yamlScalar renders a value for write-back, quoting when it contains
// characters that aren't safe bare (spaces, ':', flow punctuation, …).
var bareSafeRe = regexp.MustCompile(`^[A-Za-z0-9_.\-/]+$`)

// reservedYAMLScalar matches NON-numeric plain values YAML would re-decode as
// something other than a string — null/~ and the booleans (incl. YAML 1.1
// yes/no/on/off), plus the special floats. Writing `label: null` bare would
// round-trip to an empty/typed value, silently losing the literal text, so these
// must be quoted. Numbers are deliberately NOT here: numeric fields (geom.w,
// font size, …) legitimately want a bare number, and yamlScalar can't see the
// target field's type.
var reservedYAMLScalar = regexp.MustCompile(`(?i)^(null|~|true|false|yes|no|on|off|y|n|\.nan|[-+]?\.inf)$`)

func yamlScalar(v string) string {
	if v == "" {
		return `""`
	}
	if bareSafeRe.MatchString(v) && !reservedYAMLScalar.MatchString(v) {
		return v
	}
	// Double-quoted YAML scalar. Escape control characters too — a raw newline
	// would otherwise be folded back to a space (or break the line) on the next
	// parse, silently corrupting multi-line label/content values.
	s := v
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return `"` + s + `"`
}

// upsertScalar sets `key: value` inside the node/edge block at startLine
// (flow or block form), preserving comments/formatting. An empty value
// removes the key. Mirrors UpsertInlineKey's surgery for scalar values.
func upsertScalar(src string, startLine int, key, value string, bare bool) (string, bool) {
	lines := strings.Split(src, "\n")
	if startLine < 0 || startLine >= len(lines) {
		return src, false
	}
	itemIndent := indentOf(lines[startLine])
	childIndent := itemIndent + 2
	remove := value == ""

	editFlowMap := func(l string) string {
		l = regexp.MustCompile(`,?\s*`+regexp.QuoteMeta(key)+`:\s*("(?:[^"\\]|\\.)*"|[^,}]*)`).ReplaceAllString(l, "")
		if !remove {
			val := fmt.Sprintf("%s: %s, ", key, scalarOf(value, bare))
			if i := strings.Index(l, "{"); i >= 0 {
				l = l[:i+1] + " " + val + strings.TrimLeft(l[i+1:], " ")
			}
		}
		l = regexp.MustCompile(`\{\s*,`).ReplaceAllString(l, "{ ")
		l = regexp.MustCompile(`,\s*,`).ReplaceAllString(l, ", ")
		l = regexp.MustCompile(`,\s*\}`).ReplaceAllString(l, " }")
		return l
	}
	if strings.Contains(lines[startLine], "{") && strings.Contains(lines[startLine], "}") {
		lines[startLine] = editFlowMap(lines[startLine])
		return strings.Join(lines, "\n"), true
	}
	if collapsed, lastLine := collapseFlowMap(lines, startLine); collapsed != "" {
		edited := editFlowMap(collapsed)
		out := append([]string{}, lines[:startLine]...)
		out = append(out, edited)
		out = append(out, lines[lastLine+1:]...)
		return strings.Join(out, "\n"), true
	}

	// Block form. The anchor key on the start line (`- key: value`) is edited
	// in place; never removed (it holds the item together).
	if m := regexp.MustCompile(`^(\s*-\s*)` + regexp.QuoteMeta(key) + `:`).FindStringSubmatch(lines[startLine]); m != nil {
		if !remove {
			lines[startLine] = m[1] + key + ": " + scalarOf(value, bare)
		}
		return strings.Join(lines, "\n"), true
	}
	blockEnd := len(lines)
	for i := startLine + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if indentOf(lines[i]) <= itemIndent {
			blockEnd = i
			break
		}
	}
	keyRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `:`)
	newLine := strings.Repeat(" ", childIndent) + key + ": " + scalarOf(value, bare)
	for i := startLine + 1; i < blockEnd; i++ {
		if indentOf(lines[i]) == childIndent && keyRe.MatchString(lines[i]) {
			out := append([]string{}, lines[:i]...)
			if !remove {
				out = append(out, newLine)
			}
			out = append(out, lines[i+1:]...)
			return strings.Join(out, "\n"), true
		}
	}
	if remove {
		return strings.Join(lines, "\n"), true
	}
	out := append([]string{}, lines[:startLine+1]...)
	out = append(out, newLine)
	out = append(out, lines[startLine+1:]...)
	return strings.Join(out, "\n"), true
}

// FindPartIDLine returns the line index of `id: <id>` (with optional
// `- ` prefix and quotes), or -1.
func FindPartIDLine(src, id string) int {
	// Match `id: <id>` in both block form (`- id: c` / `id: c`) and flow
	// form (`- { id: c, … }`). The id is bounded by a comma, brace,
	// whitespace, or line end so "c" never matches "client".
	re := regexp.MustCompile(`(?:^|[-{,]\s*)id:\s*"?` + regexp.QuoteMeta(id) + `"?\s*(?:,|}|$)`)
	for i, l := range strings.Split(src, "\n") {
		if re.MatchString(l) {
			return i
		}
	}
	return -1
}

// FindConnectorLine returns the line index of the ci-th `- ` item under
// the first `connectors:` key, or -1.
func FindConnectorLine(src string, ci int) int {
	if ci < 0 {
		return -1
	}
	lines := strings.Split(src, "\n")
	connLine, connIndent := -1, 0
	header := regexp.MustCompile(`^\s*connectors:\s*(?:#.*)?$`)
	for i, l := range lines {
		if header.MatchString(l) {
			connLine, connIndent = i, indentOf(l)
			break
		}
	}
	if connLine < 0 {
		return -1
	}
	itemRe := regexp.MustCompile(`^\s*-\s`)
	itemIndent, n := -1, 0
	for i := connLine + 1; i < len(lines); i++ {
		trim := strings.TrimSpace(lines[i])
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		ind := indentOf(lines[i])
		isItem := itemRe.MatchString(lines[i])
		if ind < connIndent || (ind == connIndent && !isItem) {
			break
		}
		if itemIndent < 0 && isItem {
			itemIndent = ind
		}
		// Count only the connector sequence itself. Nested 2-D/3-D waypoint
		// lists (including indentationless values) are not other connectors.
		if isItem && ind == itemIndent {
			if n == ci {
				return i
			}
			n++
		}
	}
	return -1
}

// FindCanvasLine returns the line index of the top-level `canvas:` key
// (flow `canvas: { … }` or block form), or -1.
func FindCanvasLine(src string) int {
	re := regexp.MustCompile(`^\s*canvas\s*:`)
	for i, l := range strings.Split(src, "\n") {
		if re.MatchString(l) {
			return i
		}
	}
	return -1
}

// itemEnd returns the line index just past the YAML list item that starts at
// `start` — the next non-blank line indented ≤ the item, or EOF.
func itemEnd(lines []string, start int) int {
	ind := indentOf(lines[start])
	for i := start + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if indentOf(lines[i]) <= ind {
			return i
		}
	}
	return len(lines)
}

// findPartItemRange locates the `- ` list item that declares part `id`,
// returning [start,end) line indices (covers flow `- { id: x … }` and block
// `- id: x` / `- shape: …\n  id: x` forms, at any nesting depth).
func findPartItemRange(src, id string) (int, int, bool) {
	lines := strings.Split(src, "\n")
	reDash := regexp.MustCompile(`(?:^|[-{,]\s*)id:\s*"?` + regexp.QuoteMeta(id) + `"?\s*(?:,|}|$)`)
	reIndented := regexp.MustCompile(`^\s*id:\s*"?` + regexp.QuoteMeta(id) + `"?\s*$`)
	idLine := -1
	for i, l := range lines {
		if reDash.MatchString(l) || reIndented.MatchString(l) {
			idLine = i
			break
		}
	}
	if idLine < 0 {
		return 0, 0, false
	}
	start := idLine
	for start >= 0 && !strings.HasPrefix(strings.TrimSpace(lines[start]), "- ") {
		start--
	}
	if start < 0 {
		return 0, 0, false
	}
	return start, itemEnd(lines, start), true
}

// ConnectorItemRange returns [start,end) for the ci-th connector item.
func ConnectorItemRange(src string, ci int) (int, int, bool) {
	start := FindConnectorLine(src, ci)
	if start < 0 {
		return 0, 0, false
	}
	return start, itemEnd(strings.Split(src, "\n"), start), true
}

// findListItemLine returns the line of the idx-th `- ` item under the first
// top-level `<blockKey>:` block, or -1.
func findListItemLine(src, blockKey string, idx int) int {
	lines := strings.Split(src, "\n")
	bl, bind := -1, 0
	keyRe := regexp.MustCompile(`^(\s*)` + regexp.QuoteMeta(blockKey) + `:\s*$`)
	for i, l := range lines {
		if m := keyRe.FindStringSubmatch(l); m != nil {
			bl, bind = i, len(m[1])
			break
		}
	}
	if bl < 0 {
		return -1
	}
	itemRe := regexp.MustCompile(`^\s*-\s`)
	n := 0
	for i := bl + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if indentOf(lines[i]) <= bind {
			break
		}
		if itemRe.MatchString(lines[i]) && indentOf(lines[i]) > bind {
			if n == idx {
				return i
			}
			n++
		}
	}
	return -1
}

func DeleteLineRange(src string, start, end int) string {
	lines := strings.Split(src, "\n")
	if start < 0 || start >= len(lines) || end < start {
		return src
	}
	return strings.Join(append(append([]string{}, lines[:start]...), lines[end:]...), "\n")
}

// DeletePart removes a node and any connectors that reference it (so the
// scene stays valid — no dangling from/to). Connectors are removed
// bottom-up so earlier indices don't shift.
// ReferencedAnchorsInPart returns the YAML anchor names (&name) DEFINED inside
// the part block `id` that are still referenced (*name) somewhere outside it.
// Text surgery can't relocate an anchor's meaning, so deleting such a block
// would leave a dangling alias and corrupt the document — callers refuse it.
func ReferencedAnchorsInPart(src, id string) []string {
	s, e, ok := findPartItemRange(src, id)
	if !ok {
		return nil
	}
	lines := strings.Split(src, "\n")
	block := strings.Join(lines[s:e], "\n")
	rest := strings.Join(append(append([]string{}, lines[:s]...), lines[e:]...), "\n")
	var out []string
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`&([A-Za-z0-9_-]+)`).FindAllStringSubmatch(block, -1) {
		name := m[1]
		if seen[name] {
			continue
		}
		if regexp.MustCompile(`\*` + regexp.QuoteMeta(name) + `\b`).MatchString(rest) {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func DeletePart(src, id string) (string, bool) {
	s, e, ok := findPartItemRange(src, id)
	if !ok {
		return src, false
	}
	// Deleting a container (group/boundary/composite) also removes its nested
	// parts — collect EVERY id inside the removed subtree so we clean up the
	// connectors and place: refs that point at any of them, not just the root.
	removed := map[string]bool{id: true}
	idRe := regexp.MustCompile(`id:\s*"?([A-Za-z0-9_-]+)`)
	for _, l := range strings.Split(src, "\n")[s:e] {
		if m := idRe.FindStringSubmatch(l); m != nil {
			removed[m[1]] = true
		}
	}
	// A connector references a removed id by bare id ("core") or an anchor-
	// qualified endpoint ("core.front"); both must go.
	refsRemoved := func(v interface{}) bool {
		s, _ := v.(string)
		if removed[s] {
			return true
		}
		// Strip a ".anchor" suffix and/or a "~N" stack-instance suffix to reach
		// the base part id (e.g. "db~2", "db.left", "db~2.left" all → "db").
		base := s
		if i := strings.IndexByte(base, '.'); i > 0 {
			base = base[:i]
		}
		if i := strings.IndexByte(base, '~'); i > 0 {
			base = base[:i]
		}
		return removed[base]
	}
	conns := FindConnectors(mustParse(src))
	var refs []int
	for i, c := range conns {
		if m, ok := c.(map[string]interface{}); ok {
			if refsRemoved(m["from"]) || refsRemoved(m["to"]) {
				refs = append(refs, i)
			}
		}
	}
	out := src
	for i := len(refs) - 1; i >= 0; i-- {
		if cs, ce, ok := ConnectorItemRange(out, refs[i]); ok {
			out = DeleteLineRange(out, cs, ce)
		}
	}
	// Annotations anchored to any removed part would dangle too.
	if anns, ok := mustParse(src)["annotations"].([]interface{}); ok {
		var aref []int
		for i, a := range anns {
			if m, ok := a.(map[string]interface{}); ok {
				if refsRemoved(m["anchor"]) {
					aref = append(aref, i)
				}
			}
		}
		for i := len(aref) - 1; i >= 0; i-- {
			if as := findListItemLine(out, "annotations", aref[i]); as >= 0 {
				out = DeleteLineRange(out, as, itemEnd(strings.Split(out, "\n"), as))
			}
		}
	}
	// Other parts positioned relative to any removed part fall back to layout.
	for rid := range removed {
		out = stripPlaceReferencing(out, rid)
	}
	s, e, ok = findPartItemRange(out, id)
	if !ok {
		return src, false
	}
	return DeleteLineRange(out, s, e), true
}

// stripPlaceReferencing removes the place: block from any part whose place
// constraint points at `id` (rightOf/leftOf/inFrontOf/behind/above).
func stripPlaceReferencing(src, id string) string {
	refs := map[string]bool{}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			if pl, ok := t["place"].(map[string]interface{}); ok {
				for _, k := range []string{"rightOf", "leftOf", "inFrontOf", "behind", "above"} {
					if s, _ := pl[k].(string); s == id {
						if pid, _ := t["id"].(string); pid != "" {
							refs[pid] = true
						}
					}
				}
			}
			for _, vv := range t {
				walk(vv)
			}
		case []interface{}:
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	walk(mustParse(src))
	out := src
	for pid := range refs {
		if s, e, ok := findPartItemRange(out, pid); ok {
			out = removeKeyInRange(out, s, e, "place")
		}
	}
	return out
}

// removeKeyInRange deletes `key:` (inline flow value or block child) from the
// list item spanning [start,end).
func removeKeyInRange(src string, start, end int, key string) string {
	lines := strings.Split(src, "\n")
	if start < 0 || start >= len(lines) {
		return src
	}
	if strings.Contains(lines[start], "{") && strings.Contains(lines[start], "}") {
		l := regexp.MustCompile(`,?\s*`+regexp.QuoteMeta(key)+`:\s*\{[^}]*\}`).ReplaceAllString(lines[start], "")
		l = regexp.MustCompile(`\{\s*,`).ReplaceAllString(l, "{ ")
		l = regexp.MustCompile(`,\s*,`).ReplaceAllString(l, ", ")
		l = regexp.MustCompile(`,\s*\}`).ReplaceAllString(l, " }")
		lines[start] = l
		return strings.Join(lines, "\n")
	}
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `:`)
	for i := start + 1; i < end && i < len(lines); i++ {
		if re.MatchString(lines[i]) {
			ind := indentOf(lines[i])
			j := i + 1
			for j < len(lines) && (strings.TrimSpace(lines[j]) == "" || indentOf(lines[j]) > ind) {
				j++
			}
			return strings.Join(append(append([]string{}, lines[:i]...), lines[j:]...), "\n")
		}
	}
	return src
}

// AddPart appends a default rectangle node to the scene's (shallowest)
// parts: list. In an auto-layout scene it joins the layout; otherwise it
// lands near the origin and the user drags it.
func AddPart(src string) (string, bool) {
	lines := strings.Split(src, "\n")
	partsLine, partsIndent := -1, 0
	re := regexp.MustCompile(`^( *)parts:\s*$`)
	for i, l := range lines {
		if m := re.FindStringSubmatch(l); m != nil {
			if partsLine < 0 || len(m[1]) < partsIndent {
				partsLine, partsIndent = i, len(m[1])
			}
		}
	}
	if partsLine < 0 {
		return src, false
	}
	itemIndent, end := seqBlockEnd(lines, partsLine, partsIndent)
	newID := uniquePartID(src, "node")
	nl := strings.Repeat(" ", itemIndent) +
		fmt.Sprintf(`- { id: %s, shape: rectangle, geom: { w: 80, d: 80, h: 30 }, label: "New" }`, newID)
	out := append(append(append([]string{}, lines[:end]...), nl), lines[end:]...)
	return strings.Join(out, "\n"), true
}

// stripInlineOffset removes an inline `offset: { ... }` (and a dangling comma)
// from a part's flow/line text, so a reparented node is positioned by its new
// parent instead of a stale offset in the old frame.
// RemovePartKey removes a direct-child key (e.g. "place") from the part block
// with the given id — handling both the inline flow form (`- { … place: {…} }`)
// and the block form (a `place:` line at the item's child indent, with any
// deeper sub-lines). Used by reparent to drop a now-dangling place reference.
func RemovePartKey(src, id, key string) string {
	line := FindPartIDLine(src, id)
	if line < 0 {
		return src
	}
	lines := strings.Split(src, "\n")
	itemIndent := indentOf(lines[line])
	childIndent := itemIndent + 2
	// Inline flow item: strip `key: {…}` or `key: scalar` from the single line.
	if strings.Contains(lines[line], "{") && strings.Contains(lines[line], "}") {
		l := lines[line]
		l = regexp.MustCompile(`,?\s*`+regexp.QuoteMeta(key)+`:\s*\{[^}]*\}`).ReplaceAllString(l, "")
		l = regexp.MustCompile(`,?\s*`+regexp.QuoteMeta(key)+`:\s*[^,}]+`).ReplaceAllString(l, "")
		l = regexp.MustCompile(`\{\s*,`).ReplaceAllString(l, "{ ")
		lines[line] = l
		return strings.Join(lines, "\n")
	}
	// Block item: drop the `key:` line at child indent plus its nested body.
	blockEnd := len(lines)
	for i := line + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if indentOf(lines[i]) <= itemIndent {
			blockEnd = i
			break
		}
	}
	keyRe := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `:`)
	for i := line + 1; i < blockEnd; i++ {
		if indentOf(lines[i]) == childIndent && keyRe.MatchString(lines[i]) {
			j := i + 1
			for j < blockEnd && (strings.TrimSpace(lines[j]) == "" || indentOf(lines[j]) > childIndent) {
				j++
			}
			return strings.Join(append(append([]string{}, lines[:i]...), lines[j:]...), "\n")
		}
	}
	return src
}

func stripInlineOffset(s string) string {
	s = regexp.MustCompile(`offset:\s*\{[^}]*\}\s*,?\s*`).ReplaceAllString(s, "")
	s = regexp.MustCompile(`,\s*offset:\s*\{[^}]*\}`).ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "{ , ", "{ ")
	s = strings.ReplaceAll(s, "{,", "{")
	return s
}

// MovePart relocates a part's item block into another parent's parts: list,
// re-indenting it and dropping its now-stale offset so the new parent places
// it. targetParentID == "" moves the part to the SCENE-ROOT parts: block. It is
// a no-op (false) when the part isn't found, the target group has no parts:
// block to find/seed, or the move is degenerate (into itself).
// expandFlowItem converts a single-line flow-map list item
//
//	<ind>- { k1: v1, k2: v2, … }
//
// into block form
//
//	<ind>- k1: v1
//	<ind+2>k2: v2
//	…
//
// so a nested block `parts:` can be appended under it (a group authored or
// morphed in flow style otherwise has no place for children). Nested braces
// (e.g. geom: { … }) stay inline — splitTopCommas only splits at depth 0.
// Returns nil if the line isn't a closed single-line flow item.
func expandFlowItem(line string) []string {
	ind := indentOf(line)
	t := strings.TrimLeft(line, " ")
	if !strings.HasPrefix(t, "- ") {
		return nil
	}
	body := strings.TrimSpace(t[2:])
	if !strings.HasPrefix(body, "{") || !strings.HasSuffix(body, "}") {
		return nil
	}
	var out []string
	first := true
	for _, kv := range splitTopCommas(body[1 : len(body)-1]) {
		if kv = strings.TrimSpace(kv); kv == "" {
			continue
		}
		// As a BLOCK mapping line the key colon needs a trailing space — a tight
		// `offset:{…}` is valid inline but not block. Space only the first
		// (key) colon; inner inline values keep their own colons.
		if i := strings.IndexByte(kv, ':'); i >= 0 && i+1 < len(kv) && kv[i+1] != ' ' {
			kv = kv[:i+1] + " " + kv[i+1:]
		}
		if first {
			out = append(out, strings.Repeat(" ", ind)+"- "+kv)
			first = false
		} else {
			out = append(out, strings.Repeat(" ", ind+2)+kv)
		}
	}
	if first {
		return nil // empty flow map — nothing to expand
	}
	return out
}

// dropEmptyGroupParts removes a `parts:` key (at indent srcIndent-2) that has no
// remaining child items — but ONLY when it belongs to a GROUP list item, never
// the scene's own parts: (which must stay). Called after MovePart removes an
// item, to clean up the source group it may have emptied.
func dropEmptyGroupParts(lines []string, srcIndent int) []string {
	pIndent := srcIndent - 2
	if pIndent < 0 {
		return lines
	}
	for i := 0; i < len(lines); i++ {
		if indentOf(lines[i]) != pIndent || strings.TrimSpace(lines[i]) != "parts:" {
			continue
		}
		empty := true
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				continue
			}
			empty = indentOf(lines[j]) <= pIndent
			break
		}
		if !empty {
			continue
		}
		// Owned by a group only if the nearest line at pIndent-2 is a list item.
		isGroup := false
		for k := i - 1; k >= 0; k-- {
			if strings.TrimSpace(lines[k]) == "" {
				continue
			}
			if indentOf(lines[k]) == pIndent-2 {
				isGroup = strings.HasPrefix(strings.TrimLeft(lines[k], " "), "-")
				break
			}
			if indentOf(lines[k]) < pIndent-2 {
				break
			}
		}
		if isGroup {
			return append(append([]string{}, lines[:i]...), lines[i+1:]...)
		}
	}
	return lines
}

// ExpandInlineParts rewrites a single-line inline parts list
//
//	<ind>parts: [ {…}, {…} ]
//
// into block form
//
//	<ind>parts:
//	<ind+2>- {…}
//	<ind+2>- {…}
//
// so the per-child line locators (FindPartIDLine / findPartItemRange) can address
// each nested child individually. Without this, an edit (delete/duplicate/set-
// field) of a child inside an inline list resolves to the OUTER group. Items keep
// their flow-map form (one per line, addressable). No-op when no inline list is
// present. Bracket matching is comma/brace/quote aware via splitTopCommas.
var rePartsInline = regexp.MustCompile(`^(\s*)parts:\s*\[(.*)\]\s*$`)

func ExpandInlineParts(src string) string {
	// Fixed-point: first a flow-form GROUP item (`- { … parts: [ … ] }`) is
	// expanded to block form (which puts its `parts:` on its own line), then that
	// own-line inline list is expanded to block items. Iterating handles both in
	// either order until stable.
	for {
		next := expandInlinePartsOnce(src)
		if next == src {
			return src
		}
		src = next
	}
}

func expandInlinePartsOnce(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		t := strings.TrimSpace(l)
		// A single-line flow item carrying a nested parts list — expand the whole
		// item to block form so the `parts:` lands on its own line.
		if strings.HasPrefix(t, "- {") && strings.Contains(t, "parts:") && strings.Contains(t, "[") {
			if exp := expandFlowItem(l); exp != nil {
				out = append(out, exp...)
				continue
			}
		}
		// An own-line inline parts list — expand to block items (kept as flow maps,
		// one per line, so they're individually addressable).
		if m := rePartsInline.FindStringSubmatch(l); m != nil {
			out = append(out, m[1]+"parts:")
			for _, it := range splitTopCommas(strings.TrimSpace(m[2])) {
				if it = strings.TrimSpace(it); it != "" {
					out = append(out, m[1]+"  - "+it)
				}
			}
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func MovePart(src, id, targetParentID string) (string, bool) {
	if id == targetParentID {
		return src, false
	}
	lines := strings.Split(src, "\n")
	s, e, ok := findPartItemRange(src, id)
	if !ok {
		return src, false
	}
	item := stripInlineOffset(strings.Join(lines[s:e], "\n"))
	srcIndent := indentOf(lines[s])

	// Remove the item from its current location first.
	without := append(append([]string{}, lines[:s]...), lines[e:]...)
	// If that emptied the source GROUP's parts: block, drop the now-dangling
	// `parts:` key (cosmetic, and it can mis-indent compact-style re-inserts).
	without = dropEmptyGroupParts(without, srcIndent)

	// Find the destination parts: block and the indent its items sit at.
	partsLine, partsIndent := -1, 0
	if targetParentID == "" {
		// scene root = the shallowest parts: key
		re := regexp.MustCompile(`^( *)parts:\s*$`)
		for i, l := range without {
			if m := re.FindStringSubmatch(l); m != nil {
				if partsLine < 0 || len(m[1]) < partsIndent {
					partsLine, partsIndent = i, len(m[1])
				}
			}
		}
	} else {
		// the target group's OWN nested parts: (first parts: after its id line,
		// before the next line at ≤ the group's indent)
		destinationSource := strings.Join(without, "\n")
		gLine := FindPartIDLine(destinationSource, targetParentID)
		gStart, gEnd, found := findPartItemRange(destinationSource, targetParentID)
		if gLine < 0 || !found {
			return src, false
		}
		gIndent := indentOf(without[gStart])
		reParts := regexp.MustCompile(`^( *)parts:\s*$`)
		for i := gStart + 1; i < gEnd; i++ {
			if strings.TrimSpace(without[i]) == "" {
				continue
			}
			if m := reParts.FindStringSubmatch(without[i]); m != nil && len(m[1]) == gIndent+2 {
				partsLine, partsIndent = i, len(m[1])
				break
			}
		}
		if partsLine < 0 {
			// seed a parts: block right after the group's id line
			partsIndent = gIndent + 2
			header := strings.Repeat(" ", partsIndent) + "parts:"
			if exp := expandFlowItem(without[gLine]); exp != nil {
				// Single-line flow group `- { … }` — expand to block form first so
				// the block `parts:` is valid under it (otherwise it gets slammed
				// under a closed flow scalar, producing unparseable YAML).
				block := append(append([]string{}, exp...), header)
				res := append([]string{}, without[:gLine]...)
				res = append(res, block...)
				res = append(res, without[gLine+1:]...)
				without = res
				partsLine = gLine + len(exp)
			} else {
				without = append(without[:gLine+1], append([]string{header}, without[gLine+1:]...)...)
				partsLine = gLine + 1
			}
		}
	}
	if partsLine < 0 {
		return src, false
	}

	// Insert as the FIRST child, right after the parts: header — robust against
	// the block's deeper nested items (scanning for "the last sibling" would be
	// pulled into a grandchild's indent). Arrangement within parts: is order-
	// independent; the new parent's layout places it.
	itemIndent := partsIndent + 2
	end := partsLine + 1

	// Re-indent the extracted item lines by the indent delta.
	delta := itemIndent - srcIndent
	reindented := make([]string, 0)
	for _, l := range strings.Split(item, "\n") {
		if strings.TrimSpace(l) == "" {
			reindented = append(reindented, l)
			continue
		}
		if delta >= 0 {
			reindented = append(reindented, strings.Repeat(" ", delta)+l)
		} else {
			trim := -delta
			if trim > len(l)-len(strings.TrimLeft(l, " ")) {
				trim = len(l) - len(strings.TrimLeft(l, " "))
			}
			reindented = append(reindented, l[trim:])
		}
	}

	out := append(append(append([]string{}, without[:end]...), reindented...), without[end:]...)
	return strings.Join(out, "\n"), true
}

// FreezeGroupLayoutText drops the `layout:` from a single named group's block
// (inline or block form) so its children render from explicit offsets once one
// is dragged. Other groups and the scene root are untouched.
//
// Only the layout: key that belongs DIRECTLY to the named group is removed —
// layout: keys of nested child groups are preserved so their own internal
// arrangements are not disrupted.
func FreezeGroupLayoutText(src, groupID string) string {
	lines := strings.Split(src, "\n")
	gLine := FindPartIDLine(src, groupID)
	if gLine < 0 {
		return src
	}
	gIndent := indentOf(lines[gLine])

	// directIndent is the indentation level of the named group's own keys
	// (shape:, label:, layout:, …). It's the first non-empty line after gLine
	// that is indented more than gLine.
	directIndent := -1
	for i := gLine + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		ind := indentOf(lines[i])
		if ind <= gIndent {
			break // end of block before finding any children
		}
		directIndent = ind
		break
	}
	if directIndent < 0 {
		return src // empty block
	}

	end := len(lines)
	for i := gLine + 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		if indentOf(lines[i]) <= gIndent {
			end = i
			break
		}
	}
	layoutRe := regexp.MustCompile(`^ *layout:`)
	out := append([]string{}, lines[:gLine+1]...)
	for i := gLine + 1; i < end; i++ {
		if layoutRe.MatchString(lines[i]) && indentOf(lines[i]) == directIndent {
			// This layout: belongs to the named group itself — drop it.
			if strings.Contains(lines[i], "{") {
				continue // inline → drop the one line
			}
			ki := indentOf(lines[i]) // block → drop line + its deeper body
			j := i + 1
			for j < end && (strings.TrimSpace(lines[j]) == "" || indentOf(lines[j]) > ki) {
				j++
			}
			i = j - 1
			continue
		}
		out = append(out, lines[i])
	}
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// RenamePart renames a part's id EVERYWHERE: its own structural `id:` key and
// every reference to it — connector from/to (preserving an "id.anchor" suffix),
// place rightOf/leftOf/inFrontOf/behind/above, and annotation anchor. References
// are matched only in their key context (and as a whole token, via the trailing
// delimiter) so a label or other text that merely contains the id is untouched.
// Without this a rename stranded every reference, silently breaking the render.
func RenamePart(src, oldID, newID string) string {
	q := regexp.QuoteMeta(oldID)
	// Quote the new id if it would otherwise re-decode as a non-string (e.g.
	// "null"/booleans) — a bare `id: null` silently became an empty id.
	nv := yamlScalar(newID)
	// The part's own id key — at line-start indent, or after a -, {, or , in
	// flow form. Anchored by a trailing delimiter so "web" never matches inside
	// "webNEW".
	idKey := regexp.MustCompile(`(?m)((?:^\s*|[-{,]\s*)id:\s*)"?` + q + `"?(\s*(?:[,}\n]|$))`)
	src = idKey.ReplaceAllString(src, `${1}`+nv+`${2}`)
	// References, with an optional ".anchor" OR "~N" (stack instance) suffix
	// preserved — both forms must be rewritten or they dangle after the rename.
	refKey := regexp.MustCompile(`((?:from|to|rightOf|leftOf|inFrontOf|behind|above|anchor):\s*)"?` + q + `((?:\.[A-Za-z0-9_-]+)|(?:~[0-9]+))?"?(\s*(?:[,}\n]|$))`)
	src = refKey.ReplaceAllString(src, `${1}`+nv+`${2}${3}`)
	return src
}

// DuplicatePart clones a node's block under a fresh id, placed at (ox,oy).
func DuplicatePart(src, id string, ox, oy float64) (string, bool) {
	lines := strings.Split(src, "\n")
	s, e, ok := findPartItemRange(src, id)
	if !ok {
		return src, false
	}
	newID := uniquePartID(src, id+"_copy")
	clone := append([]string{}, lines[s:e]...)
	cloneStr := strings.Join(clone, "\n")
	// Rename only the item's own id key — the FIRST structural `id:` occurrence.
	// The anchor (line-start indent, or after `-`/`{`/`,`) keeps a matching value
	// inside a quoted string (e.g. label: "user id: a") from being rewritten,
	// which produced unparseable YAML.
	idRe := regexp.MustCompile(`(?m)(^\s*|[-{,]\s*)id:\s*"?` + regexp.QuoteMeta(id) + `"?`)
	renamed := false
	cloneStr = idRe.ReplaceAllStringFunc(cloneStr, func(m string) string {
		if renamed {
			return m
		}
		renamed = true
		prefix := m[:strings.Index(m, "id:")]
		return prefix + "id: " + newID
	})
	out := strings.Join(append(append(append([]string{}, lines[:e]...), strings.Split(cloneStr, "\n")...), lines[e:]...), "\n")
	out, _ = UpsertInlineKey(out, FindPartIDLine(out, newID), "offset", ox, oy, 0)
	return out, true
}

// AddConnector appends a new connector (from → to, orthogonal routing) to the
// first connectors: block found in the scene node. If no connectors: key exists
// yet, one is inserted just before the first `parts:` key at the same indent.
//
// fromAnchor and toAnchor are optional anchor names (e.g. "right", "left").
// When non-empty they are appended to the id with a dot: "partID.anchorName".
// Empty strings produce the bare id, preserving the original behaviour.
func AddConnector(src, from, to, fromAnchor, toAnchor string) (string, bool) {
	if fromAnchor != "" {
		from = from + "." + fromAnchor
	}
	if toAnchor != "" {
		to = to + "." + toAnchor
	}
	lines := strings.Split(src, "\n")

	// Find the first `connectors:` block and its indent.
	connLine, connIndent := -1, 0
	reConn := regexp.MustCompile(`^( *)connectors:\s*$`)
	for i, l := range lines {
		if m := reConn.FindStringSubmatch(l); m != nil {
			connLine, connIndent = i, len(m[1])
			break
		}
	}

	itemLine := -1 // line AFTER which we insert the new item
	var itemIndent string

	if connLine >= 0 {
		// Find the block end + the existing items' column (handles items at the
		// same column as the key — see seqBlockEnd). Aligning the new item with
		// that column avoids mixed-indent, unparseable YAML.
		ind, end := seqBlockEnd(lines, connLine, connIndent)
		itemIndent = strings.Repeat(" ", ind)
		itemLine = end - 1
	} else {
		// No connectors: key yet — find the shallowest `parts:` key and insert
		// a connectors: block just before it (same indent level).
		reParts := regexp.MustCompile(`^( *)parts:\s*$`)
		partsLine, partsIndent := -1, 1<<30
		for i, l := range lines {
			if m := reParts.FindStringSubmatch(l); m != nil {
				if len(m[1]) < partsIndent {
					partsLine, partsIndent = i, len(m[1])
				}
			}
		}
		if partsLine < 0 {
			return src, false
		}
		connIndent = partsIndent
		itemIndent = strings.Repeat(" ", connIndent+2)
		header := strings.Repeat(" ", connIndent) + "connectors:"
		newLines := append(append([]string{}, lines[:partsLine]...),
			header,
		)
		newLines = append(newLines, lines[partsLine:]...)
		lines = newLines
		itemLine = partsLine // insert after the new connectors: header
	}

	routing := "orthogonal"
	if strings.HasPrefix(fromAnchor, "port-") || strings.HasPrefix(toAnchor, "port-") {
		routing = "orthogonal2"
	}
	nl := itemIndent + fmt.Sprintf(`- { from: %s, to: %s, routing: %s }`, yamlScalar(from), yamlScalar(to), routing)
	out := append(append(append([]string{}, lines[:itemLine+1]...), nl), lines[itemLine+1:]...)
	return strings.Join(out, "\n"), true
}

func uniquePartID(src, base string) string {
	id := base
	for n := 2; ; n++ {
		if _, _, ok := findPartItemRange(src, id); !ok {
			return id
		}
		id = fmt.Sprintf("%s%d", base, n)
	}
}

func mustParse(src string) map[string]interface{} {
	var m map[string]interface{}
	_ = yaml.Unmarshal([]byte(src), &m)
	return m
}
