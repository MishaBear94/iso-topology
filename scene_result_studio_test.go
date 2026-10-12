package isotopo

import (
	"github.com/dop251/goja"
	"testing"
)

func TestStudioScreenLabelRecordsAndLiveTransform(t *testing.T) {
	vm := goja.New()
	script := `var window={},screenLabelCache=null,srcEl={value:'source'},bcrCalls=0,queryCalls=0;
 var matrix={a:2,b:.25,c:-.1,d:1.5,e:100,f:50};
 var groups=[{getAttribute:()=> 'node',getScreenCTM:()=>matrix,getBoundingClientRect:()=>{bcrCalls++;return {left:900,top:900,right:950,bottom:950,width:50,height:50};}}];
 var svg={querySelectorAll:()=>{queryCalls++;return groups;}};
 var records=[{part_id:'node',text_metrics:'shaped-ink',bounds:{x:10,y:20,width:40,height:30}}];
 ` + studioFunctions(t, "screenLabelRegions", "updateModel")
	if _, err := vm.RunString(script); err != nil {
		t.Fatal(err)
	}
	checks := []struct{ name, code string }{
		{"same response", `updateModel({model:[],routes:[],screenLabels:records});screenLabelRegions('node',svg)[0][0][0]===118&&bcrCalls===0`},
		{"current group transform", `matrix.e+=25;matrix.f-=10;var poly=screenLabelRegions('node',svg)[0];poly[0][0]===143&&poly[0][1]===72.5&&bcrCalls===0&&queryCalls===1`},
		{"new SVG invalidates element cache", `var svg2={querySelectorAll:()=>{queryCalls++;return groups;}};screenLabelRegions('node',svg2).length===1&&queryCalls===2`},
		{"unknown font keeps visual envelope", `updateModel({screenLabels:[{...records[0],text_metrics:'estimated-advance'}]});screenLabelRegions('node',svg)[0][0][0]===900&&bcrCalls===1`},
		{"old server clears prior records", `updateModel({model:[],routes:[]});window.screenLabels===null&&screenLabelRegions('node',svg)[0][0][0]===900&&bcrCalls===2`},
		{"identity mismatch cannot reassign", `updateModel({screenLabels:[{...records[0],part_id:'other'}]});screenLabelRegions('other',svg).length===0&&screenLabelRegions('node',svg)[0][0][0]===900`},
		{"empty layer authoritative", `groups=[];updateModel({screenLabels:[]});screenLabelRegions('node',svg).length===0`},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			r, err := vm.RunString(tc.code)
			if err != nil || !r.ToBoolean() {
				t.Fatalf("%s: %v %v", tc.name, r, err)
			}
		})
	}
}
