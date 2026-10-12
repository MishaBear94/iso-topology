# Testing architecture

The edit engine's bugs lived at the **intersection of independent axes** —
operation × YAML form × container kind × value class × reference topology — not
in any single feature. So the suite is organised around **axes and invariants**,
not hand-picked scenarios. Discover with fuzz; lock with the deterministic
layers; every layer asserts the **same** invariants.

## The axes (what varies)

Defined once in `editharness_test.go`:

- **operation** — move, reparent, set-field (label/style/shape/id), add, add-edge, delete, duplicate
- **YAML form** — flow vs block, tight vs spaced, inline `parts:[…]` vs block, same-column vs indented
- **container** — root, plain/offset group, autosize, layout row/col/grid, boundary, nested
- **value class** — normal, reserved word (`null`/`true`), special chars (`<>&"{},`), control chars, NaN/Inf/huge, empty, dotted, `~N`
- **reference topology** — connector / place / annotation refs, with `.anchor` and `~N` suffixes

## The layers

| Layer | File | Role |
|---|---|---|
| Unit | `yamledit/*_test.go` | text-surgery helpers vs adversarial inputs |
| Property / acceptance | `acceptance_position_test.go`, `bugfix_regression_test.go`, `injection_xss_test.go` | rich semantic invariants on controlled inputs |
| Matrix | `editmatrix_test.go` | the **op × container** cross-product, explicit |
| Cumulative session | `editsequence_test.go` | seeded random op-chains; floor invariants after every step |
| Golden | `samples/**/expected.svg` | rendering fidelity, byte-stable |
| Fuzz | `editfuzz_test.go` (`FuzzEditSession`) | coverage-guided discovery, reuses the harness |

## The invariants (the oracle)

The **floor invariants** (`checkFloorInvariants`) hold for any op on any doc and
never false-fail, so they run over random sequences and fuzz:

- **F1** no panic
- **F2** a successful op's output always re-parses (no silent corruption)
- **F3** a refused op (err) leaves the source unchanged
- **F4** no op introduces a duplicate part id (only `duplicate` adds an id)

Richer semantic invariants — position preservation, idempotency, inverse,
locality, reference-integrity, no-injection, resolver≡renderer — live in the
property/matrix layers where the input is controlled.

## Fuzz → ratchet

`FuzzEditSession` mutates `(seed, steps)` over the generator. **Every crasher is
minimized into a deterministic test in `bugfix_regression_test.go`** — the
deterministic suite only grows, fuzz only discovers.

## Release gate

Run before every version (must be all-green):

```sh
scripts/release-regression.sh          # ~60s fuzz
FUZZTIME=5m scripts/release-regression.sh   # deeper pre-release
```

It runs build, vet, the full suite (unit + golden + matrix + cumulative
session), the WASM build, and a bounded coverage-guided fuzz of the edit engine.

## Rendering and conservative repair

Rendering regressions need both geometric checks and consumer parity. The
permanent suites cover these boundaries:

| Boundary | Tests |
|---|---|
| Submitted paths, attachment identity, editable segments and hit regions | `connector_*_test.go`, `route_edit_test.go`, `interaction_regions_test.go` |
| Text ink, font resources, body occlusion and fallback behavior | `textmetrics/*_test.go`, `iso25d/*_test.go`, `connector_text*_test.go`, `screen_text_test.go` |
| Shared preview, report, interaction and CLI results | `scene_result*_test.go`, `scene_consistency_test.go`, `plan_scene_test.go`, `cmd/isotopo/scene_result_test.go` |
| Plan routing, lanes and crowding | `plan_route_content_test.go`, `plan_route_lanes_test.go`, `plan_route_crowding_test.go` |
| Isometric crowding, authored-route protection and rejected remote crossings | `iso_route_crowding_test.go` |
| Polygon clipping and area integration | `spatial_clip_parity_test.go`, `spatial_geometry_test.go`, `region_area_test.go` |
| Studio transforms, selection and asynchronous edit transactions | `studio_interaction_test.go`, `scene_result_studio_test.go` |

The clipping parity test compares coordinates bit for bit, polygon ownership,
degeneracy and remaining work budgets against the pre-optimization clipper. The
area tests also use an independent cell-occupancy oracle transformed into slanted
polygons. Performance changes must preserve diagnostic completeness and candidate
acceptance, as well as SVG output. Do not approve a shortcut solely because its
total conflict count decreases.

CI runs vet, dependency-manifest checks, native tests, a WASM build and selected
geometry tests in Node.js. The foreground-order test runs without a rasterizer:
a uniformly nearer surface must not acquire a reverse paint dependency from
floating-point clipping slivers. Pixel tests separately check antialiasing.

Run the committed benchmarks with allocations enabled:

```sh
go test . -run '^$' -bench 'Benchmark(IsoRouteCrowding|PainterIntersection)$' -benchmem -count=3
```

`preview`, `report`, `repair` and `dense-report` measure different entry points.
The dense fixture has 22 edges; it is a stress case with repeated routes, not a
representative product-wide latency percentile. Compare interleaved runs on the
same machine. These measurements do not establish browser frame rate.

For an actual WASM runtime check, install Node.js and use the Go distribution's
runner (in addition to a WASM build):

```sh
GOOS=js GOARCH=wasm go test \
  -exec="$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
  . ./iso25d ./textmetrics \
  -run 'TestSpatial|TestRegion|TestIsoRouteCrowding|TestPlanRoute|TestConnector|TestBuiltin' \
  -count=1
```

Native raster tests require `resvg`; the optional Chrome screenshot test needs
its documented browser setup. A skipped pixel test is not browser validation.
The JavaScript tests execute actual Studio functions with DOM boundary stubs;
they do not replace native pointer, zoom and font-rendering checks.

Current automatic repairs preserve explicit routes and reject unresolved
observations. The clearance observer's scope and exclusions are documented in
[the API reference](reference/api.md#isometric-route-clearance-and-conservative-repair).
General curve clearance, explicit bus/bundle intent, global port/channel
assignment, arbitrary font fallback and the experimental depth compositor remain
separate work. Dense diagnostics still need latency profiling; passing these
tests does not establish that every diagram is optimal or free of visual defects.
