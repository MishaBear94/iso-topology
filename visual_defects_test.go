package isotopo

import "testing"

// Two parallel semantic edges take different physical routes. Moving the
// obstacle transfers the tunnel from one edge to the other: total counts are
// unchanged, but accepting it would damage a previously clear connection.
func TestVisualDefectsPreserveParallelEdgeIdentity(t *testing.T) {
	edges := []planEdge{
		{ci: 4, from: planRect{id: "a"}, to: planRect{id: "b"}, pts: [][2]float64{{0, 10}, {100, 10}}},
		{ci: 7, from: planRect{id: "a"}, to: planRect{id: "b"}, pts: [][2]float64{{0, 50}, {100, 50}}},
	}
	obstacle := planRect{id: "obstacle", x: 40, y: 0, w: 20, d: 20, h: 80}
	before, after := visualDefects{}, visualDefects{}
	b := evalGeomWithDefects([]planRect{obstacle}, edges, before)
	obstacle.y = 40
	a := evalGeomWithDefects([]planRect{obstacle}, edges, after)
	if b.EdgesThroughNodes != 1 || a.EdgesThroughNodes != 1 {
		t.Fatalf("fixture must preserve one tunnel: before %d, after %d", b.EdgesThroughNodes, a.EdgesThroughNodes)
	}
	if before[visualDefect{kind: "tunnel", edge: 4, a: "obstacle"}] != 1 || after[visualDefect{kind: "tunnel", edge: 7, a: "obstacle"}] != 1 {
		t.Fatalf("parallel edges lost their identities: before %v, after %v", before, after)
	}
	if after.noWorseThan(before) {
		t.Fatal("a new tunnel on another parallel edge must be rejected")
	}
	if !before.noWorseThan(before) || !(visualDefects{}).noWorseThan(before) {
		t.Fatal("unchanged or cleared defects must remain admissible")
	}
}

func TestVisualDefectsPreserveCrossingPairIdentity(t *testing.T) {
	edges := []planEdge{
		{ci: 0, from: planRect{id: "a"}, to: planRect{id: "b"}, pts: [][2]float64{{0, 10}, {100, 10}}},
		{ci: 1, from: planRect{id: "c"}, to: planRect{id: "d"}, pts: [][2]float64{{40, 0}, {40, 20}}},
		{ci: 2, from: planRect{id: "e"}, to: planRect{id: "f"}, pts: [][2]float64{{60, 40}, {60, 60}}},
	}
	before, after := visualDefects{}, visualDefects{}
	b := evalGeomWithDefects(nil, edges, before)
	edges[0].pts = [][2]float64{{0, 50}, {100, 50}}
	a := evalGeomWithDefects(nil, edges, after)
	if b.Crossings != 1 || a.Crossings != 1 {
		t.Fatalf("fixture must preserve one crossing: before %d, after %d", b.Crossings, a.Crossings)
	}
	if after.noWorseThan(before) {
		t.Fatal("clearing one crossing cannot pay for a different crossing")
	}
}
