package isotopo

import (
	"math"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
)

// Execute the actual embedded browser functions; stubs provide only the DOM
// boundary. The browser evidence separately exercises native mouse and CTM.
func studioFunctions(t *testing.T, names ...string) string {
	t.Helper()
	script := strings.NewReplacer("{{LANGQ}}", `"yaml"`, "{{PATHQ}}", `"test.yaml"`).Replace(studioJS)
	program, err := parser.ParseFile(nil, "studio.js", script, 0)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[string]bool{}
	for _, n := range names {
		wanted[n] = true
	}
	out := ""
	for _, statement := range program.Body {
		f, ok := statement.(*ast.FunctionDeclaration)
		if !ok || f.Function.Name == nil {
			continue
		}
		n := f.Function.Name.Name.String()
		if !wanted[n] {
			continue
		}
		source := script[int(f.Idx0())-1 : int(f.Idx1())-1]
		if f.Function.Async && !strings.HasPrefix(source, "async ") {
			source = "async " + source
		}
		out += source + "\n"
		delete(wanted, n)
	}
	if len(wanted) != 0 {
		t.Fatalf("missing browser functions: %v", wanted)
	}
	return out
}

func TestStudioProjectionAndDragTransform(t *testing.T) {
	vm := goja.New()
	script := `var mode='iso', panX=0,panY=0,scale=1,fitScale=1;const C30=Math.sqrt(3)/2,S30=.5;
 var svg={dataset:{sceneTx:'20.25',sceneTy:'30.5'},getScreenCTM:()=>({a:2*scale*fitScale,b:0,c:0,d:2*scale*fitScale,e:100+panX,f:50+panY})};
 var zoomer={querySelector:()=>{svg.dataset.projection=mode;return svg;}};
 function clientToSvgUser(x,y){return [(x-100)/2,(y-50)/2];}
 function apply(){}
 ` + studioFunctions(t, "sceneOrigin", "isTopView", "worldToSvgUser", "screenToWorldDelta", "dragUserDelta", "captureSceneFrame", "restoreSceneFrame")
	if _, err := vm.RunString(script); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"iso", "top"} {
		if err := vm.Set("mode", mode); err != nil {
			t.Fatal(err)
		}
		result, err := vm.RunString(`(()=>{const p=worldToSvgUser(40,20,0);return screenToWorldDelta(p[0]-20.25,p[1]-30.5);})()`)
		if err != nil {
			t.Fatal(err)
		}
		a := result.ToObject(vm)
		if math.Abs(a.Get("0").ToFloat()-40) > 1e-8 || math.Abs(a.Get("1").ToFloat()-20) > 1e-8 {
			t.Fatal("projection roundtrip", result)
		}
	}
	r, err := vm.RunString(`dragUserDelta({x:120,y:70},{clientX:160,clientY:100})`)
	if err != nil {
		t.Fatal(err)
	}
	a := r.ToObject(vm)
	if a.Get("0").ToFloat() != 20 || a.Get("1").ToFloat() != 15 {
		t.Fatal("drag must use full SVG CTM, not UI zoom", r)
	}
	r, err = vm.RunString(`var frame=captureSceneFrame();svg.dataset.sceneTx='45.25';svg.dataset.sceneTy='20.5';fitScale=.5;restoreSceneFrame(frame);[panX,panY,scale]`)
	if err != nil {
		t.Fatal(err)
	}
	a = r.ToObject(vm)
	if a.Get("0").ToFloat() != -50 || a.Get("1").ToFloat() != 20 || a.Get("2").ToFloat() != 2 {
		t.Fatal("viewBox change must preserve world origin and effective scale on screen", r)
	}
}

func TestStudioMoveFailureAndReparentHistory(t *testing.T) {
	vm := goja.New()
	script := `var serverOK=true,srcEl={value:'original'},undoStack=[],redoStack=[],calls=0,ok=false,result=null,transportError=false,valid=true;
 var LANG='yaml',ORIGINAL='original',DRAFTKEY='test';
 var document={getElementById:()=>null};var window={interactionModel:[{id:'leaf',parentId:'group'}]};
 var zoomer={innerHTML:''};var localStorage={setItem:()=>{}};
 function projQS(){return '';}
 function showIssues(){} function captureSceneFrame(){return null;} function restoreSceneFrame(){}
 function setDirty(){} function updateModel(){} function buildMap(){} function paint(){} function pinnedRange(){}
 function wireHover(){} function wireDrag(){} function adaptStage(){} function markRendered(){}
 async function fetch(){calls++;if(transportError)throw Error('offline');return {ok:ok,json:async()=>valid?({yaml:'moved',svg:'<svg/>'}):({yaml:'partial'})};}
	` + studioFunctions(t, "pushUndo", "commitMove", "reparentNode", "commitConnection")
	if _, err := vm.RunString(script); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []struct{ name, setup string }{
		{"HTTP rejection", `ok=false;transportError=false;valid=true;`},
		{"network failure", `ok=true;transportError=true;valid=true;`},
		{"incomplete response", `ok=true;transportError=false;valid=false;`},
	} {
		t.Run(failure.name, func(t *testing.T) {
			if _, err := vm.RunString(failure.setup + `undoStack=Array.from({length:100},(_,i)=>'history'+i);redoStack=['redo-a','redo-b'];var expectedHistory=JSON.stringify([undoStack,redoStack]);commitMove('node','leaf',10,20).then(v=>result=v)`); err != nil {
				t.Fatal(err)
			}
			r, err := vm.RunString(`result===false&&srcEl.value==='original'&&JSON.stringify([undoStack,redoStack])===expectedHistory`)
			if err != nil || !r.ToBoolean() {
				t.Fatal("failed move must preserve full undo and redo history", err)
			}
			if _, err := vm.RunString(`reparentNode('leaf','other')`); err != nil {
				t.Fatal(err)
			}
			r, err = vm.RunString(`srcEl.value==='original'&&JSON.stringify([undoStack,redoStack])===expectedHistory`)
			if err != nil || !r.ToBoolean() {
				t.Fatal("failed reparent must preserve full undo and redo history", err)
			}
			if _, err := vm.RunString(`commitConnection('a~2','port-right','b','port-left').then(v=>result=v)`); err != nil {
				t.Fatal(err)
			}
			r, err = vm.RunString(`result===false&&srcEl.value==='original'&&JSON.stringify([undoStack,redoStack])===expectedHistory`)
			if err != nil || !r.ToBoolean() {
				t.Fatal("failed connection must preserve full undo and redo history", err)
			}
		})
	}
	if _, err := vm.RunString(`ok=false;transportError=false;valid=true;undoStack=[];redoStack=[];`); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.RunString(`commitMove('node','leaf',10,20).then(v=>result=v)`); err != nil {
		t.Fatal(err)
	}
	if vm.Get("result").ToBoolean() || vm.Get("srcEl").ToObject(vm).Get("value").String() != "original" || vm.Get("undoStack").ToObject(vm).Get("length").ToInteger() != 0 {
		t.Fatal("rejected move must not alter source/history")
	}
	if _, err := vm.RunString(`ok=true;commitMove('node','leaf',10,20).then(v=>result=v)`); err != nil {
		t.Fatal(err)
	}
	if !vm.Get("result").ToBoolean() || vm.Get("undoStack").ToObject(vm).Get("length").ToInteger() != 1 {
		t.Fatal("successful move must create exactly one undo")
	}
	calls := vm.Get("calls").ToInteger()
	if _, err := vm.RunString(`reparentNode('leaf','group')`); err != nil {
		t.Fatal(err)
	}
	if vm.Get("calls").ToInteger() != calls || vm.Get("undoStack").ToObject(vm).Get("length").ToInteger() != 1 {
		t.Fatal("unchanged parent must be a no-op")
	}
	if _, err := vm.RunString(`reparentNode('leaf','other',false)`); err != nil {
		t.Fatal(err)
	}
	if vm.Get("calls").ToInteger() != calls+1 || vm.Get("undoStack").ToObject(vm).Get("length").ToInteger() != 1 {
		t.Fatal("reparent in the same gesture must share its undo")
	}
}

func TestStudioSegmentPreviewTransactions(t *testing.T) {
	vm := goja.New()
	script := `var srcEl={value:'original'},VIEWMODE='iso',edgeDrag=null,undoStack=[],redoStack=[],requests=[],installs=[],issues=[];
 var LANG='yaml',ORIGINAL='original',DRAFTKEY='test';var document={body:{style:{}}};
 var localStorage={setItem:()=>{}},AbortController=function(){this.signal={};this.abort=()=>{this.signal.aborted=true;}};
 function setTimeout(){return 1;}function clearTimeout(){}function setDirty(){}function updateModel(){}function markRendered(){}
 function showIssues(v){issues=v;}function L(v){return v;}
 function installSegmentSVG(svg){installs.push(svg);}
 function fetch(url,opts){return new Promise((resolve,reject)=>requests.push({url,opts,resolve,reject}));}
 function makeDrag(){edgeDrag={segmentEdit:true,ci:0,seg:{j:3},source:'original',projection:'iso',originalSvg:'original-svg',
 version:1,accepted:-1,desired:[20,-10],result:null,running:false,released:false,cancelled:false};return edgeDrag;}
 function reply(index,data,ok){requests[index].resolve({ok:ok!==false,json:async()=>data,text:async()=>'rejected'});}
 ` + studioFunctions(t, "pushUndo", "segmentDragCurrent", "cancelSegmentDrag", "finishSegmentDrag", "pumpSegmentPreview")
	if _, err := vm.RunString(script); err != nil {
		t.Fatal(err)
	}
	run := func(s string) {
		t.Helper()
		if _, err := vm.RunString(s); err != nil {
			t.Fatal(err)
		}
	}
	check := func(s string) {
		t.Helper()
		v, err := vm.RunString(s)
		if err != nil || !v.ToBoolean() {
			t.Fatal(s, err)
		}
	}
	run(`var d=makeDrag();pumpSegmentPreview(d);`)
	check(`requests.length===1&&requests[0].opts.body==='original'&&undoStack.length===0`)
	run(`d.version=2;d.desired=[30,-15];d.released=true;pumpSegmentPreview(d);`)
	check(`requests.length===1`)
	run(`reply(0,{yaml:'old',svg:'old-svg'});`)
	check(`requests.length===2&&installs.length===0&&srcEl.value==='original'`)
	run(`reply(1,{yaml:'latest',svg:'latest-svg'});`)
	check(`edgeDrag===null&&srcEl.value==='latest'&&undoStack.length===1&&undoStack[0]==='original'&&installs.every(s=>s==='latest-svg')`)
	for _, failure := range []string{"http", "transport", "partial", "cancel", "source", "projection", "noop"} {
		t.Run(failure, func(t *testing.T) {
			run(`srcEl.value='original';VIEWMODE='iso';requests=[];installs=[];issues=[];
    undoStack=Array.from({length:100},(_,i)=>'history'+i);redoStack=['redo-a','redo-b'];var history=JSON.stringify([undoStack,redoStack]);
    d=makeDrag();d.released=true;pumpSegmentPreview(d);`)
			switch failure {
			case "http":
				run(`reply(0,{yaml:'bad',svg:'bad-svg'},false);`)
			case "transport":
				run(`requests[0].reject(Error('offline'));`)
			case "partial":
				run(`reply(0,{yaml:'partial'});`)
			case "cancel":
				run(`cancelSegmentDrag();reply(0,{yaml:'late',svg:'late-svg'});`)
			case "source":
				run(`srcEl.value='newer';cancelSegmentDrag();reply(0,{yaml:'late',svg:'late-svg'});`)
			case "projection":
				run(`cancelSegmentDrag();VIEWMODE='top';reply(0,{yaml:'late',svg:'late-svg'});`)
			case "noop":
				run(`reply(0,{yaml:'original',svg:'original-svg'});`)
			}
			check(`JSON.stringify([undoStack,redoStack])===history&&edgeDrag===null&&!installs.includes('late-svg')&&document.body.style.cursor===''`)
			if failure == "source" {
				check(`srcEl.value==='newer'`)
			} else {
				check(`srcEl.value==='original'`)
			}
		})
	}
}

func TestStudioSegmentSelectionRejectsPointerAmbiguity(t *testing.T) {
	vm := goja.New()
	script := `var matrix={a:1,b:0,c:0,d:1,e:0,f:0},p={getScreenCTM:()=>matrix};
 function DOMPoint(x,y){this.x=x;this.y=y;this.matrixTransform=m=>({x:m.a*x+m.c*y+m.e,y:m.b*x+m.d*y+m.f});}
 var eligible=[],visible=[];
 function editableRouteSegments(){return eligible;}
 function visibleRouteSegments(){return visible;}
 function point(x,y){return new DOMPoint(x,y).matrixTransform(matrix);}
 ` + studioFunctions(t, "distToSeg", "segmentHitCandidates", "nearestSegmentCandidate", "nearestEditableSegment")
	if _, err := vm.RunString(script); err != nil {
		t.Fatal(err)
	}
	for _, matrix := range []string{`{a:.25,b:0,c:0,d:.25,e:70,f:30}`, `{a:1,b:0,c:0,d:1,e:-10,f:80}`, `{a:4,b:0,c:0,d:4,e:20,f:10}`, `{a:1.2,b:.4,c:-.3,d:.9,e:50,f:30}`} {
		_, err := vm.RunString(`matrix=` + matrix + `;
 eligible=[{segment:0,j:0,sx:0,sy:0,ex:100,ey:100},{segment:2,j:2,sx:0,sy:100,ex:100,ey:0}];visible=eligible.slice();
 var q=point(50,50);if(nearestEditableSegment(p,q.x,q.y)!==null)throw Error('crossing selected by array order');
 eligible.reverse();if(nearestEditableSegment(p,q.x,q.y)!==null)throw Error('reordered crossing selected');
 q=point(20,20);if(nearestEditableSegment(p,q.x,q.y)?.j!==0)throw Error('clear source not selectable');
 eligible=eligible.filter(s=>s.j===0);q=point(50,50);
 if(nearestEditableSegment(p,q.x,q.y)!==null)throw Error('noneditable visible leg ignored');
 visible=eligible.slice();if(nearestEditableSegment(p,q.x,q.y)?.j!==0)throw Error('hidden leg blocked clear source');
 `)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := vm.RunString(`
 eligible=[{segment:0,j:0,sx:0,sy:0,ex:100,ey:0},{segment:1,j:1,sx:0,sy:1,ex:100,ey:1}];visible=eligible.slice();
 matrix={a:.25,b:0,c:0,d:.25,e:0,f:0};q=point(50,0);
 if(nearestEditableSegment(p,q.x,q.y)!==null)throw Error('subpixel separation selected');
 matrix={a:4,b:0,c:0,d:4,e:0,f:0};q=point(50,0);
 if(nearestEditableSegment(p,q.x,q.y)?.j!==0)throw Error('clear zoomed separation rejected');
 q=point(50,.5);if(nearestEditableSegment(p,q.x,q.y)!==null)throw Error('equidistant parallel legs selected');
 `); err != nil {
		t.Fatal(err)
	}
}

func TestStudioVisibleEditIntervalsUseFinalCoverage(t *testing.T) {
	vm := goja.New()
	script := `var srcEl={value:'current'},topView=false;function isTopView(){return topView;}
 var route={index:0,world3d:[[0,0,0],[100,0,0],[100,0,80]],drawing:{kind:'polyline',points:[[0,0],[100,0],[100,80]],
 editable:[{segment:0,t0:0,t1:1,route_segment:0}],body_coverage:{spans:[
 {segment:0,t0:0,t1:.4,opaque_parts:[]},{segment:0,t0:.4,t1:.6,opaque_parts:['body']},{segment:0,t0:.6,t1:1,opaque_parts:[]},
 {segment:1,t0:0,t1:1,opaque_parts:[]}]}}};
 var window={connectorRouteSource:'current',connectorRoutes:[route]},p={getAttribute:()=> '0'};
 ` + studioFunctions(t, "connectorRouteForPath", "visibleRouteSegments", "editableRouteSegments")
	script += `
 var visible=visibleRouteSegments(p),edits=editableRouteSegments(p);
 if(visible.length!==3||edits.length!==2||edits[0].ex!==40||edits[1].sx!==60||edits.some(s=>s.segment!==0||s.j!==0))throw Error('coverage interval mismatch');
 if(!visible.some(s=>s.segment===1))throw Error('visible noneditable competitor omitted');
 route.drawing.spatial={mode:'depth',spans:[{segment:0,t0:0,t1:1,occluders:['front']},{segment:1,t0:0,t1:1,occluders:[]}]};
 if(editableRouteSegments(p).length||visibleRouteSegments(p).length!==1)throw Error('active depth spans not applied');
 route.drawing.spatial.mode='legacy';if(editableRouteSegments(p).length!==2)throw Error('legacy coverage not restored');
 srcEl.value='newer';if(visibleRouteSegments(p).length||editableRouteSegments(p).length)throw Error('stale source used');
 srcEl.value='current';topView=true;if(visibleRouteSegments(p).length)throw Error('iso metadata used for plan');
 `
	if _, err := vm.RunString(script); err != nil {
		t.Fatal(err)
	}
}
