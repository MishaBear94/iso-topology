package isotopo

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// orthogonalThread3D preserves every authored corner and inserts axis-aligned
// bridges where a moved endpoint/corner no longer lines up. Unlike the legacy
// XY thread, a height change never gets folded into an oblique horizontal leg.
func orthogonalThread3D(points [][3]float64) [][3]float64 {
	if len(points) == 0 {
		return nil
	}
	out := [][3]float64{points[0]}
	for _, p := range points[1:] {
		a := out[len(out)-1]
		for axis := 0; axis < 3; axis++ {
			if math.Abs(p[axis]-a[axis]) > 1e-7 {
				a[axis] = p[axis]
				out = append(out, a)
			}
		}
		out[len(out)-1] = p
	}
	return out
}

func routeAxis(a, b [3]float64) int {
	axis := -1
	for k := 0; k < 3; k++ {
		if math.Abs(b[k]-a[k]) > 1e-5 {
			if axis != -1 {
				return -1
			}
			axis = k
		}
	}
	return axis
}

// moveRouteSegment keeps endpoint heights and identities. Horizontal legs
// move perpendicular to themselves; vertical risers move on the XY plane.
// Preserve short terminal leads so a handle move cannot detach a port.
func moveRouteSegment(points [][3]float64, segment int, dx, dy float64) ([][3]float64, error) {
	if segment < 0 || len(points) < 2 || segment >= len(points)-1 {
		return nil, fmt.Errorf("segment out of range")
	}
	axis := routeAxis(points[segment], points[segment+1])
	if axis < 0 {
		return nil, fmt.Errorf("segment is not axis aligned")
	}
	delta := [3]float64{dx, dy, 0}
	if axis == 0 {
		delta[0] = 0
	}
	if axis == 1 {
		delta[1] = 0
	}
	if math.Abs(delta[0])+math.Abs(delta[1]) < 1e-7 {
		return append([][3]float64(nil), points...), nil
	}
	shift := func(p [3]float64) [3]float64 {
		for k := range p {
			p[k] += delta[k]
		}
		return p
	}
	lead := func(a, b [3]float64) [3]float64 {
		length := math.Sqrt(math.Pow(b[0]-a[0], 2) + math.Pow(b[1]-a[1], 2) + math.Pow(b[2]-a[2], 2))
		p := a
		if length > 0 {
			for k := range p {
				p[k] += math.Min(24, length/3) * (b[k] - a[k]) / length
			}
		}
		return p
	}
	out := append([][3]float64(nil), points[:segment]...)
	a, b := points[segment], points[segment+1]
	if segment == 0 {
		l := lead(a, b)
		out = append(out, a, l, shift(l))
	} else {
		out = append(out, shift(a))
	}
	if segment+1 == len(points)-1 {
		l := lead(b, a)
		out = append(out, shift(l), l, b)
	} else {
		out = append(out, shift(b))
		out = append(out, points[segment+2:]...)
	}
	return orthogonalThread3D(out), nil
}

func applyMoveSegment(format string, src []byte, op EditOp) ([]byte, error) {
	if format == "d2" {
		return src, fmt.Errorf("move-segment requires a YAML or JSON composite")
	}
	for _, v := range []float64{op.DWX, op.DWY} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > maxGeomDim {
			return src, fmt.Errorf("invalid segment delta")
		}
	}
	doc, err := LoadInput(context.Background(), format, src, LayoutDagre)
	if err != nil {
		return src, err
	}
	for _, issue := range Validate(doc) {
		if issue.Severity == SeverityError {
			return src, fmt.Errorf("cannot edit invalid source: %s", issue.Message)
		}
	}
	scene := doc.Scene()
	if scene == nil || op.CI < 0 || op.CI >= len(scene.Connectors) {
		return src, fmt.Errorf("connector not found")
	}
	c := scene.Connectors[op.CI]
	if c == nil || (c.Routing != "orthogonal" && c.Routing != "orthogonal2") {
		return src, fmt.Errorf("segment editing requires an orthogonal connector")
	}
	cv := Canvas{}
	if doc.Canvas != nil {
		cv = *doc.Canvas
	}
	cv.Projection = "iso"
	_, routes := RenderWithRouteInfo(scene, doc.Theme, &cv, nil)
	var route *ConnectorRoute
	for i := range routes {
		if routes[i].Index == op.CI {
			route = &routes[i]
			break
		}
	}
	if route == nil {
		return src, fmt.Errorf("connector has no editable route")
	}
	points, err := moveRouteSegment(route.World3D, op.Segment, op.DWX, op.DWY)
	if err != nil {
		return src, err
	}
	if len(points) == len(route.World3D) {
		equal := true
		for i := range points {
			equal = equal && points[i] == route.World3D[i]
		}
		if equal {
			return src, nil
		}
	}
	if len(points)-2 > 512 {
		return src, fmt.Errorf("edited route exceeds 512 corners")
	}
	var waypoints []WorldPoint
	for _, p := range points[1 : len(points)-1] {
		for _, v := range p {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > maxGeomDim {
				return src, fmt.Errorf("edited route exceeds coordinate bounds")
			}
		}
		waypoints = append(waypoints, WorldPoint{WX: p[0], WY: p[1], WZ: p[2]})
	}
	sceneID := ""
	for id, n := range doc.Nodes {
		if n == scene {
			sceneID = id
			break
		}
	}
	if sceneID == "" {
		return src, fmt.Errorf("segment editing requires an explicit scene node")
	}
	return writeConnectorWaypoints3D(format, src, sceneID, op.CI, waypoints)
}

// Locate the selected scene/connector through YAML nodes, not a global count
// of dash-prefixed lines (nested waypoint lists contain dashes themselves).
func mappingValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func writeConnectorWaypoints3D(format string, src []byte, scene string, index int, points []WorldPoint) ([]byte, error) {
	if index < 0 {
		return src, fmt.Errorf("connector index must be nonnegative")
	}
	if format == "json" {
		var d map[string]json.RawMessage
		if err := json.Unmarshal(src, &d); err != nil {
			return src, err
		}
		var nodes map[string]json.RawMessage
		if err := json.Unmarshal(d["nodes"], &nodes); err != nil {
			return src, err
		}
		var node map[string]json.RawMessage
		if err := json.Unmarshal(nodes[scene], &node); err != nil {
			return src, err
		}
		var cs []map[string]json.RawMessage
		if err := json.Unmarshal(node["connectors"], &cs); err != nil || index >= len(cs) {
			return src, fmt.Errorf("connector not found in JSON")
		}
		cs[index]["waypoints3d"], _ = json.Marshal(points)
		node["connectors"], _ = json.Marshal(cs)
		nodes[scene], _ = json.Marshal(node)
		d["nodes"], _ = json.Marshal(nodes)
		out, err := json.MarshalIndent(d, "", "  ")
		return out, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return src, err
	}
	if len(root.Content) == 0 {
		return src, fmt.Errorf("empty source")
	}
	cs := mappingValue(mappingValue(mappingValue(root.Content[0], "nodes"), scene), "connectors")
	if cs == nil || cs.Kind != yaml.SequenceNode || index >= len(cs.Content) {
		return src, fmt.Errorf("connector not found in source")
	}
	n := cs.Content[index]
	if n.Kind != yaml.MappingNode || n.Anchor != "" {
		return src, fmt.Errorf("aliased connector cannot be edited independently")
	}
	var value yaml.Node
	if err := value.Encode(points); err != nil {
		return src, err
	}
	value.Style = yaml.FlowStyle
	for _, p := range value.Content {
		p.Style = yaml.FlowStyle
	}
	old := mappingValue(n, "waypoints3d")
	// Anchors can have consumers outside this connector; editing their source
	// would also change those consumers. Decline rather than mutate shared data.
	var shared func(*yaml.Node) bool
	shared = func(v *yaml.Node) bool {
		if v.Anchor != "" || v.Kind == yaml.AliasNode {
			return true
		}
		for _, child := range v.Content {
			if shared(child) {
				return true
			}
		}
		return false
	}
	if shared(n) {
		return src, fmt.Errorf("aliased connector cannot be edited independently")
	}
	lines := strings.Split(string(src), "\n")
	if n.Style&yaml.FlowStyle != 0 {
		if old != nil {
			value.HeadComment, value.LineComment, value.FootComment = old.HeadComment, old.LineComment, old.FootComment
			var note func(*yaml.Node)
			note = func(v *yaml.Node) {
				for _, ch := range v.Content {
					for _, c := range []string{ch.HeadComment, ch.LineComment, ch.FootComment} {
						if c != "" {
							value.HeadComment += "\n" + c
						}
					}
					note(ch)
				}
			}
			note(old)
			*old = value
		} else {
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "waypoints3d"}, &value)
		}
		// Replace exactly one {...} byte range, including when connectors itself
		// is a flow sequence. YAML columns count runes, source offsets count bytes.
		start := 0
		for _, line := range lines[:n.Line-1] {
			start += len(line) + 1
		}
		start += len(string([]rune(lines[n.Line-1])[:n.Column-1]))
		end, err := flowMappingEnd(string(src), start)
		if err != nil {
			return src, err
		}
		n.HeadComment, n.LineComment, n.FootComment = "", "", ""
		encoded, err := yaml.Marshal(n)
		if err != nil {
			return src, err
		}
		return append(append(append([]byte{}, src[:start]...), []byte(strings.TrimSpace(string(encoded)))...), src[end:]...), nil
	}
	indent := n.Column - 1
	encoded, err := yaml.Marshal(&value)
	if err != nil {
		return src, err
	}
	field := "waypoints3d: " + strings.TrimSpace(string(encoded))
	if old == nil {
		// Insert before the first key, so a multiline first value stays intact.
		i := n.Line - 1
		col := len(string([]rune(lines[i])[:n.Column-1]))
		first := lines[i][:col] + field
		rest := strings.Repeat(" ", indent) + lines[i][col:]
		lines = append(append(append([]string{}, lines[:i]...), first, rest), lines[i+1:]...)
	} else {
		var key *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == "waypoints3d" {
				key = n.Content[i]
				break
			}
		}
		first := key.Line - 1
		end := first + 1
		for end < len(lines) {
			trim := strings.TrimSpace(lines[end])
			ind := len(lines[end]) - len(strings.TrimLeft(lines[end], " "))
			// Indentationless block sequences belong to this value; a following
			// connector is two columns farther left and still terminates the range.
			if trim != "" && !strings.HasPrefix(trim, "#") && (ind < indent || (ind == indent && !strings.HasPrefix(trim, "- "))) {
				break
			}
			end++
		}
		// Comments attached to old coordinates are retained as standalone comments
		// in the rewritten field. Sibling comments outside the span stay byte exact.
		comments := map[int][]string{}
		var collect func(*yaml.Node)
		collect = func(v *yaml.Node) {
			for _, c := range []string{v.HeadComment, v.LineComment, v.FootComment} {
				if c != "" {
					comments[v.Line] = append(comments[v.Line], strings.Split(c, "\n")...)
				}
			}
			for _, ch := range v.Content {
				collect(ch)
			}
		}
		collect(key)
		collect(old)
		var positions []int
		for l := range comments {
			positions = append(positions, l)
		}
		sort.Ints(positions)
		col := len(string([]rune(lines[first])[:key.Column-1]))
		repl := []string{lines[first][:col] + field}
		seen := map[string]bool{}
		for _, l := range positions {
			for _, c := range comments[l] {
				if !seen[c] {
					repl = append(repl, strings.Repeat(" ", indent)+c)
					seen[c] = true
				}
			}
		}
		for _, line := range lines[first+1 : end] {
			if strings.HasPrefix(strings.TrimSpace(line), "#") && !seen[strings.TrimSpace(line)] {
				repl = append(repl, line)
				seen[strings.TrimSpace(line)] = true
			}
		}
		lines = append(append(append([]string{}, lines[:first]...), repl...), lines[end:]...)
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// Scan a YAML flow mapping without interpreting braces inside quoted strings
// or comments. start is the mapping's opening brace (from yaml.Node.Column).
func flowMappingEnd(src string, start int) (int, error) {
	depth := 0
	var quote byte
	comment := false
	for i := start; i < len(src); i++ {
		c := src[i]
		if comment {
			if c == '\n' {
				comment = false
			}
			continue
		}
		if quote != 0 {
			if quote == '"' && c == '\\' {
				i++
				continue
			}
			if c == quote {
				if quote == '\'' && i+1 < len(src) && src[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if c == '#' && (i == 0 || src[i-1] == ' ' || src[i-1] == '\n') {
			comment = true
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '{' {
			depth++
		}
		if c == '}' {
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		}
	}
	return start, fmt.Errorf("unterminated connector mapping")
}
