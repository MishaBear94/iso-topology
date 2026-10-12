
"use strict";
const LANG={{LANGQ}}, PATH={{PATHQ}};
const FILENAME=PATH.split('/').pop();
const srcEl=document.getElementById('src'), hlEl=document.getElementById('hl'), gutEl=document.getElementById('gut');
let ORIGINAL=srcEl.value; const DRAFTKEY='isotopo-draft:'+PATH;
const staleEl=document.getElementById('stale');
let dirty=false;
// A divergent draft from a previous session, kept for one-click recovery. The
// page DEFAULTS to the disk version (ORIGINAL) — the draft is not auto-applied,
// so external edits to the file are visible on reload instead of being shadowed.
let pendingDraft=null;
function setDirty(d){
  dirty=d;
  document.getElementById('discard').hidden=!d;
  // any dirty-state change makes a leftover pending draft moot (a fresh edit
  // overwrote it; a revert/save resolved it) — drop the restore affordance.
  const r=document.getElementById('restore'); if(r) r.hidden=true;
}
function discardDraft(){
  try{localStorage.removeItem(DRAFTKEY);}catch(_){}
  if(location.hash.indexOf('#src=')===0){
    try{history.replaceState(null,'',location.pathname+location.search);}catch(_){}
  }
  pendingDraft=null;
  srcEl.value=ORIGINAL; setDirty(false);
  buildMap(); paint(null);
  if(serverOK) rerender();
}
// Recover the previous-session draft the page chose NOT to auto-apply.
function restoreDraft(){
  if(pendingDraft==null) return;
  srcEl.value=pendingDraft; pendingDraft=null;
  setDirty(srcEl.value!==ORIGINAL);
  buildMap(); paint(null);
  if(serverOK) rerender();
}

/* ── UI language (i18n) ─────────────────────────────────────────────
   Default English; the header toggle flips to Chinese and back. Every
   visible UI string is keyed: static text via data-i18n / data-i18n-title
   on the element, dynamic text via L('key') at the call site. The choice
   persists in localStorage so it survives reloads. The DSL source the user
   edits is never touched — this is chrome only. */
const I18N={
  en:{
    brand_sub:"isometric diagrams as code", lang_title:"Language · 语言",
    stale:"showing last good render",
    exp_svg:"↓ SVG", t_exp_svg:"download exactly what the canvas shows (last good render if the source is broken)",
    exp_png:"↓ PNG", t_exp_png:"download the current render as PNG (2x)",
    exp_yaml:"↓ YAML", t_exp_yaml:"download the current YAML source",
    overwrite:"↑ Overwrite", t_overwrite:"OVERWRITE the original file on disk with the current editor content",
    t_zoom_in:"zoom in (⌘+)", t_zoom_out:"zoom out (⌘−)", t_zoom_fit:"fit to window (⌘0)", t_zoom_reset:"reset to 100%",
    t_splitter:"drag to resize the editor",
    render:"Render", t_render:"Cmd/Ctrl+Enter",
    t_viewtoggle:"switch between 2.5D isometric and flat top-down plan view (preview only — does not change the source)",
    auto:"Auto", snap:"Snap", t_snap:"snap dragged nodes to the grid",
    t_copypath:"copy full path", revert:"revert", restore_draft:"restore draft",
    help_title:"Keyboard shortcuts",
    hk_rerender:"Re-render", hk_fit:"Fit to window", hk_zoom:"Zoom in / out", hk_indent:"Indent",
    hk_panel:"Show / hide editor", hk_help:"This panel",
    hk_pin:"Pin a node", hk_pin_v:"click it on the canvas",
    hk_jump:"Jump to an error", hk_jump_v:"click it in the issues panel", hk_delete:"Delete selected",
    ctx_add:"＋ Add node", ctx_edit:"✎ Edit details", ctx_dup:"⧉ Duplicate", ctx_del:"✕ Delete",
    detail_title:"Edit details", detail_cancel:"Cancel", detail_apply:"Update",
    foot_hover:"Hover a node to jump to its source · click to pin · double-click or right-click to edit",
    foot_zoom:"Scroll to zoom · drag empty canvas to box-select (shift+click adds) · alt-drag or middle-drag to pan · double-click empty canvas to reset",
    foot_render:"render", foot_shortcuts:"all shortcuts",
    foot_origsvg:"Original SVG", foot_browse:"Browse nodes", foot_about:"About Studio",
    // dynamic
    rendered:"Rendered", t_insync:"canvas is in sync with the code as of this time",
    static_file:"Static file", t_static:'run "isotopo serve <input>" for live re-render',
    offline:"Offline", t_offline:'renderer unreachable — is "isotopo serve" still running?',
    t_offline_write:'cannot write file — is "isotopo serve" still running?',
    rendering:"Rendering…", render_failed:"Render failed", t_render_failed:"the source has errors — see the issues panel",
    view_iso:"◳ Iso", view_plan:"◰ Plan",
    saving:"Saving…", not_saved:"Not saved", saved:"Saved", wrote:"wrote", save_failed:"Save failed",
    t_autosaved:"auto-saved to the source file", updated_from_file:"Updated from file",
    no_fields:"No editable fields.", edit_node:"Edit node — ", edit_canvas:"Edit canvas & background", edit_edge:"Edit edge — ",
    copy_from:"Copy style from", copy_pick:"Pick a node…", dd_none:"(none)", code:"Code",
    t_jump:"click to jump to this line", panel_hide:"hide editor (⌘E)", panel_show:"show editor (⌘E)",
    eff_auto:"auto", eff_inherits:"inherits", eff_default:"default", df_unset:"unset",
    opt_solid:"Solid", opt_dashed:"Dashed", opt_dotted:"Dotted", opt_none:"(none)",
    df_embedded:"Embedded image", df_replace:"Replace…", df_icon_ph:"iso://… or pick a file",
    df_clear:"Clear", df_browse:"Browse…"
  },
  zh:{
    brand_sub:"代码即等距架构图", lang_title:"Language · 语言",
    stale:"显示上一次成功的渲染",
    exp_svg:"↓ SVG", t_exp_svg:"下载画布当前所见(源码出错时为上次成功的渲染)",
    exp_png:"↓ PNG", t_exp_png:"以 PNG 下载当前渲染(2 倍)",
    exp_yaml:"↓ YAML", t_exp_yaml:"下载当前 YAML 源码",
    overwrite:"↑ 覆盖写入", t_overwrite:"用当前编辑内容覆盖写入磁盘上的原始文件",
    t_zoom_in:"放大 (⌘+)", t_zoom_out:"缩小 (⌘−)", t_zoom_fit:"适应窗口 (⌘0)", t_zoom_reset:"重置为 100%",
    t_splitter:"拖动以调整编辑器宽度",
    render:"渲染", t_render:"Cmd/Ctrl+Enter",
    t_viewtoggle:"在 2.5D 等距视图与俯视平面视图间切换(仅预览,不改源码)",
    auto:"自动", snap:"吸附", t_snap:"拖动节点时吸附到网格",
    t_copypath:"复制完整路径", revert:"还原", restore_draft:"恢复草稿",
    help_title:"键盘快捷键",
    hk_rerender:"重新渲染", hk_fit:"适应窗口", hk_zoom:"放大 / 缩小", hk_indent:"缩进",
    hk_panel:"显示 / 隐藏编辑器", hk_help:"本面板",
    hk_pin:"固定节点", hk_pin_v:"在画布上点击它",
    hk_jump:"跳转到错误", hk_jump_v:"在问题面板中点击它", hk_delete:"删除选中对象",
    ctx_add:"＋ 添加节点", ctx_edit:"✎ 编辑详情", ctx_dup:"⧉ 复制", ctx_del:"✕ 删除",
    detail_title:"编辑详情", detail_cancel:"取消", detail_apply:"更新",
    foot_hover:"悬停节点跳转到源码 · 点击固定 · 双击或右键编辑",
    foot_zoom:"滚轮缩放 · 空白处拖动框选(shift+点击增删) · alt 或中键拖动平移 · 双击空白画布重置",
    foot_render:"渲染", foot_shortcuts:"全部快捷键",
    foot_origsvg:"原始 SVG", foot_browse:"浏览节点", foot_about:"关于 Studio",
    // dynamic
    rendered:"已渲染", t_insync:"画布与代码同步于此刻",
    static_file:"静态文件", t_static:'运行 "isotopo serve <输入文件>" 以实时重渲染',
    offline:"离线", t_offline:'渲染器不可达 —— "isotopo serve" 还在运行吗?',
    t_offline_write:'无法写入文件 —— "isotopo serve" 还在运行吗?',
    rendering:"渲染中…", render_failed:"渲染失败", t_render_failed:"源码有错误 —— 见问题面板",
    view_iso:"◳ 等距", view_plan:"◰ 平面",
    saving:"保存中…", not_saved:"未保存", saved:"已保存", wrote:"已写入", save_failed:"保存失败",
    t_autosaved:"已自动保存到源文件", updated_from_file:"已从文件更新",
    no_fields:"无可编辑字段。", edit_node:"编辑节点 — ", edit_canvas:"编辑画布与背景", edit_edge:"编辑连线 — ",
    copy_from:"复制样式自", copy_pick:"选择节点…", dd_none:"(无)", code:"代码",
    t_jump:"点击跳转到该行", panel_hide:"隐藏编辑器 (⌘E)", panel_show:"显示编辑器 (⌘E)",
    eff_auto:"自动", eff_inherits:"继承", eff_default:"默认", df_unset:"未设置",
    opt_solid:"实线", opt_dashed:"虚线", opt_dotted:"点线", opt_none:"(无)",
    df_embedded:"内嵌图片", df_replace:"替换…", df_icon_ph:"iso://… 或选择文件",
    df_clear:"清除", df_browse:"浏览…"
  }
};
let UILANG=(function(){try{return localStorage.getItem('isotopo-uilang')==='zh'?'zh':'en';}catch(_){return 'en';}})();
function L(k){const d=I18N[UILANG]||I18N.en; return (d&&d[k]!=null)?d[k]:(I18N.en[k]!=null?I18N.en[k]:k);}
function refreshStatusText(){
  if(typeof serverOK==='undefined') return;
  if(!location.protocol.startsWith('http')){ setStatus('',L('static_file'),L('t_static')); return; }
  if(!serverOK){ setStatus('err',L('offline'),L('t_offline')); return; }
  markRendered();
}
function applyI18n(){
  document.querySelectorAll('[data-i18n]').forEach(el=>{el.textContent=L(el.getAttribute('data-i18n'));});
  document.querySelectorAll('[data-i18n-title]').forEach(el=>{el.title=L(el.getAttribute('data-i18n-title'));});
  document.documentElement.lang = UILANG==='zh'?'zh':'en';
  const lb=document.getElementById('langtoggle'); if(lb) lb.textContent = UILANG==='zh'?'中文':'EN';
  // state-dependent labels the static pass can't set on its own:
  const vt=document.getElementById('viewtoggle'); if(vt&&typeof VIEWMODE!=='undefined') vt.textContent = VIEWMODE==='top'?L('view_plan'):L('view_iso');
  const pt=document.getElementById('paneltoggle'); if(pt&&typeof panelOpen!=='undefined') pt.title = panelOpen?L('panel_hide'):L('panel_show');
  refreshStatusText();
}
function toggleLang(){
  UILANG = UILANG==='zh'?'en':'zh';
  try{localStorage.setItem('isotopo-uilang',UILANG);}catch(_){ }
  applyI18n();
  // the detail modal's field labels are localized server-side at fetch time;
  // if it's open, re-fetch so they switch language too.
  if(typeof detailModal!=='undefined' && detailModal && !detailModal.hidden && detailTarget) openDetail(detailTarget);
}

const zoomer=document.getElementById('zoomer'), viewport=document.getElementById('viewport');

/* ── editor backdrop + SVG↔source hover map ────────────────────── */
let lineMap={}, connMap=[];
function esc(t){return t.replace(/&/g,'&amp;').replace(/</g,'&lt;');}
function buildMap(){
  const lines=srcEl.value.split('\n'); lineMap={}; connMap=[];
  const idRe=/(?:^|[\s{])id:\s*"?([A-Za-z0-9_~-]+)/;
  for(let i=0;i<lines.length;i++){
    const m=lines[i].match(idRe); if(!m) continue;
    let start=i;
    if(!/^\s*-/.test(lines[i])){
      for(let k=i-1;k>=0;k--){
        if(/^\s*-\s/.test(lines[k])){start=k;break;}
        if(lines[k].trim() && lines[k].search(/\S/)===0) break;
      }
    }
    const ind=lines[start].search(/\S/);
    let end=lines.length-1;
    for(let k=start+1;k<lines.length;k++){
      const t=lines[k]; if(!t.trim()) continue;
      const ki=t.search(/\S/);
      if(ki<ind || (ki===ind && /^\s*-\s/.test(t)) || ki===0){end=k-1;break;}
    }
    if(!(m[1] in lineMap)) lineMap[m[1]]=[start,end];
  }
  // connMap[ci] = [startLine,endLine] of the ci-th connector, mirroring the
  // server's findConnectorLine (the FIRST connectors: block, in order), so
  // data-connector ci maps straight to its source lines.
  let c0=-1,cInd=0;
  for(let i=0;i<lines.length;i++){
    const m=lines[i].match(/^(\s*)connectors:\s*(#.*)?$/);
    if(m){c0=i;cInd=m[1].length;break;}
  }
  if(c0>=0){
    const starts=[]; let itemInd=-1, blockEnd=lines.length-1;
    for(let i=c0+1;i<lines.length;i++){
      const t=lines[i]; if(!t.trim()) continue;
      const ind=t.search(/\S/);
      if(ind<=cInd){blockEnd=i-1;break;}
      if(/^\s*-/.test(t)){ if(itemInd<0) itemInd=ind; if(ind===itemInd) starts.push(i); }
    }
    for(let k=0;k<starts.length;k++)
      connMap.push([starts[k], k+1<starts.length?starts[k+1]-1:blockEnd]);
  }
}
/* one-line YAML tokenizer: comments, keys, strings (iso:// URIs get
   their own class), numbers, booleans, list dashes. Escapes as it goes
   so layout stays byte-identical with the textarea overlay. */
function tokLine(l){
  if(!l) return ' ';
  let out='';
  const km=l.match(/^(\s*)(- )?([A-Za-z_][\w.-]*)(:)(?=\s|$)/);
  let rest=l;
  if(km){
    out+=esc(km[1])+(km[2]?'<span class="tk-d">- </span>':'')+
      '<span class="tk-k">'+esc(km[3])+'</span>:';
    rest=l.slice(km[0].length);
  }else{
    const dm=l.match(/^(\s*)(- )/);
    if(dm){out+=esc(dm[1])+'<span class="tk-d">- </span>';rest=l.slice(dm[0].length);}
    else rest=l;
  }
  const re=/("(?:[^"\\]|\\.)*"?)|(#.*$)|(\b(?:true|false|null)\b)|(-?\d+(?:\.\d+)?\b)/g;
  let last=0,m;
  while((m=re.exec(rest))){
    out+=esc(rest.slice(last,m.index));
    if(m[1])      out+='<span class="'+(m[1].indexOf('iso://')>=0?'tk-u':'tk-s')+'">'+esc(m[1])+'</span>';
    else if(m[2]) out+='<span class="tk-c">'+esc(m[2])+'</span>';
    else if(m[3]) out+='<span class="tk-b">'+esc(m[3])+'</span>';
    else          out+='<span class="tk-n">'+esc(m[4])+'</span>';
    last=m.index+m[0].length;
  }
  out+=esc(rest.slice(last));
  return out;
}
let flashLine=-1;
function paint(range){
  const lines=srcEl.value.split('\n');
  hlEl.innerHTML=lines.map((l,i)=>{
    const hit=range&&i>=range[0]&&i<=range[1];
    const cls='ln'+(hit?' hit':'')+(hit&&i===range[0]?' hit-a':'')+(hit&&i===range[1]?' hit-b':'')+(i===flashLine?' flash':'');
    return '<div class="'+cls+'">'+tokLine(l)+'</div>';
  }).join('');
  gutEl.innerHTML=lines.map((_,i)=>'<div>'+(i+1)+'</div>').join('');
  hlEl.scrollTop=srcEl.scrollTop; hlEl.scrollLeft=srcEl.scrollLeft; gutEl.scrollTop=srcEl.scrollTop;
}
srcEl.addEventListener('scroll',()=>{hlEl.scrollTop=srcEl.scrollTop;hlEl.scrollLeft=srcEl.scrollLeft;gutEl.scrollTop=srcEl.scrollTop;});
let pinId=null, pinCi=null;   // pinned node id OR connector index (exclusive)
function rangeFor(id){
  if(!(id in lineMap)) id=id.replace(/~\d+$/,'');
  return lineMap[id]||null;
}
// line range of whatever is currently pinned (node or edge), or null
function pinnedRange(){return pinId?rangeFor(pinId):(pinCi!=null?connMap[pinCi]:null);}
function glowEdge(ci){
  zoomer.querySelectorAll('path[data-connector]').forEach(p=>{
    p.classList.toggle('pinned', ci!=null && +p.getAttribute('data-connector')===ci);
  });
}
function scrollToLine(ln){
  const lh=srcEl.scrollHeight/srcEl.value.split('\n').length;
  srcEl.scrollTop=Math.max(0,ln*lh-60);
  hlEl.scrollTop=srcEl.scrollTop; gutEl.scrollTop=srcEl.scrollTop;
}
function glowOnly(id){
  zoomer.querySelectorAll('g[data-part-id]').forEach(g=>{
    const gid=g.getAttribute('data-part-id');
    g.classList.toggle('hi', gid===id || (id!==null && gid.replace(/~\d+$/,'')===id));
  });
}

/* ── multi-select: marquee box selection + shift-click + group move ──────
   `selection` holds BASE part ids (no ~N stack suffix). Selected nodes get
   the .sel class (persistent accent outline). Sources of truth:
     marquee drag on empty canvas   → replace (or extend with shift)
     shift+click on a node          → toggle that node
     Esc / plain click empty canvas → clear
   Only LEAF nodes are marquee-selectable: putting a container AND its child
   in one selection would double-move the child (the container's offset
   already carries it). Containers keep their existing single-drag path. */
let selection=new Set();
function baseId(g){return g.getAttribute('data-part-id').replace(/~\d+$/,'');}
function applySelectionClasses(){
  zoomer.querySelectorAll('g[data-part-id]').forEach(g=>{
    g.classList.toggle('sel', selection.has(baseId(g)));
  });
}
function setSelection(ids){ selection=new Set(ids); applySelectionClasses(); }
function clearSelection(){ if(selection.size){ selection.clear(); applySelectionClasses(); } }
function toggleSelect(id){ selection.has(id)?selection.delete(id):selection.add(id); applySelectionClasses(); }
// After a re-render replaces the SVG, drop ids that no longer exist and
// re-apply the .sel classes to the fresh elements.
function pruneSelection(){
  if(window.interactionModel&&selection.size){
    const live=new Set(window.interactionModel.map(p=>p.id));
    for(const id of [...selection]) if(!live.has(id)) selection.delete(id);
  }
  applySelectionClasses();
}
// Projected body/content polygons come from the same shape and layout resolvers
// as rendering. The union retains concave gaps and all stack instances.
function partScreenRegions(pm){
  const svg=zoomer.querySelector('svg'); if(!svg) return [];
  const m=svg.getScreenCTM(); if(!m) return [];
  const [tx,ty]=sceneOrigin();
  let regions=pm.selectionRegions;
  if(!Array.isArray(regions)){
    // Compatibility with an older server; new models always supply regions.
    const points=[];
    for(const x of [pm.x,pm.x+pm.w]) for(const y of [pm.y,pm.y+pm.d]) for(const z of [pm.z||0,(pm.z||0)+(pm.h||0)]){
      const [u,v]=worldToSvgUser(x,y,z); points.push([u-tx,v-ty]);
    }
    const xs=points.map(p=>p[0]),ys=points.map(p=>p[1]);
    regions=[[[Math.min(...xs),Math.min(...ys)],[Math.max(...xs),Math.min(...ys)],[Math.max(...xs),Math.max(...ys)],[Math.min(...xs),Math.max(...ys)]]];
  }
  const out=regions.map(poly=>poly.map(([x,y])=>[m.a*(x+tx)+m.c*(y+ty)+m.e,m.b*(x+tx)+m.d*(y+ty)+m.f]));
  out.push(...screenLabelRegions(pm.id,svg));
  return out;
}

let screenLabelCache=null;
function screenLabelRegions(id,svg){
  const records=window.screenLabels;
  if(!screenLabelCache||screenLabelCache.svg!==svg||screenLabelCache.records!==records){
    const groups=Array.from(svg.querySelectorAll('g[data-screen-label]'));
    const byID=new Map();
    let valid=Array.isArray(records)&&records.length===groups.length;
    if(valid) for(let i=0;i<records.length;i++){
      const record=records[i],g=groups[i],b=record&&record.bounds;
      if(!record||typeof (record.part_id||'')!=='string'||(g.getAttribute('data-part-id')||'')!==(record.part_id||'')||
         !b||![b.x,b.y,b.width,b.height].every(Number.isFinite)||b.width<=0||b.height<=0){valid=false;break;}
      const owner=(record.part_id||'').replace(/~\d+$/,'');
      if(!owner) continue;
      if(!byID.has(owner)) byID.set(owner,[]);
      byID.get(owner).push({g,record});
    }
    // Old servers and unmatched records keep the full DOM fallback. Never
    // associate a stale record with a different source part by array position.
    if(!valid){
      byID.clear();
      groups.forEach(g=>{const owner=(g.getAttribute('data-part-id')||'').replace(/~\d+$/,'');if(!owner)return;if(!byID.has(owner))byID.set(owner,[]);byID.get(owner).push({g,record:null});});
    }
    screenLabelCache={svg,records,byID};
  }
  const out=[];
  for(const {g,record} of screenLabelCache.byID.get(id)||[]){
    if(!record||record.text_metrics!=='shaped-ink'){
      // An unbound font can paint outside its estimated chip. Retain the real
      // browser envelope until its glyph geometry has a bound resource.
      const b=g.getBoundingClientRect();
      if(b.width&&b.height)out.push([[b.left,b.top],[b.right,b.top],[b.right,b.bottom],[b.left,b.bottom]]);
      continue;
    }
    const m=g.getScreenCTM();if(!m)continue;
    const b=record.bounds;
    // Use the CURRENT group transform: a live drag translates the DOM before
    // the next server result arrives. Committed geometry must follow it.
    out.push([[b.x,b.y],[b.x+b.width,b.y],[b.x+b.width,b.y+b.height],[b.x,b.y+b.height]].map(([x,y])=>[m.a*x+m.c*y+m.e,m.b*x+m.d*y+m.f]));
  }
  return out;
}
function partScreenAABB(pm){
  const pts=partScreenRegions(pm).flat(); if(!pts.length) return null;
  return {minX:Math.min(...pts.map(p=>p[0])),minY:Math.min(...pts.map(p=>p[1])),maxX:Math.max(...pts.map(p=>p[0])),maxY:Math.max(...pts.map(p=>p[1]))};
}
function marqueeHits(r){
  const out=[];
  if(!window.interactionModel||!r) return out;
  for(const pm of window.interactionModel){
    if(!pm.container&&partScreenRegions(pm).some(poly=>polygonTouchesRect(poly,r))) out.push(pm.id);
  }
  return out;
}
// E2E hook: tests read the live selection without poking at internals.
window.studioSelection=()=>[...selection];
/* ── drag-to-connect: draw a new connector by dragging from a node ────────
   Hovering a node shows 4 handle dots at the iso face centres (top, left,
   right) and the nadir vertex. Positions are computed from the actual polygon
   vertices via getScreenCTM() — NOT from the DOM bounding box — so they sit
   exactly on the isometric surface regardless of zoom or scroll.
   Handles live on a dedicated SVG overlay so they don't interfere with the
   existing node SVG structure. ──────────────────────────────────────────── */
let connDrag=null;       // {fromId, startX, startY, line} while dragging
let connTarget=null;     // id of the node the pointer is currently over

// World-space interaction model received from the server alongside each SVG.
// Array of {id, container, x, y, w, d, anchors:[{name,wx,wy}]}.
// Exposed on window so E2E tests (Playwright) can inspect it directly.
window.interactionModel=null;
window.screenLabels=null;
function updateModel(data){
 if(data&&data.model) window.interactionModel=data.model;
 window.screenLabels=data&&Array.isArray(data.screenLabels)?data.screenLabels:null;
 screenLabelCache=null;
 window.connectorRoutes=data&&data.routes||[];
 window.connectorRouteSource=srcEl.value;
}

const HANDLE_R=5, HANDLE_COL='#2563EB', HANDLE_HOV='#1D4ED8';

// Connection handles live INSIDE the rendered SVG, in the SAME coordinate
// system as the nodes. The zoomer's pan/zoom transform therefore moves
// handles and nodes together — a handle can never drift off its node, and no
// per-frame re-projection is needed. handleLayer() is a <g> appended to the
// root <svg>; it's recreated lazily after each re-render replaces the SVG.
function handleLayer(){
  const svg=zoomer.querySelector('svg'); if(!svg) return null;
  let g=svg.querySelector('#conn-handles');
  if(!g){ g=document.createElementNS('http://www.w3.org/2000/svg','g'); g.setAttribute('id','conn-handles'); svg.appendChild(g); }
  return g;
}
let _clearTimer=null;
function clearHandles(immediate){
  const wipe=()=>{ const l=zoomer.querySelector('#conn-handles'); l&&l.querySelectorAll('.conn-handle').forEach(h=>h.remove()); };
  if(immediate){ if(_clearTimer){clearTimeout(_clearTimer);_clearTimer=null;} wipe(); }
  else{ if(_clearTimer) return; _clearTimer=setTimeout(()=>{_clearTimer=null;wipe();},120); }
}
function cancelClear(){ if(_clearTimer){clearTimeout(_clearTimer);_clearTimer=null;} }

// ── world ↔ SVG-user coordinate helpers ──────────────────────────────────
// data-scene-tx/ty (emitted by the renderer on the root <svg>) records where
// world (0,0,0) lands in SVG user space.
function sceneOrigin(){
  const svg=zoomer.querySelector('svg');
  return svg?[parseFloat(svg.dataset.sceneTx||'0'),parseFloat(svg.dataset.sceneTy||'0')]:[0,0];
}
// world → SVG-user, applying the SAME projection the renderer uses
// (projectIso subtracts wz, so a handle rides the correct face height).
function isTopView(){return zoomer.querySelector('svg')?.dataset.projection==='top';}
function worldToSvgUser(wx,wy,wz){
  const [tx,ty]=sceneOrigin();
  if(isTopView()) return [wx+tx,wy+ty];
  return [(wx-wy)*C30+tx,(wx+wy)*S30-(wz||0)+ty];
}
// Preserve world (0,0) and the effective scale across viewBox/margin changes.
// A responsive SVG can change its own scale independently of Studio's zoom.
// Recentring a moved node on the cursor loses the original grab offset.
function captureSceneFrame(){
  const svg=zoomer.querySelector('svg'),m=svg&&svg.getScreenCTM();if(!m) return null;
  const [x,y]=sceneOrigin();return [m.a*x+m.c*y+m.e,m.b*x+m.d*y+m.f,Math.hypot(m.a,m.b)];
}
function restoreSceneFrame(frame){
  let now=captureSceneFrame();if(!frame||!now)return;
  if(frame[2]>0&&now[2]>0){
    scale*=frame[2]/now[2];apply();now=captureSceneFrame();if(!now)return;
  }
  panX+=frame[0]-now[0];panY+=frame[1]-now[1];apply();
}
// client (screen) px → SVG-user via the live CTM (covers viewBox, flex, and
// the zoomer's pan/zoom). Used for drag endpoints + nearest-anchor tests.
function clientToSvgUser(clientX,clientY){
  const svg=zoomer.querySelector('svg'); if(!svg) return [0,0];
  const m=svg.getScreenCTM(); if(!m) return [0,0];
  const p=svg.createSVGPoint(); p.x=clientX; p.y=clientY;
  const u=p.matrixTransform(m.inverse());
  return [u.x,u.y];
}
// SVG-user units per screen pixel — handles multiply by this to keep a
// constant on-screen size regardless of zoom.
function userPxScale(){
  const svg=zoomer.querySelector('svg'); const m=svg&&svg.getScreenCTM();
  return (m&&m.a)?1/m.a:1;
}
// Re-size existing handles after a zoom changes the user→px ratio (pan leaves
// it unchanged, so this is a cheap no-op then).
function rescaleHandles(){
  const l=zoomer.querySelector('#conn-handles'); if(!l) return;
  const s=userPxScale();
  l.querySelectorAll('.conn-handle').forEach(c=>{ c.setAttribute('r',HANDLE_R*s); c.setAttribute('stroke-width',1.5*s); });
  const rb=l.querySelector('#conn-rubber'); if(rb) rb.setAttribute('stroke-width',1.8*s);
}

// Polygon centroid / nadir in SVG-user coords (fallback for shapes with no
// interaction model, e.g. .d2 files). getCTM maps the polygon's local points
// into SVG-user space (includes the node <g>'s translate, excludes zoomer).
function polyCentroidUser(polygon){
  if(!polygon) return null;
  const pts=polygon.points;
  if(!pts||pts.numberOfItems===0) return null;
  let ux=0, uy=0;
  for(let i=0;i<pts.numberOfItems;i++){ ux+=pts.getItem(i).x; uy+=pts.getItem(i).y; }
  ux/=pts.numberOfItems; uy/=pts.numberOfItems;
  const m=polygon.getCTM(); if(!m) return [ux,uy];
  const p=polygon.ownerSVGElement.createSVGPoint(); p.x=ux; p.y=uy;
  const u=p.matrixTransform(m); return [u.x,u.y];
}

// Return the nadir (lowest point in SVG-user space) across all face polygons.
function nadirUser(g){
  let best=null;
  g.querySelectorAll('polygon[data-face]').forEach(poly=>{
    const pts=poly.points, m=poly.getCTM(), svg=poly.ownerSVGElement;
    for(let i=0;i<pts.numberOfItems;i++){
      const p=svg.createSVGPoint();
      p.x=pts.getItem(i).x; p.y=pts.getItem(i).y;
      const u=m?p.matrixTransform(m):p;
      if(!best||u.y>best[1]) best=[u.x,u.y];
    }
  });
  return best;
}

// One source may have several displayed instances. Ports retain the exact
// instance ID; source editing and movement continue to use baseId separately.
function connectionPorts(partId){
  const pm=window.interactionModel&&window.interactionModel.find(p=>p.id===partId.replace(/~\d+$/,''));
  if(!pm) return null;
  if(Array.isArray(pm.ports)&&pm.ports.length) return pm.ports.filter(p=>p.instanceId===partId);
  if(Array.isArray(pm.ports)&&isTopView()) return [];
  return pm.anchors||[]; // compatibility with older servers
}
function nearestAnchor(partId,clientX,clientY){
  const ports=connectionPorts(partId);if(!ports||!ports.length)return null;
  const [ux,uy]=clientToSvgUser(clientX,clientY);let best=null,bestD=Infinity;
  for(const a of ports){
    const [cx,cy]=worldToSvgUser(a.wx,a.wy,a.wz),d=Math.hypot(ux-cx,uy-cy);
    if(d<bestD){bestD=d;best={name:a.name,cx,cy};}
  }
  return best;
}

// showHandles renders anchor dots for the hovered node, INSIDE the rendered
// SVG so they share the node's coordinate system (pan/zoom move them in
// lockstep). Positions come from the interaction model's world anchors
// projected to SVG-user space; falls back to polygon geometry for shapes not
// in the model (e.g. d2 files). Dots are sized in SVG-user units scaled by
// 1/CTM so they read as a constant on-screen size.
// New models expose fixed ports in either projection. Legacy face anchors
// remain the fallback for older servers and non-body annotation shapes.
function showHandles(g){
  const layer=handleLayer(); if(!layer) return;
  clearHandles(true);
  const partId=g.getAttribute('data-part-id');

  // Build the list of {name, cx, cy} anchor points in SVG-user coords.
  let anchors=[];
  const ports=connectionPorts(partId);
  if(ports!==null){
    for(const a of ports){
      const [cx,cy]=worldToSvgUser(a.wx,a.wy,a.wz);
      anchors.push({name:a.name,cx,cy,approximate:a.approximate});
    }
  }else{
    // Polygon fallback: top/right/bottom(nadir)/left
    const fallback=[
      {name:'top',    pt:polyCentroidUser(g.querySelector('polygon[data-face="top"]'))},
      {name:'right',  pt:polyCentroidUser(g.querySelector('polygon[data-face="right"]'))},
      {name:'bottom', pt:nadirUser(g)},
      {name:'left',   pt:polyCentroidUser(g.querySelector('polygon[data-face="left"]'))},
    ];
    for(const f of fallback){
      if(f.pt) anchors.push({name:f.name,cx:f.pt[0],cy:f.pt[1]});
    }
  }

  const s=userPxScale();
  anchors.forEach(({name,cx,cy,approximate})=>{
    const c=document.createElementNS('http://www.w3.org/2000/svg','circle');
    c.setAttribute('cx',cx); c.setAttribute('cy',cy);
    c.setAttribute('r',HANDLE_R*s);
    c.setAttribute('fill',HANDLE_COL);
    c.setAttribute('stroke','#fff'); c.setAttribute('stroke-width',1.5*s);
    c.setAttribute('class','conn-handle');
    c.style.cssText='pointer-events:all;cursor:crosshair;';
    c.dataset.name=name;c.dataset.instanceId=partId;
    if(approximate)c.setAttribute('stroke-dasharray','2 2');
    c.dataset.cx=cx; c.dataset.cy=cy;
    c.addEventListener('mouseenter',()=>{cancelClear();c.setAttribute('fill',HANDLE_HOV);});
    c.addEventListener('mouseleave',()=>c.setAttribute('fill',HANDLE_COL));
    c.addEventListener('mousedown',ev=>{
      if(ev.button!==0) return;
      ev.preventDefault(); ev.stopPropagation();
      const line=document.createElementNS('http://www.w3.org/2000/svg','polyline');
      line.setAttribute('points',cx+','+cy+' '+cx+','+cy);
      line.setAttribute('stroke',HANDLE_COL); line.setAttribute('stroke-width',1.8*userPxScale());
      line.setAttribute('fill','none'); line.setAttribute('stroke-dasharray','5,3');
      line.setAttribute('id','conn-rubber');
      // The rubber-band follows the cursor, so its endpoint sits right under the
      // pointer. Without this it would intercept the hit-test and swallow the
      // target node's mouseenter — leaving connTarget null and the drop a no-op.
      line.style.pointerEvents='none';
      layer.appendChild(line);
      // Record which anchor the drag started from (SVG-user coords).
      connDrag={fromId:partId, fromAnchor:name, startX:cx, startY:cy, line};
      document.body.style.cursor='crosshair';
    });
    layer.appendChild(c);
  });
}

// highlightNearestHandle highlights the anchor dot nearest the cursor.
// Cursor (clientX, clientY) and dots are compared in SVG-user space.
function highlightNearestHandle(clientX, clientY){
  const layer=zoomer.querySelector('#conn-handles'); if(!layer) return;
  const handles=[...layer.querySelectorAll('.conn-handle')];
  if(!handles.length) return;
  const [ux,uy]=clientToSvgUser(clientX,clientY);
  let bestEl=null, bestD=Infinity;
  for(const h of handles){
    const hx=parseFloat(h.getAttribute('cx')), hy=parseFloat(h.getAttribute('cy'));
    const d=Math.hypot(ux-hx,uy-hy);
    if(d<bestD){bestD=d;bestEl=h;}
  }
  for(const h of handles) h.setAttribute('fill',h===bestEl?HANDLE_HOV:HANDLE_COL);
}

window.addEventListener('mousemove',ev=>{
  if(connDrag){
    const handle=ev.target?.closest?.('.conn-handle'),node=ev.target?.closest?.('g[data-part-id]');
    const id=handle?.dataset.instanceId||node?.getAttribute('data-part-id');
    connTarget=id&&id!==connDrag.fromId?id:null;
    const target=connTarget&&nearestAnchor(connTarget,ev.clientX,ev.clientY);
    const [ux,uy]=target?[target.cx,target.cy]:clientToSvgUser(ev.clientX,ev.clientY);
    connDrag.line.setAttribute('points',connDrag.startX+','+connDrag.startY+' '+ux+','+uy);
    if(target)highlightNearestHandle(ev.clientX,ev.clientY);
    return;
  }
  const layer=zoomer.querySelector('#conn-handles');
  const overHandle=ev.target&&ev.target.classList&&ev.target.classList.contains('conn-handle');
  const overNode=ev.target&&ev.target.closest&&ev.target.closest('g[data-part-id]');
  if(overHandle||overNode){
    cancelClear();
    if(layer&&layer.querySelector('.conn-handle')) highlightNearestHandle(ev.clientX,ev.clientY);
  }else{
    if(layer&&layer.querySelector('.conn-handle')) clearHandles(); // debounced 120ms
    // Sweep stray node highlight: a node keeps .hi when the pointer slips off it
    // onto an in-SVG connection handle (which suppresses the node's mouseleave
    // clear). On open canvas, clear any non-pinned highlight left behind.
    zoomer.querySelectorAll('g[data-part-id].hi').forEach(g=>{ if(pinId!==g.getAttribute('data-part-id')) g.classList.remove('hi'); });
  }
},true);

window.addEventListener('mouseup',async ev=>{
  if(!connDrag) return;
  const drag=connDrag; connDrag=null;
  drag.line.remove(); clearHandles(true);
  document.body.style.cursor='';
  const toId=connTarget;
  connTarget=null;
  if(!toId||toId===drag.fromId||!serverOK) return;
  const target=nearestAnchor(toId,ev.clientX,ev.clientY);
  await commitConnection(drag.fromId,drag.fromAnchor||'',toId,target?target.name:'');
},true);

async function commitConnection(fromId,fromAnchor,toId,toAnchor){
  if(!serverOK)return false;
  try{
    let url='/api/op?op=add-edge&from='+encodeURIComponent(fromId)+'&to='+encodeURIComponent(toId)+'&format='+encodeURIComponent(LANG);
    if(fromAnchor) url+='&fanchor='+encodeURIComponent(fromAnchor);
    if(toAnchor)   url+='&tanchor='+encodeURIComponent(toAnchor);
    const r=await fetch(url+projQS(),{method:'POST',body:srcEl.value});
    if(!r.ok) return false;
    const data=await r.json();
    if(!data.svg||typeof data.yaml!=='string')return false;
    const frame=captureSceneFrame();pushUndo();
    if(typeof data.yaml==='string'){ srcEl.value=data.yaml; setDirty(srcEl.value!==ORIGINAL);
      try{localStorage.setItem(DRAFTKEY,srcEl.value);}catch(_){} }
    if(data.svg){ updateModel(data); zoomer.innerHTML=data.svg; buildMap(); paint(pinnedRange()); wireHover(); wireDrag(); adaptStage(); markRendered(); }
    restoreSceneFrame(frame);
    showIssues(data.issues||[]);return true;
  }catch(_){return false;}
}

function wireHover(){
  zoomer.querySelectorAll('g[data-part-id]').forEach(g=>{
    const id=g.getAttribute('data-part-id');
    g.style.cursor='move';
    g.addEventListener('mouseenter',()=>{
      g.classList.add('hi');
      if(connDrag){ connTarget=id; showHandles(g); return; }
      showHandles(g);
      const r=rangeFor(id); if(!r) return;
      paint(r); scrollToLine(r[0]);
    });
    g.addEventListener('mouseleave',(ev)=>{
      if(connDrag){
        if(ev.relatedTarget?.classList?.contains('conn-handle')&&ev.relatedTarget.dataset.instanceId===id)return;
        if(connTarget===id)connTarget=null;return;
      }
      // Don't clear handles if the cursor moved onto one of them — they live
      // in an overlay on top of the node and the browser fires mouseleave on
      // the <g> when the pointer enters an overlaid element.
      if(ev.relatedTarget && ev.relatedTarget.classList && ev.relatedTarget.classList.contains('conn-handle')) return;
      clearHandles();
      if(pinId!==id) g.classList.remove('hi');
      paint(pinnedRange());
    });
    g.addEventListener('click',e=>{
      e.stopPropagation();
      // shift+click = add/remove this node in the multi-selection (leaves
      // only — a container in a group selection would double-move its
      // children). It never pins, so the two idioms don't fight.
      if(e.shiftKey){
        const bid=id.replace(/~\d+$/,'');
        if(!isContainerId(bid)) toggleSelect(bid);
        return;
      }
      pinId = (pinId===id)?null:id; pinCi=null;   // pinning a node clears any edge pin
      glowOnly(pinId); glowEdge(null);
      const pr=pinnedRange();
      paint(pr);
      if(pr) scrollToLine(pr[0]);
    });
  });
  // Edges: hover highlights + scrolls to the connector's source lines;
  // click PINS it (persistent highlight) just like a node. The line itself
  // glows via CSS (.pinned / :hover).
  zoomer.querySelectorAll('path[data-connector]').forEach(p=>{
    const ci=+p.getAttribute('data-connector');
    p.addEventListener('mouseenter',()=>{
      const r=connMap[ci]; if(!r) return;
      paint(r); scrollToLine(r[0]);
    });
    p.addEventListener('mouseleave',()=>{
      paint(pinnedRange());
    });
    p.addEventListener('click',e=>{
      e.stopPropagation();
      pinCi = (pinCi===ci)?null:ci; pinId=null;   // pinning an edge clears any node pin
      glowOnly(null); glowEdge(pinCi);
      const pr=pinnedRange();
      paint(pr);
      if(pr) scrollToLine(pr[0]);
    });
  });
  if(pinId) glowOnly(pinId);
  if(pinCi!=null) glowEdge(pinCi);
}

/* ── drag-to-edit: move a node (offset) or shift an edge (bend) ──── */
const C30=0.8660254037844387, S30=0.5;
let nodeDrag=null, edgeDrag=null;
function screenToWorldDelta(dsx,dsy){
  if(isTopView()) return [dsx,dsy];
  // invert the iso ground-plane projection (sx=(wx-wy)c30, sy=(wx+wy)s30)
  return [dsx/(2*C30)+dsy/(2*S30), -dsx/(2*C30)+dsy/(2*S30)];
}
// Route provenance comes from the same render as the SVG. Editing a stale
// diagram or an ambiguous projected overlap must never guess a world segment.
function connectorRouteForPath(p){
  if(window.connectorRouteSource!==srcEl.value || isTopView()) return null;
  return (window.connectorRoutes||[]).find(r=>r.index===+p.getAttribute('data-connector'))||null;
}
// Include visible legs without edit provenance: they still compete visually
// with nearby editable legs. Hidden or endpoint-clipped geometry does not.
function visibleRouteSegments(p){
  const r=connectorRouteForPath(p),d=r&&r.drawing;
  if(!d||d.kind!=='polyline')return [];
  const depth=d.spatial&&d.spatial.mode==='depth';
  const coverage=depth?d.spatial.spans:d.body_coverage?.spans;
  const spans=[];
  for(const cover of coverage||[]){
    if((depth?cover.occluders||[]:cover.opaque_parts||[]).length)continue;
    const a=d.points[cover.segment],b=d.points[cover.segment+1];
    const lo=cover.t0,hi=cover.t1;if(!a||!b||hi<=lo)continue;
    spans.push({segment:cover.segment,t0:lo,t1:hi,sx:a[0]+(b[0]-a[0])*lo,sy:a[1]+(b[1]-a[1])*lo,
      ex:a[0]+(b[0]-a[0])*hi,ey:a[1]+(b[1]-a[1])*hi});
  }
  return spans;
}
function editableRouteSegments(p){
  const r=connectorRouteForPath(p);
  if(!r||!r.drawing||!r.world3d)return [];
  const spans=[],drawing=r.drawing,bySegment=new Map();
  for(const span of visibleRouteSegments(p)){
    if(!bySegment.has(span.segment))bySegment.set(span.segment,[]);
    bySegment.get(span.segment).push(span);
  }
  for(const s of drawing.editable||[]){
    const a=drawing.points[s.segment],b=drawing.points[s.segment+1];
    const w=r.world3d[s.route_segment],v=r.world3d[s.route_segment+1];
    if(!a||!b||!w||!v)continue;
    const axis=[0,1,2].find(k=>Math.abs(w[k]-v[k])>1e-5);
    if(axis===undefined)continue;
    for(const cover of bySegment.get(s.segment)||[]){
      const lo=Math.max(s.t0,cover.t0),hi=Math.min(s.t1,cover.t1);
      if(hi<=lo)continue;
      spans.push({segment:s.segment,j:s.route_segment,axis,sx:a[0]+(b[0]-a[0])*lo,sy:a[1]+(b[1]-a[1])*lo,
        ex:a[0]+(b[0]-a[0])*hi,ey:a[1]+(b[1]-a[1])*hi});
    }
  }
  return spans;
}

function distToSeg(px,py,ax,ay,bx,by){
  const vx=bx-ax,vy=by-ay,wx=px-ax,wy=py-ay;
  const length=vx*vx+vy*vy; let t=length>0?(wx*vx+wy*vy)/length:0; t=Math.max(0,Math.min(1,t));
  return Math.hypot(px-(ax+t*vx),py-(ay+t*vy));
}
function segmentHitCandidates(p){
  const m=p.getScreenCTM();if(!m)return null;
  const project=s=>{
    const a=new DOMPoint(s.sx,s.sy).matrixTransform(m),b=new DOMPoint(s.ex,s.ey).matrixTransform(m);
    return {...s,ax:a.x,ay:a.y,bx:b.x,by:b.y};
  };
  return {editable:editableRouteSegments(p).map(project),visible:visibleRouteSegments(p).map(project)};
}
function nearestSegmentCandidate(candidates,cx,cy){
  if(!candidates)return null;
  const distanceTo=s=>distToSeg(cx,cy,s.ax,s.ay,s.bx,s.by);
  let best=null,distance=7;
  for(const s of candidates.editable){
    const d=distanceTo(s);
    if(d<distance){best=s;distance=d;}
  }
  if(!best)return null;
  // A one-CSS-pixel separation is required from another visible leg of this
  // same SVG path. Native hit testing already distinguishes different paths.
  // A crossing or near tie must not choose a world segment by array order.
  for(const s of candidates.visible){
    if(s.segment!==best.segment&&distanceTo(s)<=distance+1)return null;
  }
  return best;
}
function nearestEditableSegment(p,cx,cy){
  return nearestSegmentCandidate(segmentHitCandidates(p),cx,cy);
}

function beginSegmentDrag(p,seg,e){
  if(!serverOK||nodeDrag||edgeDrag) return;
  removeSegHandles(p);p.classList.remove('edge-hover');
  renderVersion++;
  const m=p.ownerSVGElement.getScreenCTM();if(!m)return;
  edgeDrag={segmentEdit:true,el:p,ci:+p.getAttribute('data-connector'),seg,x:e.clientX,y:e.clientY,
    inverse:m.inverse(),source:srcEl.value,projection:VIEWMODE,originalSvg:zoomer.innerHTML,
    frame:captureSceneFrame(),version:0,accepted:-1,desired:null,result:null,running:false,released:false,cancelled:false};
  document.body.style.cursor='grabbing';
}
window.addEventListener('blur',()=>cancelSegmentDrag());
function segmentDragCurrent(d){
  return edgeDrag===d&&!d.cancelled&&srcEl.value===d.source&&VIEWMODE===d.projection;
}
function segmentDragDelta(d,e){
  // Freeze the full initial CTM. SVG replacement/viewBox changes during the
  // preview must not feed back into the world delta of this same gesture.
  const a=new DOMPoint(d.x,d.y).matrixTransform(d.inverse),b=new DOMPoint(e.clientX,e.clientY).matrixTransform(d.inverse);
  const w=screenToWorldDelta(b.x-a.x,b.y-a.y);
  if(d.seg.axis===0)w[0]=0;else if(d.seg.axis===1)w[1]=0;
  return w.map(v=>Math.round(v*1e6)/1e6);
}
function requestSegmentPreview(d,e,release){
  if(!segmentDragCurrent(d)){cancelSegmentDrag();return;}
  const delta=segmentDragDelta(d,e);
  if(release)d.released=true;
  if(Math.abs(delta[0])<=2&&Math.abs(delta[1])<=2){
    if(release){cancelSegmentDrag();return;}
    delta[0]=delta[1]=0;
  }
  if(!d.desired||d.desired[0]!==delta[0]||d.desired[1]!==delta[1]){
    d.desired=delta;d.version++;d.result=null;
  }
  if(d.released)marqueeJustEnded=true;
  pumpSegmentPreview(d);
}
function installSegmentSVG(svg,frame,interactive){
  zoomer.innerHTML=svg;adaptStage();restoreSceneFrame(frame);
  if(interactive){buildMap();paint(pinnedRange());wireHover();wireDrag();}
}
function cancelSegmentDrag(){
  const d=edgeDrag;if(!d||!d.segmentEdit)return;
  d.cancelled=true;edgeDrag=null;
  if(d.controller)d.controller.abort();
  document.body.style.cursor='';
  // The preview never changes source. Restore its original picture even when
  // typing invalidated the gesture; the normal stale/render UI handles typing.
  installSegmentSVG(d.originalSvg,d.frame,true);
}
function finishSegmentDrag(d){
  if(!segmentDragCurrent(d)||!d.released||d.accepted!==d.version||!d.result)return;
  const data=d.result;
  if(data.yaml===d.source){cancelSegmentDrag();return;}
  edgeDrag=null;document.body.style.cursor='';
  pushUndo();srcEl.value=data.yaml;setDirty(srcEl.value!==ORIGINAL);
  try{localStorage.setItem(DRAFTKEY,srcEl.value);}catch(_){}
  updateModel(data);
  // Commit exactly the accepted preview response, without another solve.
  installSegmentSVG(data.svg,d.frame,true);markRendered();showIssues(data.issues||[]);
}
async function pumpSegmentPreview(d){
  if(d.running)return;
  d.running=true;
  try{
    while(segmentDragCurrent(d)&&d.accepted!==d.version){
      const version=d.version,delta=d.desired;
      d.controller=new AbortController();
      const timeout=setTimeout(()=>d.controller.abort(),10000);
      let data;
      try{
        const r=await fetch('/api/move?kind=edge&ci='+d.ci+'&segment='+d.seg.j+'&dwx='+delta[0]+'&dwy='+delta[1]+
          '&format='+encodeURIComponent(LANG)+'&projection='+encodeURIComponent(d.projection),
          {method:'POST',body:d.source,signal:d.controller.signal});
        if(!r.ok)throw Error(await r.text());
        data=await r.json();
        if(!data.svg||typeof data.yaml!=='string')throw Error((data.issues||[]).map(i=>i.message).join('; ')||L('render_failed'));
      }catch(error){
        if(segmentDragCurrent(d)&&version===d.version){cancelSegmentDrag();showIssues([{severity:'error',path:'connector',message:String(error)}]);}
        if(!segmentDragCurrent(d))return;
        continue;
      }finally{clearTimeout(timeout);}
      if(!segmentDragCurrent(d))return;
      if(version!==d.version)continue; // coalesce, never display stale answers
      d.result=data;d.accepted=version;
      installSegmentSVG(data.svg,d.frame,false);
    }
    finishSegmentDrag(d);
  }finally{d.running=false;}
}

// Live edge-follow: shift a path's FROM end (first coord pair) and/or TO end
// (last pair) by (dx,dy) user units. Used while dragging a node so its docked
// connector endpoints track it — the node translates rigidly by the same
// (dx,dy), so the glued endpoint stays on its face and the line rubber-bands.
function shiftEnds(d,moveFrom,moveTo,dx,dy){
  const re=/-?\d*\.?\d+/g, nums=[]; let m;
  while((m=re.exec(d))) nums.push({v:parseFloat(m[0]),i:m.index,len:m[0].length});
  if(nums.length<4) return d;
  const edits=[];
  if(moveFrom){edits.push([0,dx],[1,dy]);}
  if(moveTo){edits.push([nums.length-2,dx],[nums.length-1,dy]);}
  edits.sort((a,b)=>b[0]-a[0]);   // right-to-left so earlier offsets stay valid
  let s=d;
  for(const ed of edits){const t=nums[ed[0]]; s=s.slice(0,t.i)+(t.v+ed[1]).toFixed(2)+s.slice(t.i+t.len);}
  return s;
}
const SNAP_STEP=40;   // one grid cell (world units)
// ids (optional): a multi-selection group move — the server applies the same
// delta to every id atomically in ONE round-trip (/api/move&ids=…), so the
// batch is a single UNDO step and can never half-apply.
async function commitMove(kind,key,dwx,dwy,dropX,dropY,wp,ids){
  if(!serverOK) return false;
  const snapEl=document.getElementById('snap');
  const snap=(kind==='node' && snapEl && snapEl.checked) ? SNAP_STEP : 0;
  const multi=!!(ids&&ids.length>1);
  let qp = kind==='node' ? 'kind=node&id='+encodeURIComponent(key) : 'kind=edge&ci='+key;
  if(multi) qp='kind=node&ids='+encodeURIComponent(JSON.stringify(ids));
  if(wp) qp += '&wp='+encodeURIComponent(JSON.stringify(wp));
  try{
    const r=await fetch('/api/move?'+qp+'&dwx='+dwx+'&dwy='+dwy+'&snap='+snap+'&format='+encodeURIComponent(LANG)+projQS(),
      {method:'POST',body:srcEl.value});
    if(!r.ok) return false;
    const data=await r.json();
    if(!data.svg||typeof data.yaml!=='string'){showIssues(data.issues||[]);return false;}
    const frame=captureSceneFrame();
    // Record only accepted edits: a rejected request must preserve redo and
    // must not evict the oldest entry from a full undo history.
    pushUndo();
    if(typeof data.yaml==='string'){ srcEl.value=data.yaml; setDirty(srcEl.value!==ORIGINAL);
      try{localStorage.setItem(DRAFTKEY,srcEl.value);}catch(_){} }
    if(data.svg){
      updateModel(data);
      zoomer.innerHTML=data.svg;
      buildMap(); paint(pinnedRange());
      wireHover(); wireDrag(); adaptStage(); markRendered();
      restoreSceneFrame(frame);
    }
    showIssues(data.issues||[]);
    return true;
  }catch(_){return false;}
}
/* ── right-click → context menu → "Edit details" detail editor ──────── */
const ctxmenu=document.getElementById('ctxmenu');
const detailModal=document.getElementById('detailModal');
const detailFields=document.getElementById('detailFields');
const detailTitle=document.getElementById('detailTitle');
let ctxTarget=null, detailTarget=null;
function escAttr(t){return String(t).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/"/g,'&quot;');}
// A part is a container (group/boundary/composite lane) if its source block
// holds a nested `parts:` key. Containers can't be duplicated (children would
// get colliding ids) or deleted from the canvas (it'd remove the whole lane),
// matching the server-side guards in ApplyOp — so we hide those items.
function isContainerId(id){
  const r=lineMap[id]; if(!r) return false;
  const lines=srcEl.value.split('\n');
  for(let i=r[0];i<=r[1]&&i<lines.length;i++){
    if(/^\s*parts:\s*(#.*)?$/.test(lines[i])) return true;
  }
  return false;
}
function showCtx(x,y,kind,key){
  ctxTarget={kind,key};
  // Add node only from the empty canvas; Duplicate leaf nodes only; Delete
  // leaf nodes/edges; canvas itself is edit + add only; containers (lanes) are
  // edit-only — structural ops on them corrupt or wipe a whole group.
  const container = kind==='node' && isContainerId(key);
  document.getElementById('ctxadd').hidden = kind!=='canvas';
  document.getElementById('ctxdup').hidden = kind!=='node' || container;
  document.getElementById('ctxdel').hidden = kind==='canvas' || container;
  ctxmenu.style.left=Math.min(x,innerWidth-160)+'px';
  ctxmenu.style.top=Math.min(y,innerHeight-110)+'px';
  ctxmenu.hidden=false;
}
function hideCtx(){ctxmenu.hidden=true;}
document.addEventListener('mousedown',e=>{ if(!ctxmenu.hidden && !ctxmenu.contains(e.target)) hideCtx(); });
document.addEventListener('scroll',hideCtx,true);
document.getElementById('ctxadd').addEventListener('click',()=>{ opCommit('add',{kind:'node',key:''}); });
document.getElementById('ctxedit').addEventListener('click',()=>{ if(ctxTarget) openDetail(ctxTarget); });
document.getElementById('ctxdup').addEventListener('click',()=>{ if(ctxTarget) opCommit('duplicate',ctxTarget); });
document.getElementById('ctxdel').addEventListener('click',()=>{ if(ctxTarget) opCommit('delete',ctxTarget); });
// Structural op (delete/duplicate): same write-back shape as commitMove.
async function opCommit(op,t){
  hideCtx();
  if(!serverOK) return;
  pushUndo();
  try{
    const r=await fetch('/api/op?op='+op+'&'+qpFor(t)+'&format='+encodeURIComponent(LANG)+projQS(),{method:'POST',body:srcEl.value});
    if(!r.ok) return;
    const data=await r.json();
    if(typeof data.yaml==='string'){ srcEl.value=data.yaml; setDirty(srcEl.value!==ORIGINAL);
      try{localStorage.setItem(DRAFTKEY,srcEl.value);}catch(_){} }
    if(data.svg){ updateModel(data); zoomer.innerHTML=data.svg; buildMap(); paint(pinnedRange()); wireHover(); wireDrag(); adaptStage(); markRendered(); }
    showIssues(data.issues||[]);
  }catch(_){}
}
// deletePinned removes the currently pinned node/edge via the same op as the
// right-click Delete. Containers (group/boundary lanes) are protected, matching
// showCtx + the server-side guard. Returns true if a delete was issued. The pin
// is cleared first since the object is about to disappear.
function deletePinned(){
  if(!serverOK) return false;
  let t=null;
  if(pinId){ if(isContainerId(pinId)) return false; t={kind:'node',key:pinId}; }
  else if(pinCi!=null){ t={kind:'edge',key:pinCi}; }
  if(!t) return false;
  pinId=null; pinCi=null;
  opCommit('delete',t);
  return true;
}
// deleteSelection removes every node in `ids` (the multi-selection, containers
// already filtered out) as ONE undo step, atomically: the deletes chain on a
// LOCAL text variable and the editor/canvas are only updated when every id
// succeeded — a mid-batch failure discards the whole batch (and its undo
// entry), so a partial delete can never be persisted or auto-saved.
async function deleteSelection(ids){
  if(!serverOK||!ids.length) return;
  pushUndo();
  let cur=srcEl.value, data=null;
  try{
    for(const id of ids){
      const r=await fetch('/api/op?op=delete&kind=node&id='+encodeURIComponent(id)+'&format='+encodeURIComponent(LANG)+projQS(),{method:'POST',body:cur});
      if(!r.ok){ undoStack.pop(); return; }
      data=await r.json();
      if(typeof data.yaml==='string') cur=data.yaml;
    }
  }catch(_){ undoStack.pop(); return; }
  clearSelection(); pinId=null; pinCi=null;
  srcEl.value=cur; setDirty(srcEl.value!==ORIGINAL);
  try{localStorage.setItem(DRAFTKEY,srcEl.value);}catch(_){}
  if(data&&data.svg){ updateModel(data); zoomer.innerHTML=data.svg; buildMap(); paint(null); wireHover(); wireDrag(); adaptStage(); markRendered(); }
  showIssues((data&&data.issues)||[]);
}
// true when keyboard focus is in a text field (editor, detail inputs) — there
// Backspace/Delete must edit text, never delete the selected object.
function inEditable(){
  const a=document.activeElement; if(!a) return false;
  return a.tagName==='INPUT'||a.tagName==='TEXTAREA'||a.isContentEditable;
}
/* ── undo / redo of STRUCTURAL edits (drag, detail, delete, duplicate).
   Each commit snapshots the pre-change YAML; ⌘Z/⌘⇧Z restore it. Plain
   typing in the editor keeps the textarea's own native undo. ────────────── */
let undoStack=[], redoStack=[];
function pushUndo(){ undoStack.push(srcEl.value); if(undoStack.length>100) undoStack.shift(); redoStack=[]; }
function applyHistory(v){
  cancelSegmentDrag();
  srcEl.value=v; setDirty(v!==ORIGINAL);
  try{ v===ORIGINAL?localStorage.removeItem(DRAFTKEY):localStorage.setItem(DRAFTKEY,v); }catch(_){}
  buildMap(); paint(pinnedRange()); if(serverOK) rerender();
}
function undo(){ cancelSegmentDrag();if(!undoStack.length) return; redoStack.push(srcEl.value); applyHistory(undoStack.pop()); }
function redo(){ cancelSegmentDrag();if(!redoStack.length) return; undoStack.push(srcEl.value); applyHistory(redoStack.pop()); }
function qpFor(t){
  if(t.kind==='node') return 'kind=node&id='+encodeURIComponent(t.key);
  if(t.kind==='edge') return 'kind=edge&ci='+t.key;
  return 'kind=canvas';
}
/* ── detail form rendering: grouped sections, illustrated choice tiles,
   compact inline rows, and the raw YAML key shown alongside each label so
   the friendly form stays anchored to the source. ─────────────────────── */
function renderFields(fields){
  // Master-detail layout: groups become TABS in a left rail; the selected
  // tab's fields show in the right pane. ALL panes are rendered up front
  // (inactive ones merely hidden), so switching tabs never discards unsaved
  // edits in the others — applyDetail still reads every [data-key] in the DOM.
  fields.forEach((f,i)=>f._id='df_'+i);
  const order=[], by={};
  fields.forEach(f=>{
    const g=f.group||'General';
    if(!(g in by)){ by[g]={name:g, items:[]}; order.push(g); }
    by[g].items.push(f);
  });
  const tabs=order.map((g,i)=>
    '<button type="button" class="df-tab'+(i===0?' on':'')+'" data-tab="t'+i+'">'+esc(g)+'</button>'
  ).join('');
  const panes=order.map((g,i)=>
    '<div class="df-pane" data-pane="t'+i+'"'+(i===0?'':' hidden')+'>'+groupBody(by[g].items)+'</div>'
  ).join('');
  return '<div class="df-tabs"><div class="df-rail">'+tabs+'</div><div class="df-panes">'+panes+'</div></div>';
}
function groupBody(items){
  // Second-level cards: consecutive fields sharing `sub` become one titled,
  // collapsible card. Fields with no sub sit at the top of the tab, no chrome.
  const blocks=[]; let cur=null;
  items.forEach(f=>{
    const sub=f.sub||'';
    if(!cur || cur.sub!==sub){ cur={sub, collapsed:!!f.collapsed, items:[]}; blocks.push(cur); }
    cur.items.push(f);
  });
  return blocks.map(b=>{
    const body=cardInner(b.items);
    if(!b.sub) return body;
    return '<details class="df-card"'+(b.collapsed?'':' open')+'>'+
      '<summary class="df-cardhead">'+esc(b.sub)+'</summary>'+
      '<div class="df-cardbody">'+body+'</div></details>';
  }).join('');
}
function cardInner(items){
  let html='', inlineBuf=[];
  const flush=()=>{ if(inlineBuf.length){ html+='<div class="df-inline">'+inlineBuf.join('')+'</div>'; inlineBuf=[]; } };
  items.forEach(f=>{
    if(f.inline){ inlineBuf.push(cellHTML(f,f._id)); }
    else { flush(); html+=rowHTML(f,f._id); }
  });
  flush();
  return html;
}
function showWhenAttr(f){ return f.showWhen?' data-showwhen="'+escAttr(f.showWhen)+'"':''; }
function rowHTML(f,id){
  return '<div class="df-row"'+showWhenAttr(f)+'>'+
    '<div class="df-labelline"><label class="df-k" for="'+id+'">'+esc(f.label)+'</label><code class="df-path">'+esc(f.key)+'</code></div>'+
    (f.desc?'<div class="df-desc">'+esc(f.desc)+'</div>':'')+
    fieldInput(f,id)+'</div>';
}
function cellHTML(f,id){
  return '<div class="df-cell"'+showWhenAttr(f)+'><label class="df-ck" for="'+id+'" title="'+escAttr(f.key)+'">'+esc(f.label)+'</label>'+fieldInput(f,id)+'</div>';
}
function fieldInput(f,id){
  const key=escAttr(f.key), orig=escAttr(f.value);
  if(f.control==='select') return selectHTML(f,id,key,orig);
  if(f.type==='choice') return choiceHTML(f,key,orig);
  if(f.type==='color')  return colorHTML(f,id,key,orig);
  if(f.type==='icon')   return iconHTML(f,id,key,orig);
  if(f.type==='bool')   return boolHTML(f,id,key,orig);
  if(f.control==='slider') return sliderHTML(f,id,key,orig);
  const t=f.type==='number'?'number':'text';
  // Empty field → muted placeholder of what blank means: the resolved value
  // (effWord+eff) when there is one, else the field's default/off state (empty).
  // Editable value stays empty so an untouched field keeps deferring.
  const ph=f.value?'':(f.eff?effWord(f.key)+' '+f.eff:(f.empty||''));
  return '<input type="'+t+'" id="'+id+'" data-key="'+key+'" data-orig="'+orig+'" value="'+escAttr(f.value)+'" placeholder="'+escAttr(ph)+'">';
}
function sliderHTML(f,id,key,orig){
  const min=(f.min!=null?f.min:0), max=(f.max||100), step=(f.step||1);
  const mid=(Number(min)+Number(max))/2;
  const rv=(f.value!==''&&f.value!=null)?escAttr(f.value):mid;
  const ph=f.value?'':(f.eff?effWord(f.key)+' '+f.eff:(f.empty||''));
  return '<span class="df-slider">'+
    '<input type="range" min="'+min+'" max="'+max+'" step="'+step+'" value="'+rv+'" data-slider="'+id+'">'+
    '<input type="number" min="'+min+'" max="'+max+'" step="'+step+'" id="'+id+'" data-key="'+key+'" data-orig="'+orig+'" value="'+escAttr(f.value)+'" placeholder="'+escAttr(ph)+'" class="df-snum">'+
    (f.unit?'<span class="df-unit">'+esc(f.unit)+'</span>':'')+
    '</span>';
}
function nodeIds(){ return window.interactionModel?window.interactionModel.map(p=>p.id):[]; }
// prependCopyFrom adds a "copy style from another node" picker at the top of the
// node detail form. Copies shape/size + fill/border/effects (not text, icon,
// or placement) from the chosen node into the current form.
function prependCopyFrom(selfId){
  const ids=nodeIds().filter(n=>n!==selfId);
  if(!ids.length) return;
  const bar=document.createElement('div');
  bar.className='df-copybar';
  bar.innerHTML='<span class="df-copylbl">'+esc(L('copy_from'))+'</span>'+ddHTML(ids,'','',null,'',L('copy_pick'));
  bar.querySelector('.df-dd').classList.add('df-copyfrom');
  detailFields.insertBefore(bar, detailFields.firstChild);
}
// insertEdgeArrow drops a "→" between the From and To dropdowns so the
// direction (source → target) is unmistakable.
function insertEdgeArrow(){
  const pane=detailFields.querySelector('.df-pane');
  if(!pane) return;
  const inline=pane.querySelector('.df-inline');
  if(!inline||inline.children.length<2) return;
  const arrow=document.createElement('div');
  arrow.className='df-arrow';
  arrow.textContent='→';
  inline.insertBefore(arrow, inline.children[1]);
}
function copyStyleFrom(srcId){
  fetch('/api/fields?kind=node&id='+encodeURIComponent(srcId)+'&format='+encodeURIComponent(LANG)+'&uilang='+encodeURIComponent(UILANG),{method:'POST',body:srcEl.value})
    .then(r=>r.ok?r.json():null).then(data=>{
      if(!data) return;
      (data.fields||[]).forEach(f=>{
        const p=f.key;
        const copyable=(p==='shape'||p==='preset'||p.indexOf('geom.')===0||(p.indexOf('style.')===0&&p.indexOf('style.text.')!==0));
        if(!copyable) return;
        const ctrl=detailFields.querySelector('[data-key="'+p+'"]');
        if(ctrl) setCtrlVal(ctrl,f.value||'');
      });
    }).catch(()=>{});
}
function setCtrlVal(ctrl,val){
  if(ctrl.classList&&ctrl.classList.contains('df-choice')){
    ctrl.setAttribute('data-val',val);
    ctrl.querySelectorAll('.df-tile').forEach(t=>t.classList.toggle('on',t.getAttribute('data-val')===val));
  } else if(ctrl.type==='checkbox'){ ctrl.checked=(val==='true'); }
  else {
    ctrl.value=val;
    // keep a paired colour swatch / range thumb in sync
    const wrap=ctrl.closest('.df-color,.df-slider');
    if(wrap){ const m=wrap.querySelector('input[type=color],input[type=range]'); if(m&&val) m.value=val; }
  }
  ctrl.dispatchEvent(new Event('input',{bubbles:true}));
}
// Custom dropdown — NO native <select> chrome. Renders a trigger + an absolutely
// positioned option list, both fully CSS-styled. Value lives on data-val.
function ddHTML(idsOrOpts, id, key, orig, cur, placeholder){
  let opts='<div class="df-dd-opt'+(!cur?' on':'')+'" data-val="">'+esc(placeholder||'')+'</div>';
  idsOrOpts.forEach(n=>{ opts+='<div class="df-dd-opt'+(n===cur?' on':'')+'" data-val="'+escAttr(n)+'">'+esc(n)+'</div>'; });
  const curLabel=cur||(placeholder||'');
  return '<div class="df-dd"'+(id?' id="'+id+'"':'')+(key?' data-key="'+key+'"':'')+(orig!=null?' data-orig="'+orig+'"':'')+' data-val="'+escAttr(cur||'')+'">'+
    '<button type="button" class="df-dd-cur">'+esc(curLabel)+'<span class="df-dd-caret"></span></button>'+
    '<div class="df-dd-pop">'+opts+'</div></div>';
}
function selectHTML(f,id,key,orig){
  const ids=(f.options&&f.options.length)?f.options:nodeIds();
  const cur=(f.value||'').split('.')[0]; // node part; drop any .face suffix
  return ddHTML(ids, id, key, orig, cur, L('dd_none'));
}
function choiceHTML(f,key,orig){
  const opts=(f.options||[]).slice(), cur=f.value||'';
  // case-insensitive match; keep an out-of-list / odd-case value as its own tile
  if(cur && !opts.some(o=>o.toLowerCase()===cur.toLowerCase())) opts.unshift(cur);
  const tiles=opts.map(o=>{
    const on=(o.toLowerCase()===cur.toLowerCase())?' on':'';   // '' (solid) matches cur='' too
    return '<button type="button" class="df-tile'+on+'" data-val="'+escAttr(o)+'">'+optGlyph(f.key,o)+'<span>'+esc(optLabel(f.key,o))+'</span></button>';
  }).join('');
  // inherited (no own value) → note the resolved choice so the blank tile isn't misread as "off"
  const hint=(!f.value&&f.eff)?'<div class="df-inherit">'+effWord(f.key)+' '+esc(f.eff)+'</div>':'';
  return '<div class="df-choice" data-key="'+key+'" data-orig="'+orig+'" data-val="'+escAttr(cur)+'">'+tiles+'</div>'+hint;
}
// hint verb for an empty field's resolved value: geometry is solver-computed
// ("auto"), style cascades from shape/preset/theme ("inherits"), and the rest
// (edge routing/stroke) fall back to a fixed render default ("default").
function effWord(key){
  if(key.indexOf('geom.')===0) return L('eff_auto');
  // built-in render defaults (no cascade source) read as "default"
  if(key==='style.text.orient'||key==='style.effects.opacity') return L('eff_default');
  if(key.indexOf('style.')===0) return L('eff_inherits');
  return L('eff_default');
}
function colorHTML(f,id,key,orig){
  // When the field has no own value, the colour is inherited (preset/theme) or
  // a gradient — paint the swatch with the EFFECTIVE colour the server resolved
  // (f.eff) so the picker matches the canvas, and keep the editable value empty
  // so an untouched field keeps inheriting.
  const eff=/^#[0-9a-fA-F]{6}$/.test(f.eff||'')?f.eff:'';
  const hex=/^#[0-9a-fA-F]{6}$/.test(f.value)?f.value:(eff||'#cccccc');
  const ph=f.eff?(effWord(f.key)+' '+f.eff):(f.empty||L('df_unset'));
  return '<span class="df-color"><input type="color" data-sync="'+id+'" value="'+hex+'">'+
    '<input type="text" id="'+id+'" data-key="'+key+'" data-eff="'+escAttr(f.eff||'')+'" data-orig="'+orig+'" value="'+escAttr(f.value)+'" placeholder="'+escAttr(ph)+'"></span>';
}
function iconHTML(f,id,key,orig){
  const FILE=' accept="image/svg+xml,image/png,image/jpeg,image/gif,image/webp,.svg,.png,.jpg,.jpeg,.gif,.webp"';
  if(/^data:/.test(f.value)){
    return '<span class="df-icon"><span class="df-chip">'+esc(L('df_embedded'))+'</span>'+
      '<input type="hidden" id="'+id+'" data-key="'+key+'" data-orig="'+orig+'" value="'+escAttr(f.value)+'">'+
      '<button type="button" class="df-browse" data-pick="'+id+'">'+esc(L('df_replace'))+'</button>'+
      '<button type="button" class="df-clear" data-clear="'+id+'">'+esc(L('df_clear'))+'</button>'+
      '<input type="file" class="df-file" data-pick="'+id+'"'+FILE+' hidden></span>';
  }
  return '<span class="df-icon"><input type="text" id="'+id+'" data-key="'+key+'" data-orig="'+orig+'" value="'+escAttr(f.value)+'" placeholder="'+escAttr(L('df_icon_ph'))+'">'+
    '<button type="button" class="df-browse" data-pick="'+id+'">'+esc(L('df_browse'))+'</button>'+
    '<input type="file" class="df-file" data-pick="'+id+'"'+FILE+' hidden></span>';
}
// optGlyph returns a small inline SVG illustrating an enum value, so the
// choice tiles read at a glance instead of being bare words.
function optLabel(key,val){
  const DASH={'stroke.dash:':'opt_solid','stroke.dash:6 4':'opt_dashed','stroke.dash:1 5':'opt_dotted'};
  if(key.endsWith('.dir')&&val==='') return L('eff_default');
  const dk=DASH[key+':'+val]; if(dk) return L(dk);
  return val || L('opt_none');
}
function optGlyph(key,val){
  const G={
    'stroke.dash:':'<path d="M3 10 17 10"/>',
    'stroke.dash:6 4':'<path d="M3 10 17 10" stroke-dasharray="5 3"/>',
    'stroke.dash:1 5':'<path d="M3 10 17 10" stroke-dasharray="0.5 4"/>',
    'grid:none':'<rect x="3" y="3" width="14" height="14" rx="2"/>',
    'grid:iso':'<path d="M10 3 17 10 10 17 3 10Z"/><path d="M10 7 13 10 10 13 7 10Z"/>',
    'grid:dots':'<circle cx="6" cy="6" r="1.4"/><circle cx="14" cy="6" r="1.4"/><circle cx="10" cy="10" r="1.4"/><circle cx="6" cy="14" r="1.4"/><circle cx="14" cy="14" r="1.4"/>',
    'grid:hatch':'<path d="M3 13 13 3M7 17 17 7"/>',
    'grid:solid':'<rect x="3" y="3" width="14" height="14" rx="2" fill="currentColor" stroke="none"/>',
    'arrow:none':'<path d="M3 10 17 10"/>',
    'arrow:triangle':'<path d="M3 10 12 10"/><path d="M11 6 17 10 11 14Z" fill="currentColor" stroke="none"/>',
    'routing:orthogonal':'<path d="M3 5 10 5 10 15 17 15"/>',
    'routing:straight':'<path d="M3 16 17 4"/>',
    'routing:bezier':'<path d="M3 16 C9 16 11 4 17 4"/>',
    'shape:rectangle':'<path d="M3 8 10 4 17 8 10 12Z"/><path d="M3 8 3 12 10 16 10 12"/><path d="M17 8 17 12 10 16"/>',
    'shape:cylinder':'<ellipse cx="10" cy="6" rx="6" ry="2.4"/><path d="M4 6 4 14 A6 2.4 0 0 0 16 14 L16 6"/>',
    'shape:circle':'<circle cx="10" cy="10" r="6"/>',
    'shape:cloud':'<path d="M6.5 15 A3 3 0 0 1 7 9 A4 4 0 0 1 14.5 10 A2.6 2.6 0 0 1 14 15Z"/>',
    'shape:person':'<circle cx="10" cy="6.5" r="2.6"/><path d="M5 16 A5 5 0 0 1 15 16"/>',
    'shape:prism':'<path d="M10 3 17 8 14.5 16 5.5 16 3 8Z"/>',
    'shape:hexprism':'<path d="M6 4 14 4 17 10 14 16 6 16 3 10Z"/>',
    'shape:triprism':'<path d="M10 3 17 16 3 16Z"/>',
    'shape:octprism':'<path d="M7 3 13 3 17 7 17 13 13 17 7 17 3 13 3 7Z"/>',
    'shape:diamond':'<path d="M10 3 16 10 10 17 4 10Z"/>',
    'shape:group':'<rect x="3" y="3" width="14" height="14" rx="2" stroke-dasharray="3 2"/>',
    'shape:boundary':'<rect x="3" y="3" width="14" height="14" rx="3" stroke-dasharray="3 2.5"/>',
    'shape:text':'<path d="M5 5 15 5M10 5 10 15"/>',
    'shape:array1d':'<rect x="2" y="8" width="4" height="4"/><rect x="8" y="8" width="4" height="4"/><rect x="14" y="8" width="4" height="4"/>',
    'shape:array2d':'<rect x="3" y="3" width="4" height="4"/><rect x="8" y="3" width="4" height="4"/><rect x="13" y="3" width="4" height="4"/><rect x="3" y="8" width="4" height="4"/><rect x="8" y="8" width="4" height="4"/><rect x="13" y="8" width="4" height="4"/><rect x="3" y="13" width="4" height="4"/><rect x="8" y="13" width="4" height="4"/><rect x="13" y="13" width="4" height="4"/>',
    'shape:array3d':'<rect x="6" y="6" width="4" height="4"/><rect x="11" y="6" width="4" height="4"/><rect x="6" y="11" width="4" height="4"/><rect x="11" y="11" width="4" height="4"/><path d="M8 4 17 4 17 13M13 4 13 8"/>',
    'shape:browser-panel':'<rect x="3" y="4" width="14" height="12" rx="1.5"/><path d="M3 8 17 8"/><circle cx="5.6" cy="6" r="0.7" fill="currentColor" stroke="none"/><circle cx="8" cy="6" r="0.7" fill="currentColor" stroke="none"/>',
    'shape:capsule':'<rect x="3" y="6" width="14" height="8" rx="4"/>',
    'shape:cone':'<path d="M10 3 5 14M10 3 15 14"/><ellipse cx="10" cy="14" rx="5" ry="2"/>',
    'shape:custom_path':'<path d="M4 12 C5 5 9 5 10 9 C11 13 15 13 16 7"/>',
    'shape:dome':'<path d="M4 14 A6 6 0 0 1 16 14"/><path d="M3 14 17 14"/>',
    'shape:frustum':'<path d="M6 5 14 5 17 15 3 15Z"/>',
    'shape:pyramid':'<path d="M10 2 4 11 10 14 16 11Z"/><path d="M4 11 10 14 16 11"/>',
    'shape:rack':'<rect x="5" y="3" width="10" height="14" rx="1"/><path d="M5 7 15 7M5 11 15 11M5 14 15 14"/>',
    'shape:screen':'<rect x="3" y="3" width="14" height="10" rx="1"/><path d="M10 13 10 16M7 16 13 16"/>',
    'shape:torus':'<circle cx="10" cy="10" r="6.5"/><circle cx="10" cy="10" r="2.4"/>',
    'shape:wedge':'<path d="M3 15 12 15 12 6Z"/><path d="M12 6 17 8 17 17 12 15"/><path d="M3 15 8 17 17 17"/>'
  };
  // Gradient direction → an actual arrow (matched by VALUE, since the key
  // varies per face: top/left/rightGradient.dir).
  const DIR={
    '':'<path d="M4 10 16 10"/>',
    'down':'<path d="M10 3 10 16M6 12 10 16 14 12"/>',
    'up':'<path d="M10 17 10 4M6 8 10 4 14 8"/>',
    'left':'<path d="M17 10 4 10M8 6 4 10 8 14"/>',
    'right':'<path d="M3 10 16 10M12 6 16 10 12 14"/>',
    'diag':'<path d="M5 5 15 15M15 9 15 15 9 15"/>'
  };
  let inner=G[key+':'+val];
  if(inner===undefined && key.endsWith('.dir')) inner=DIR[val];
  // No meaningful glyph (weight / align / mode / orient / pattern …) → render
  // a label-only tile rather than a row of identical, meaningless circles.
  if(inner===undefined) return '';
  return '<svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">'+inner+'</svg>';
}
function wireDetailInputs(){
  // Tab rail → swap the visible pane (panes stay in the DOM, so edits persist).
  const rail=detailFields.querySelector('.df-rail');
  if(rail) rail.querySelectorAll('.df-tab').forEach(tab=>{
    tab.addEventListener('click',()=>{
      rail.querySelectorAll('.df-tab').forEach(x=>x.classList.remove('on'));
      tab.classList.add('on');
      const id=tab.getAttribute('data-tab');
      detailFields.querySelectorAll('.df-pane').forEach(p=>{ p.hidden=(p.getAttribute('data-pane')!==id); });
    });
  });
  detailFields.querySelectorAll('input[type="color"][data-sync]').forEach(cp=>{
    cp.addEventListener('input',()=>{const t=document.getElementById(cp.getAttribute('data-sync')); if(t)t.value=cp.value;});
  });
  detailFields.querySelectorAll('.df-choice').forEach(ch=>{
    ch.querySelectorAll('.df-tile').forEach(t=>{
      t.addEventListener('click',()=>{
        ch.querySelectorAll('.df-tile').forEach(x=>x.classList.remove('on'));
        t.classList.add('on'); ch.setAttribute('data-val',t.getAttribute('data-val'));
      });
    });
  });
  detailFields.querySelectorAll('.df-browse[data-pick]').forEach(btn=>{
    const pid=btn.getAttribute('data-pick');
    const file=detailFields.querySelector('.df-file[data-pick="'+pid+'"]');
    const text=document.getElementById(pid);
    btn.addEventListener('click',()=>file.click());
    file.addEventListener('change',()=>{const f=file.files&&file.files[0]; if(!f)return; const rd=new FileReader(); rd.onload=()=>{text.value=rd.result;}; rd.readAsDataURL(f);});
  });
  detailFields.querySelectorAll('.df-clear[data-clear]').forEach(b=>{
    b.addEventListener('click',()=>{const t=document.getElementById(b.getAttribute('data-clear')); if(t)t.value='';});
  });
  // Sliders: drag the range, the number box follows (and vice-versa).
  detailFields.querySelectorAll('input[type="range"][data-slider]').forEach(rg=>{
    const t=document.getElementById(rg.getAttribute('data-slider')); if(!t) return;
    rg.addEventListener('input',()=>{ t.value=rg.value; });
    t.addEventListener('input',()=>{ if(t.value!=='') rg.value=t.value; });
  });
  // Conditional rows: show one only when its data-showwhen dependency holds
  // ("path" has any value, or "path=val" matches exactly).
  const cssEsc=(s)=>(window.CSS&&CSS.escape)?CSS.escape(s):s.replace(/["\\]/g,'\\$&');
  const applyShowWhen=()=>{
    detailFields.querySelectorAll('[data-showwhen]').forEach(el=>{
      const cond=el.getAttribute('data-showwhen'); const eq=cond.indexOf('=');
      const path=eq>=0?cond.slice(0,eq):cond; const want=eq>=0?cond.slice(eq+1):null;
      const ctrl=detailFields.querySelector('[data-key="'+cssEsc(path)+'"]');
      let v=ctrl?fieldVal(ctrl):'';
      // presence-gated rows also count an INHERITED value (data-eff), so e.g. a
      // face that gets its gradient from a preset still shows the direction row.
      if(!v && ctrl && want==null) v=ctrl.getAttribute('data-eff')||'';
      const show=want==null?(v!==''&&v!=null):(v===want);
      el.style.display=show?'':'none';
    });
  };
  detailFields.addEventListener('input',applyShowWhen);
  detailFields.addEventListener('click',applyShowWhen);
  applyShowWhen();
  // Custom dropdowns (no native <select>).
  detailFields.querySelectorAll('.df-dd').forEach(wireDropdown);
  if(!detailFields._ddClose){
    detailFields._ddClose=true;
    document.addEventListener('click',e=>{ if(!(e.target.closest&&e.target.closest('.df-dd'))) detailFields.querySelectorAll('.df-dd.open').forEach(d=>d.classList.remove('open')); });
  }
  // copy-from dropdown: copy on pick, then reset its label.
  const cf=detailFields.querySelector('.df-dd.df-copyfrom');
  if(cf) cf.addEventListener('input',()=>{ const v=cf.getAttribute('data-val'); if(v){ copyStyleFrom(v); setDDVal(cf,''); } });
  // Edge from/to must differ: grey out the option the other side picked.
  const fromDD=detailFields.querySelector('.df-dd[data-key="from"]');
  const toDD=detailFields.querySelector('.df-dd[data-key="to"]');
  if(fromDD&&toDD){
    const dv=dd=>dd.getAttribute('data-val');
    const syncFromTo=()=>{
      toDD.querySelectorAll('.df-dd-opt').forEach(o=>{const v=o.getAttribute('data-val'); o.classList.toggle('off', v!==''&&v===dv(fromDD));});
      fromDD.querySelectorAll('.df-dd-opt').forEach(o=>{const v=o.getAttribute('data-val'); o.classList.toggle('off', v!==''&&v===dv(toDD));});
    };
    fromDD.addEventListener('input',()=>{ if(dv(toDD)&&dv(toDD)===dv(fromDD)) setDDVal(toDD,''); syncFromTo(); });
    toDD.addEventListener('input',()=>{ if(dv(fromDD)&&dv(fromDD)===dv(toDD)) setDDVal(fromDD,''); syncFromTo(); });
    syncFromTo();
  }
}
// wireDropdown gives a custom .df-dd open/close + option-select behavior.
function wireDropdown(dd){
  const cur=dd.querySelector('.df-dd-cur');
  cur.addEventListener('click',e=>{ e.stopPropagation(); const open=dd.classList.contains('open');
    detailFields.querySelectorAll('.df-dd.open').forEach(d=>d.classList.remove('open')); if(!open) dd.classList.add('open'); });
  dd.querySelectorAll('.df-dd-opt').forEach(o=>{
    o.addEventListener('click',e=>{ e.stopPropagation();
      if(o.classList.contains('off')) return; // disabled (e.g. equals the other endpoint)
      setDDVal(dd, o.getAttribute('data-val'), o.textContent);
      dd.classList.remove('open');
    });
  });
}
// setDDVal sets a custom dropdown's value, label, selected state, and fires input.
function setDDVal(dd, val, label){
  dd.setAttribute('data-val', val);
  if(label==null){ const m=dd.querySelector('.df-dd-opt[data-val="'+(window.CSS&&CSS.escape?CSS.escape(val):val)+'"]'); label=m?m.textContent:''; }
  const cur=dd.querySelector('.df-dd-cur'); if(cur&&cur.firstChild) cur.firstChild.nodeValue=label||'';
  dd.querySelectorAll('.df-dd-opt').forEach(o=>o.classList.toggle('on', o.getAttribute('data-val')===val));
  dd.dispatchEvent(new Event('input',{bubbles:true}));
}
// a choice tile-group exposes its value via data-val; everything else via .value
// a checkbox carries its bool as checked→"true"/unchecked→"" (empty removes the
// key); a choice tile-group via data-val; everything else via .value
function fieldVal(el){
  if(el.type==='checkbox') return el.checked?'true':'';
  if(el.classList&&(el.classList.contains('df-choice')||el.classList.contains('df-dd'))) return el.getAttribute('data-val');
  return el.value;
}
// boolHTML renders an on/off checkbox. data-orig holds the original value so
// applyDetail only sends it when toggled; checked → value "true", unchecked →
// "" (empty removes the key, reverting to the default).
function boolHTML(f,id,key,orig){
  const on=/^(true|yes|on)$/i.test(f.value||'')?' checked':'';
  return '<input class="df-bool" type="checkbox" id="'+id+'" data-key="'+key+'" data-orig="'+orig+'"'+on+'>';
}
async function openDetail(t){
  hideCtx();
  if(!serverOK) return;
  const qp = qpFor(t);
  try{
    const r=await fetch('/api/fields?'+qp+'&format='+encodeURIComponent(LANG)+'&uilang='+encodeURIComponent(UILANG),{method:'POST',body:srcEl.value});
    if(!r.ok) return;
    const data=await r.json();
    detailTarget=t;
    const fields=data.fields||[];
    if(t.kind==='node'){
      detailTitle.textContent=L('edit_node')+t.key;
    }else if(t.kind==='canvas'){
      detailTitle.textContent=L('edit_canvas');
    }else{
      const fv=k=>{const f=fields.find(x=>x.key===k);return f?f.value:'';};
      detailTitle.textContent=L('edit_edge')+(fv('from')||'?')+' → '+(fv('to')||'?');
    }
    if(!fields.length){
      detailFields.innerHTML='<div class="df-desc" style="padding:8px 0">'+L('no_fields')+'</div>';
    }else{
      detailFields.innerHTML=renderFields(fields);
      if(t.kind==='node') prependCopyFrom(t.key);
      if(t.kind==='edge') insertEdgeArrow();
      wireDetailInputs();
    }
    detailModal.hidden=false;
  }catch(_){}
}
function closeDetail(){detailModal.hidden=true; detailTarget=null;}
detailModal.addEventListener('mousedown',e=>{ if(e.target===detailModal) closeDetail(); });
async function applyDetail(){
  if(!detailTarget||!serverOK) return;
  const t=detailTarget, changes={};
  detailFields.querySelectorAll('[data-key]').forEach(el=>{
    if(el.disabled) return;
    const v=fieldVal(el);
    if(v!==el.getAttribute('data-orig')) changes[el.getAttribute('data-key')]=v;
  });
  if(!Object.keys(changes).length){ closeDetail(); return; }
  pushUndo();
  const qp = qpFor(t);
  try{
    const r=await fetch('/api/edit?'+qp+'&f='+encodeURIComponent(JSON.stringify(changes))+'&format='+encodeURIComponent(LANG)+projQS(),
      {method:'POST',body:srcEl.value});
    if(!r.ok) return;
    const data=await r.json();
    if(typeof data.yaml==='string'){ srcEl.value=data.yaml; setDirty(srcEl.value!==ORIGINAL);
      try{localStorage.setItem(DRAFTKEY,srcEl.value);}catch(_){} }
    if(data.svg){ updateModel(data); zoomer.innerHTML=data.svg; buildMap(); paint(pinnedRange()); wireHover(); wireDrag(); adaptStage(); markRendered(); }
    showIssues(data.issues||[]);
    closeDetail();
  }catch(_){}
}
function wireDrag(){
  zoomer.querySelectorAll('path[data-edge-hit]').forEach(p=>p.remove());
  // Attach unconditionally — probe() may not have resolved at first
  // paint; commitMove() guards on serverOK at drop time instead.
  zoomer.querySelectorAll('g[data-part-id]').forEach(g=>{
    // Hover affordance: 4-way 'move' arrows — clearly distinct from the
    // canvas pan cursor (open-hand 'grab'), so a movable object reads as
    // movable the instant the pointer is over it.
    g.style.cursor='move';
    // Container shapes (boundary/group): hit only the painted silhouette, NOT the
    // rectangular bounding box. The iso diamond's bounding box is far larger than
    // the visible shape and overlaps neighbours, so bounding-box made the pointer
    // over "empty" canvas land inside a container's box and light it up. With
    // visiblePainted only the drawn edges/faces respond.
    if(window.interactionModel){
      const pm=window.interactionModel.find(p=>p.id===g.getAttribute('data-part-id').replace(/~\d+$/,''));
      if(pm&&pm.container) g.setAttribute('pointer-events','visiblePainted');
    }
    g.addEventListener('mousedown',e=>{
      if(e.button!==0) return;   // left-button drags only; right-click = context menu
      e.preventDefault();   // suppress the browser's native SVG image-drag
      e.stopPropagation();  // don't let the viewport start a pan
      g.style.cursor='grabbing';
      document.body.style.cursor='grabbing';  // hold feedback if pointer outruns the element
      g.classList.add('dragging');
      const did=g.getAttribute('data-part-id').replace(/~\d+$/,'');
      // Group move: pressing a node that is IN the multi-selection drags the
      // whole selection by the same delta (committed server-side as one
      // atomic move-multi op → one undo step). Every selected <g> live-follows.
      const group=(selection.size>1&&selection.has(did))?[...selection]:null;
      const set=dragMemberIds(window.interactionModel,group||[did]);
      const els=[];
      zoomer.querySelectorAll('g[data-part-id]').forEach(el=>{
        if(set.has(baseId(el))) els.push({el,base:el.getAttribute('transform')||''});
      });
      // connectors docked to any dragged node, so they can live-follow the
      // drag (an edge with BOTH ends in the set rides along rigidly)
      const edges=[];
      zoomer.querySelectorAll('path[data-connector],path[data-edge-hit]').forEach(p=>{
        const mf=set.has((p.getAttribute('data-from')||'').replace(/~\d+$/,'')), mt=set.has((p.getAttribute('data-to')||'').replace(/~\d+$/,''));
        if(mf||mt) edges.push({el:p,mf,mt,baseD:p.getAttribute('d')});
      });
      nodeDrag={el:g,id:did,ids:group,els,x:e.clientX,y:e.clientY,base:g.getAttribute('transform')||'',edges};
    });
    g.addEventListener('contextmenu',e=>{
      e.preventDefault(); e.stopPropagation();
      showCtx(e.clientX,e.clientY,'node',g.getAttribute('data-part-id').replace(/~\d+$/,''));
    });
    // Double-click = edit details, same as right-click → Edit (a no-move
    // release is below the 2px drag threshold, so it never commits a move).
    g.addEventListener('dblclick',e=>{
      e.preventDefault(); e.stopPropagation();
      openDetail({kind:'node',key:g.getAttribute('data-part-id').replace(/~\d+$/,'')});
    });
  });
  zoomer.querySelectorAll('path[data-connector]').forEach(p=>{
    // Keep the authored stroke width; visible segment handles enlarge the
    // editing target without changing the drawing.
    // Initial cursor — updated dynamically on mousemove below.
    p.style.cursor='grab';
    p.addEventListener('contextmenu',e=>{
      e.preventDefault(); e.stopPropagation();
      showCtx(e.clientX,e.clientY,'edge',p.getAttribute('data-connector'));
    });
    p.addEventListener('dblclick',e=>{
      e.preventDefault(); e.stopPropagation();
      openDetail({kind:'edge',key:p.getAttribute('data-connector')});
    });
    // Dynamic directional cursor: reflect the iso axis the segment will travel on.
    // world-X segment (axisIsX) drags in world-Y → screen direction ↗↙ (nesw-resize)
    // world-Y segment (!axisIsX) drags in world-X → screen direction ↖↘ (nwse-resize)
    function refreshEdgeCursor(cx,cy){
      if(nodeDrag||edgeDrag) return;
      const seg=nearestEditableSegment(p,cx,cy);
      p.style.cursor=seg?(seg.axis===2?'move':seg.axis===0?'nesw-resize':'nwse-resize'):(p.hasAttribute('data-route')?'not-allowed':'grab');
    }
    p.addEventListener('mouseenter',e=>{ refreshEdgeCursor(e.clientX,e.clientY); showSegHandles(p); });
    p.addEventListener('mousemove',e=>{ refreshEdgeCursor(e.clientX,e.clientY); });
    p.addEventListener('mouseleave',e=>{if(!edgeDrag&&!(e.relatedTarget&&e.relatedTarget.getAttribute&&e.relatedTarget.getAttribute('data-seg-handle')===p.getAttribute('data-connector')))removeSegHandles(p);});
    p.addEventListener('mousedown',e=>{
      if(e.button!==0) return;   // left-button drags only; right-click = context menu
      e.preventDefault();
      e.stopPropagation();
      if(nodeDrag||edgeDrag)return;
      const seg=nearestEditableSegment(p,e.clientX,e.clientY);
      if(seg){beginSegmentDrag(p,seg,e);return;}
      // Orthogonal paths require unambiguous 3-D provenance. Never fall back
      // to the old XY-only bend edit when that information is unavailable.
      if(p.hasAttribute('data-route'))return;
      p.style.cursor='grabbing';document.body.style.cursor='grabbing';p.classList.add('dragging');
      edgeDrag={el:p,ci:p.getAttribute('data-connector'),x:e.clientX,y:e.clientY,
        base:p.getAttribute('transform')||'',baseD:p.getAttribute('d')};
    });
    // Invisible hit corridor in the path's own painter layer. It does not
    // thicken the drawing or place hit targets above later-painted nodes.
    const hit=p.cloneNode(false);
    hit.removeAttribute('data-connector');hit.removeAttribute('data-route');hit.removeAttribute('class');hit.removeAttribute('id');
    hit.setAttribute('data-edge-hit',p.getAttribute('data-connector'));
    if(p.getAttribute('data-hit-clip'))hit.setAttribute('clip-path',p.getAttribute('data-hit-clip'));
    hit.setAttribute('stroke','transparent');hit.setAttribute('stroke-width','10');
    hit.setAttribute('vector-effect','non-scaling-stroke');hit.setAttribute('pointer-events','stroke');hit.setAttribute('aria-hidden','true');
    for(const type of ['mouseenter','mouseleave','mousemove','mousedown','click','dblclick','contextmenu']){
      hit.addEventListener(type,e=>{
        if(type==='mouseenter')p.classList.add('edge-hover');
        if(type==='mouseleave')p.classList.remove('edge-hover');
        e.stopPropagation();if(type==='mousedown'||type==='contextmenu')e.preventDefault();
        p.dispatchEvent(new MouseEvent(type,{clientX:e.clientX,clientY:e.clientY,button:e.button,buttons:e.buttons,
          shiftKey:e.shiftKey,altKey:e.altKey,ctrlKey:e.ctrlKey,metaKey:e.metaKey,relatedTarget:e.relatedTarget}));
        hit.style.cursor=p.style.cursor;
      });
    }
    p.after(hit);
  });
  // every re-render funnels through wireDrag — re-apply (and prune) the
  // multi-selection highlight onto the freshly replaced SVG elements.
  pruneSelection();
}

// showSegHandles / removeSegHandles: place small circle affordances at each
// mapped, uncovered segment midpoint. Native hit testing suppresses handles
// under labels, strokes and bodies outside the coverage model.
function showSegHandles(p){
  removeSegHandles(p);
  if(edgeDrag||nodeDrag)return;
  const svg=p.ownerSVGElement,candidates=segmentHitCandidates(p);if(!svg||!candidates)return;
  const NS='http://www.w3.org/2000/svg';
  const accent=getComputedStyle(document.documentElement).getPropertyValue('--accent').trim()||'#10aeb9';
  for(const seg of candidates.editable){
    const mx=(seg.sx+seg.ex)/2,my=(seg.sy+seg.ey)/2;
    if(Math.hypot(seg.ax-seg.bx,seg.ay-seg.by)<14)continue;
    const q={x:(seg.ax+seg.bx)/2,y:(seg.ay+seg.by)/2};
    const chosen=nearestSegmentCandidate(candidates,q.x,q.y);
    if(!chosen||chosen.segment!==seg.segment)continue;
    const hit=document.elementFromPoint(q.x,q.y);
    if(hit!==p&&(!hit||hit.getAttribute('data-edge-hit')!==p.getAttribute('data-connector')))continue; // native painter/overlay hit
    const c=document.createElementNS(NS,'circle');
    c.setAttribute('cx',mx);c.setAttribute('cy',my);c.setAttribute('r','5');
    c.setAttribute('fill',accent);c.setAttribute('stroke','#fff');c.setAttribute('stroke-width','1.5');
    c.setAttribute('data-seg-handle',p.getAttribute('data-connector')||'');c.classList.add('seg-handle');
    c.style.cursor=seg.axis===2?'move':seg.axis===0?'nesw-resize':'nwse-resize';
    c.addEventListener('mouseleave',e=>{if(!edgeDrag&&e.relatedTarget!==p&&!(e.relatedTarget&&e.relatedTarget.getAttribute&&e.relatedTarget.getAttribute('data-edge-hit')===p.getAttribute('data-connector')))removeSegHandles(p);});
    c.addEventListener('mousedown',e=>{if(e.button!==0)return;e.preventDefault();e.stopPropagation();beginSegmentDrag(p,seg,e);});
    // A handle lives above the path. Retain it when the pointer leaves the
    // path for this handle; ordinary canvas moves clear abandoned handles.
    svg.appendChild(c);requestAnimationFrame(()=>c.classList.add('visible'));
  }
}
function removeSegHandles(p){
  const ci=p.getAttribute('data-connector')||'';
  const svg=p.ownerSVGElement; if(!svg) return;
  svg.querySelectorAll(`circle[data-seg-handle="${CSS.escape(ci)}"]`).forEach(c=>c.remove());
}
// Live follow by COMPOSING with the element's existing transform
// attribute. A node <g> already carries transform="translate(x y)";
// setting CSS style.transform would OVERRIDE that attribute in SVG and
// teleport the node to the origin — the real-browser "completely
// unusable" bug. So we add the drag delta to the attribute's own
// translate instead. Lengths are SVG user units → screen px / scale.
function liveTranslate(el, base, dx, dy){
  const m = base.match(/translate\(\s*(-?[\d.]+)[ ,]+(-?[\d.]+)\s*\)/);
  if(m){
    const nx=parseFloat(m[1])+dx, ny=parseFloat(m[2])+dy;
    el.setAttribute('transform', base.slice(0,m.index)+'translate('+nx+' '+ny+')'+base.slice(m.index+m[0].length));
  }else{
    el.setAttribute('transform', ('translate('+dx+' '+dy+') ')+base);
  }
}
// CTM inversion includes viewBox fitting and CSS zoom/pan, not just the
// Studio zoom slider. Preview and committed world deltas share this transform.
function dragUserDelta(d,e){
  const a=clientToSvgUser(d.x,d.y),b=clientToSvgUser(e.clientX,e.clientY);
  return [b[0]-a[0],b[1]-a[1]];
}
window.addEventListener('mousemove',e=>{
  const d=nodeDrag||edgeDrag; if(!d) return;
  if(d.segmentEdit){if(!d.released)requestSegmentPreview(d,e,false);return;}
  const [dx,dy]=dragUserDelta(d,e);
  if(nodeDrag && d.els) d.els.forEach(n=>liveTranslate(n.el, n.base, dx, dy)); // group: all selected ride
  else liveTranslate(d.el, d.base, dx, dy);
  // node drag: docked connector endpoints track the node in real time
  if(nodeDrag && d.edges) d.edges.forEach(ed=>ed.el.setAttribute('d', shiftEnds(ed.baseD, ed.mf, ed.mt, dx, dy)));
  // live drop-target feedback: highlight the group the node would land in, so
  // it's obvious BEFORE releasing whether the re-home was recognised.
  // (single drags only — a group move keeps every node's current parent)
  if(nodeDrag && !d.ids) highlightDropTarget(containerUnder(e.clientX,e.clientY,d.id));
});
// Mark one group slab as the active drop target (clears any previous one).
let dropTargetId=null;
function highlightDropTarget(id){
  if(id===dropTargetId) return;
  dropTargetId=id;
  zoomer.querySelectorAll('g[data-part-id].drop-target').forEach(g=>g.classList.remove('drop-target'));
  if(id){
    const g=zoomer.querySelector('g[data-part-id="'+(window.CSS&&CSS.escape?CSS.escape(id):id)+'"]');
    if(g) g.classList.add('drop-target');
  }
}
window.addEventListener('mouseup',e=>{
  const d=nodeDrag||edgeDrag, kind=nodeDrag?'node':(edgeDrag?'edge':null);
  highlightDropTarget(null);   // clear the drop-target glow regardless of outcome
  if(!kind) return;
  if(d.segmentEdit){requestSegmentPreview(d,e,true);return;}
  nodeDrag=null; edgeDrag=null;
  // restore the pre-drag transform; commit re-renders the SVG fresh
  if(d.els) d.els.forEach(n=>n.el.setAttribute('transform', n.base));
  else d.el.setAttribute('transform', d.base);
  if(d.baseD!=null) d.el.setAttribute('d', d.baseD);
  // restore live-followed edges; the commit re-render replaces them cleanly,
  // and a below-threshold release leaves nothing changed.
  if(d.edges) d.edges.forEach(ed=>ed.el.setAttribute('d', ed.baseD));
  d.el.classList.remove('dragging'); d.el.style.cursor=(kind==='edge'?'grab':'move'); document.body.style.cursor='';
  if(kind==='edge') removeSegHandles(d.el);
  const wd=screenToWorldDelta(...dragUserDelta(d,e));
  if(Math.abs(wd[0])<=2 && Math.abs(wd[1])<=2) return;   // below drag threshold
  // A completed drag's trailing click lands on the re-rendered canvas (the
  // node under the cursor was just replaced), which would clearSelection()
  // and force a re-marquee before every subsequent group move. Same guard
  // the marquee uses: swallow exactly one trailing click.
  marqueeJustEnded=true;
  if(kind==='node' && d.ids){
    // Group move: ONE atomic server round-trip moves the whole selection by
    // the same delta (relative positions preserved) = ONE undo step. No
    // reparent pass — every node keeps its current container.
    commitMove(kind, d.id, Math.round(wd[0]), Math.round(wd[1]), e.clientX, e.clientY, null, d.ids);
  }else if(kind==='node'){
    // Position first, then re-home: drop onto a group's slab joins it; drop on
    // bare canvas returns it to the scene root. The server no-ops the reparent
    // when the parent is unchanged, so an in-group nudge keeps its offset.
    const target=containerUnder(e.clientX, e.clientY, d.id);
    commitMove(kind, d.id, Math.round(wd[0]), Math.round(wd[1]), e.clientX, e.clientY)
      .then(ok=>{if(ok) return reparentNode(d.id, target||'', false);});
  }else{
    commitMove(kind, d.ci, Math.round(wd[0]), Math.round(wd[1]), e.clientX, e.clientY);
  }
});

// containerUnder returns the id of the container the drag cursor landed
// in, or null (drop to scene root).
//
// Algorithm: inverse-project the cursor's screen position (cx, cy) to world
// coordinates, then find the innermost (smallest-AABB) container whose world
// AABB contains that point.
//
// Why cursor-position rather than node-AABB overlap:
//   Drag-OUT: once the cursor leaves the container's world footprint the node
//   escapes — even if the node's own footprint still straddles the boundary.
//   The previous 5%-overlap approach failed whenever a group's AABB covered
//   the entire viewport (e.g. an auto-sized boundary with many children spread
//   across the scene), making it impossible to drag a child out at all.
//   Drag-IN to small container: cursor precision is sufficient; users aim the
//   cursor at the target group, not the node's centroid.
//   Nested containers: the innermost container wins (smallest area containing
//   the cursor point), without any extra bookkeeping.
//
// Fallback (interactionModel null, e.g. d2 file or stale server):
// pixel hit-test via elementsFromPoint.
function containerUnder(cx, cy, exceptId){
  if(window.interactionModel){
    // Inverse iso projection: screen (cx,cy) → world (curWx, curWy).
    //
    // The SVG is positioned by CSS flex layout (not by panX/panY alone), so we
    // can't use stage.left/top + panX/panY as the coordinate origin. Instead we
    // use the SVG element's getScreenCTM() which accounts for all CSS transforms,
    // flex positioning, and the viewBox, plus the data-scene-tx/ty attributes that
    // the renderer embeds to record where world (0,0) sits in SVG user space.
    //
    //   SVG user (u,v)  =  (wx-wy)*C30 + sceneTx,  (wx+wy)*S30 + sceneTy
    //   screen (cx,cy)  =  m.a*u + m.e,  m.d*v + m.f   (m = getScreenCTM)
    //
    // Rearranging: u = (cx - m.e)/m.a,  v = (cy - m.f)/m.d
    //              dsx = u - sceneTx,  dsy = v - sceneTy
    //              [curWx, curWy] = screenToWorldDelta(dsx, dsy)
    const svg = zoomer.querySelector('svg');
    if(svg){
      const m = svg.getScreenCTM();
      if(m && m.a){
        const sceneTx = parseFloat(svg.dataset.sceneTx||'0');
        const sceneTy = parseFloat(svg.dataset.sceneTy||'0');
        const [ux,uy]=clientToSvgUser(cx,cy);
        const dsx=ux-sceneTx, dsy=uy-sceneTy;
        const [curWx,curWy] = screenToWorldDelta(dsx, dsy);
        let bestId=null, bestArea=Infinity;
        const moving=dragMemberIds(window.interactionModel,[exceptId]);
        for(const pm of window.interactionModel){
          if(!pm.container||moving.has(pm.id)) continue;
          if(curWx>=pm.x&&curWx<=pm.x+pm.w&&curWy>=pm.y&&curWy<=pm.y+pm.d){
            const area=pm.w*pm.d;
            if(area<bestArea){bestArea=area;bestId=pm.id;}
          }
        }
        return bestId;
      }
    }
  }
  // Pixel fallback: walk the paint stack at the cursor.
  for(const el of document.elementsFromPoint(cx, cy)){
    const g=el.closest && el.closest('g[data-part-id]');
    if(!g) continue;
    const id=g.getAttribute('data-part-id').replace(/~\d+$/,'');
    if(id===exceptId) continue;
    if(isContainerId(id)) return id;
  }
  return null;
}
async function reparentNode(id, target, recordUndo=true){
  if(!serverOK) return;
  const pm=window.interactionModel&&window.interactionModel.find(p=>p.id===id);
  if(pm&&(pm.parentId||'')===target) return;
  const frame=captureSceneFrame();
  try{
    const r=await fetch('/api/op?op=reparent&id='+encodeURIComponent(id)+'&target='+encodeURIComponent(target)+'&format='+encodeURIComponent(LANG)+projQS(),{method:'POST',body:srcEl.value});
    if(!r.ok) return;
    const data=await r.json();
    if(!data.svg||typeof data.yaml!=='string') return;
    if(recordUndo) pushUndo();
    if(typeof data.yaml==='string'){ srcEl.value=data.yaml; setDirty(srcEl.value!==ORIGINAL); try{localStorage.setItem(DRAFTKEY,srcEl.value);}catch(_){} }
    if(data.svg){ updateModel(data); zoomer.innerHTML=data.svg; buildMap(); paint(pinnedRange()); wireHover(); wireDrag(); adaptStage(); markRendered(); restoreSceneFrame(frame); }
  }catch(_){}
}

/* reverse mapping: caret inside a part's block lights the node up */
function caretSync(){
  const line=srcEl.value.slice(0,srcEl.selectionStart).split('\n').length-1;
  let hitId=null;
  for(const id in lineMap){
    const r=lineMap[id];
    if(line>=r[0]&&line<=r[1]){hitId=id;break;}
  }
  if(!pinId && pinCi==null) glowOnly(hitId);
}
srcEl.addEventListener('keyup',caretSync);
srcEl.addEventListener('click',caretSync);

/* ── zoom & pan ─────────────────────────────────────────────────── */
let scale=1,panX=0,panY=0;
function apply(){
  zoomer.style.transform='translate('+panX+'px,'+panY+'px) scale('+scale+')';
  const z=document.getElementById('zpct');
  if(z) z.textContent=Math.round(scale*100)+'%';
  // Handles live inside the SVG, so pan/zoom moves them with the nodes for
  // free; only their on-screen SIZE needs correcting after a zoom.
  rescaleHandles();
}
function zoomBy(f){
  const r=viewport.getBoundingClientRect();
  const cx=r.width/2, cy=r.height/2;
  const ns=Math.min(8,Math.max(0.2,scale*f));
  const eff=ns/scale;
  panX=cx-(cx-panX)*eff; panY=cy-(cy-panY)*eff;
  scale=ns; apply();
}
function resetView(){scale=1;panX=0;panY=0;apply();}
function fitView(){
  const svg=zoomer.querySelector('svg'); if(!svg) return;
  const r=viewport.getBoundingClientRect();
  const w=parseFloat(svg.getAttribute('width'))||svg.viewBox.baseVal.width;
  const h=parseFloat(svg.getAttribute('height'))||svg.viewBox.baseVal.height;
  if(!w||!h) return;
  const s=Math.min((r.width-72)/w,(r.height-72)/h);
  scale=Math.min(4,Math.max(0.2,s));
  panX=w*(1-scale)/2; panY=h*(1-scale)/2;
  apply();
}
viewport.addEventListener('wheel',e=>{
  e.preventDefault();
  if(edgeDrag&&edgeDrag.segmentEdit)return;
  const f=e.deltaY<0?1.1:0.9, r=viewport.getBoundingClientRect();
  const mx=e.clientX-r.left, my=e.clientY-r.top;
  panX=mx-(mx-panX)*f; panY=my-(my-panY)*f;
  scale=Math.min(8,Math.max(0.2,scale*f)); apply();
},{passive:false});
/* ── empty-canvas gestures ─────────────────────────────────────────
   plain left-drag  = marquee box selection (semi-transparent frame; a node
                      is hit when its projected body/content region intersects the box;
                      hold shift to ADD to the current selection)
   alt/middle-drag  = pan (the old plain-drag pan, kept on a modifier)
   plain click      = clear pin + selection · double-click = reset view
   Node/edge/handle mousedowns stopPropagation, so these fire on bare canvas only. */
let drag=null, marquee=null, marqueeJustEnded=false;
// The swallow-one-click guard protects against the TRAILING click of the
// gesture that armed it — but that click can be eaten by a node handler's
// stopPropagation and never reach the viewport. A fresh mousedown means a
// NEW user gesture, so any stale guard must die with it.
window.addEventListener('mousedown',e=>{if(edgeDrag&&edgeDrag.segmentEdit){e.preventDefault();e.stopImmediatePropagation();return;}marqueeJustEnded=false;},true);
viewport.addEventListener('mousedown',e=>{
  if(e.button===0 && !e.altKey){
    marquee={x0:e.clientX,y0:e.clientY,el:null,rect:null,add:e.shiftKey};
    e.preventDefault();
    return;
  }
  if(e.button!==0 && e.button!==1) return;
  e.preventDefault();
  drag={x:e.clientX-panX,y:e.clientY-panY};viewport.classList.add('panning');
});
window.addEventListener('mousemove',e=>{if(!drag)return;panX=e.clientX-drag.x;panY=e.clientY-drag.y;apply();});
window.addEventListener('mouseup',()=>{drag=null;viewport.classList.remove('panning');});
window.addEventListener('mousemove',e=>{
  if(!marquee) return;
  if(!marquee.el){
    // below 4px it's still a click; don't flash a 1px box on every click
    if(Math.abs(e.clientX-marquee.x0)<4 && Math.abs(e.clientY-marquee.y0)<4) return;
    const el=document.createElement('div');
    el.className='marquee';
    viewport.appendChild(el);
    marquee.el=el;
  }
  const r={left:Math.min(marquee.x0,e.clientX), top:Math.min(marquee.y0,e.clientY),
           right:Math.max(marquee.x0,e.clientX), bottom:Math.max(marquee.y0,e.clientY)};
  marquee.rect=r;
  const vp=viewport.getBoundingClientRect();
  marquee.el.style.left=(r.left-vp.left)+'px';
  marquee.el.style.top=(r.top-vp.top)+'px';
  marquee.el.style.width=(r.right-r.left)+'px';
  marquee.el.style.height=(r.bottom-r.top)+'px';
  // live preview: highlight what a release right now would select
  const hits=new Set(marqueeHits(r));
  if(marquee.add) for(const id of selection) hits.add(id);
  zoomer.querySelectorAll('g[data-part-id]').forEach(g=>g.classList.toggle('sel',hits.has(baseId(g))));
});
window.addEventListener('mouseup',e=>{
  if(!marquee) return;
  const mq=marquee; marquee=null;
  if(!mq.el) return;               // no box drawn → plain click; the click handler clears
  mq.el.remove();
  const hits=marqueeHits(mq.rect);
  if(mq.add) hits.forEach(id=>selection.add(id));
  else selection=new Set(hits);
  applySelectionClasses();
  marqueeJustEnded=true;           // the trailing click must not clear what we just selected
});
viewport.addEventListener('dblclick',resetView);
viewport.addEventListener('click',e=>{
  // Consume the swallow-one-click guard FIRST, whatever the click landed on:
  // a marquee/drag whose trailing click hits a node would otherwise leave the
  // flag armed and silently eat the user's next real canvas click.
  const swallow=marqueeJustEnded; marqueeJustEnded=false;
  if(e.target.closest && e.target.closest('g[data-part-id]')) return;
  if(swallow) return;
  clearSelection();
  if(pinId!==null||pinCi!==null){pinId=null;pinCi=null;glowOnly(null);glowEdge(null);paint(null);}
});
// Right-click the empty canvas → edit the whole-image background/grid. Node
// and edge contextmenu handlers stopPropagation, so this fires only on bare
// canvas (or a node/edge face that isn't draggable, which is fine).
viewport.addEventListener('contextmenu',e=>{
  if(e.target.closest && (e.target.closest('g[data-part-id]')||e.target.closest('path[data-connector]'))) return;
  e.preventDefault();
  showCtx(e.clientX,e.clientY,'canvas','');
});

/* ── adaptive stage: tint the backdrop after the scene's canvas ── */
function hexRGB(c){
  if(!c) return null;
  c=c.trim();
  let m=c.match(/^#([0-9a-f]{3})$/i);
  if(m) return [parseInt(m[1][0]+m[1][0],16),parseInt(m[1][1]+m[1][1],16),parseInt(m[1][2]+m[1][2],16)];
  m=c.match(/^#([0-9a-f]{6})/i);
  if(m) return [parseInt(m[1].slice(0,2),16),parseInt(m[1].slice(2,4),16),parseInt(m[1].slice(4,6),16)];
  return null;
}
function adaptStage(){
  const st=document.getElementById('stage');
  const r=zoomer.querySelector('rect[data-layer="canvas-bg"]');
  const rgb=r?hexRGB(r.getAttribute('fill')):null;
  if(!rgb){st.style.removeProperty('--stage-bg');st.style.removeProperty('--stage-dot');return;}
  const lum=(0.2126*rgb[0]+0.7152*rgb[1]+0.0722*rgb[2])/255;
  if(lum<0.45){
    const mix=rgb.map(v=>Math.round(v*0.82));
    st.style.setProperty('--stage-bg','rgb('+mix.join(',')+')');
    st.style.setProperty('--stage-dot','rgba(255,255,255,.07)');
  }else{
    const mix=rgb.map(v=>Math.round(v*0.96));
    st.style.setProperty('--stage-bg','rgb('+mix.join(',')+')');
    st.style.setProperty('--stage-dot','rgba(15,23,42,.08)');
  }
}

/* ── issues → source navigation ─────────────────────────────────── */
function pathToLine(path){
  const segs=String(path||'').replace(/\[(\d+)\]/g,'.$1').split('.').filter(Boolean);
  const lines=srcEl.value.split('\n');
  let pos=0, indent=-1;
  for(const sg of segs){
    if(/^\d+$/.test(sg)){
      let n=+sg, itemIndent=-1, found=-1;
      for(let i=pos+1;i<lines.length;i++){
        const t=lines[i]; if(!t.trim()) continue;
        const ind=t.search(/\S/);
        if(ind<=indent) break;
        if(/^\s*-(\s|$)/.test(t)){
          if(itemIndent<0) itemIndent=ind;
          if(ind===itemIndent){ if(n===0){found=i;break;} n--; }
        }
      }
      if(found<0) return pos;
      pos=found; indent=lines[found].search(/\S/);
      continue;
    }
    const re=new RegExp('^\\s*(?:- )?'+sg.replace(/[.*+?^${}()|[\]\\]/g,'\\$&')+'\\s*:');
    let found=-1;
    for(let i=pos;i<lines.length;i++){
      if(re.test(lines[i])){found=i;break;}
    }
    if(found<0) return pos;
    pos=found; indent=lines[found].search(/\S/);
  }
  return pos;
}
function jumpToIssue(path){
  const ln=pathToLine(path);
  flashLine=ln;
  paint(pinnedRange());
  scrollToLine(ln);
  setTimeout(()=>{flashLine=-1;paint(pinnedRange());},1500);
}

/* ── live re-render against isotopo serve ───────────────────────── */
const liveEl=document.getElementById('live'), renderBtn=document.getElementById('render');
document.getElementById('auto').addEventListener('change',e=>{
  if(!e.target.checked) clearTimeout(timer);
});
let serverOK=false, timer=null;
// One consolidated status (top-right). It replaces the old renderer pill AND
// the file-tab dirty dot: it reports connection + freshness in one place,
// stamping the time of the last successful render so the user can see how
// current the canvas is (auto-render is on by default).
function nowTime(){const d=new Date(),p=n=>String(n).padStart(2,'0');return p(d.getHours())+':'+p(d.getMinutes())+':'+p(d.getSeconds());}
function setStatus(cls,text,title){liveEl.className=cls;liveEl.textContent=text;liveEl.title=title||'';}
function markRendered(){setStatus('ok',L('rendered')+' '+nowTime(),L('t_insync'));}
async function probe(){
  const ovBtn=document.getElementById('overwrite');
  if(!location.protocol.startsWith('http')){
    setStatus('',L('static_file'),L('t_static'));
    renderBtn.disabled=true; if(ovBtn) ovBtn.disabled=true; return;
  }
  const was=serverOK;
  try{
    const r=await fetch('/api/ping');
    serverOK=r.ok;
  }catch(_){serverOK=false;}
  renderBtn.disabled=!serverOK; if(ovBtn) ovBtn.disabled=!serverOK;
  if(!serverOK){
    setStatus('err',L('offline'),L('t_offline'));
  }else if(!was){
    markRendered(); // recovered (or first probe): the shown canvas is current
  }
}
// View mode: 'iso' (2.5D, default) or 'top' (flat plan). It's a PREVIEW
// override sent as ?projection= on every render/edit round-trip — the source
// the editor holds is never rewritten, so toggling back is lossless.
let VIEWMODE=isTopView()?'top':'iso',renderVersion=0;
function projQS(){ return '&projection='+VIEWMODE; }
function toggleView(){
  cancelSegmentDrag();
  VIEWMODE = VIEWMODE==='top' ? 'iso' : 'top';
  const b=document.getElementById('viewtoggle');
  if(b){ b.textContent = VIEWMODE==='top' ? L('view_plan') : L('view_iso'); b.classList.toggle('on', VIEWMODE==='top'); }
  if(serverOK) rerender();
}
async function rerender(){
  if(!serverOK) return;
  cancelSegmentDrag();
  const source=srcEl.value,projection=VIEWMODE,version=++renderVersion;
  renderBtn.textContent=L('rendering');
  setStatus('',L('rendering'),'');
  try{
    const r=await fetch('/api/render?format='+encodeURIComponent(LANG)+projQS(),{method:'POST',body:source});
    const data=await r.json();
    if(version!==renderVersion||source!==srcEl.value||projection!==VIEWMODE)return;
    showIssues(data.issues||[]);
    if(data.svg){
      updateModel(data);
      zoomer.innerHTML=data.svg;
      buildMap(); paint(pinnedRange()); wireHover(); wireDrag(); adaptStage();
      staleEl.hidden=true;
      markRendered();
    }else{
      staleEl.hidden=false;
      setStatus('err',L('render_failed'),L('t_render_failed'));
    }
  }catch(e){
    showIssues([{severity:'error',path:'$',message:String(e)}]);
    staleEl.hidden=false;
    probe();
  }
  renderBtn.textContent=L('render');
}
function showIssues(list){
  const el=document.getElementById('issues');
  if(!list.length){el.classList.remove('show');el.innerHTML='';return;}
  el.classList.add('show');
  el.innerHTML=list.map(i=>'<div class="'+(i.severity==='error'?'err':'warn')+'" data-path="'+esc(i.path||'')+'" title="'+esc(L('t_jump'))+'">'+
    esc(i.severity+' '+(i.path||'')+' — '+i.message+(i.suggest?' (did you mean '+i.suggest+'?)':''))+'</div>').join('');
  el.querySelectorAll('[data-path]').forEach(d=>{
    d.addEventListener('click',()=>jumpToIssue(d.getAttribute('data-path')));
  });
}
srcEl.addEventListener('input',()=>{
  cancelSegmentDrag();
  buildMap(); paint(null);
  lastTypeTime=Date.now();   // debounce autosave while keystrokes are in flight
  const edited=srcEl.value!==ORIGINAL;
  setDirty(edited);
  try{
    if(edited) localStorage.setItem(DRAFTKEY,srcEl.value);
    else localStorage.removeItem(DRAFTKEY);
  }catch(_){}
  if(document.getElementById('auto').checked && serverOK){
    clearTimeout(timer); timer=setTimeout(()=>{if(document.getElementById('auto').checked)rerender();},600);
  }
});

/* ── live file binding ──────────────────────────────────────────────
   Studio is bound to the source file on disk: edits in the UI auto-save to
   it, and external edits to the file are pulled back into the UI. One loop
   does both — push when the editor diverges from disk, else pull when disk
   diverges from the editor. `lastSynced` is the content we believe is on disk;
   comparing against it makes our own saves not read as external changes. */
let lastSynced=ORIGINAL, lastTypeTime=0, syncing=false;
async function syncTick(){
  if(syncing || !serverOK || !location.protocol.startsWith('http')) return;
  const cur=srcEl.value;
  syncing=true;
  try{
    if(cur!==lastSynced){
      // local edits → write to disk, but wait out an in-flight burst of typing
      if(Date.now()-lastTypeTime < 600) return;
      const r=await fetch('/api/save?format='+encodeURIComponent(LANG),{method:'POST',body:cur});
      if(r.ok){
        lastSynced=cur; ORIGINAL=cur; setDirty(false);
        try{localStorage.removeItem(DRAFTKEY);}catch(_){}
        setStatus('ok',L('saved')+' '+nowTime(),L('t_autosaved'));
      } // parse error (!ok) → leave the file untouched; the issues panel shows why
      return;
    }
    // editor matches disk-as-we-know-it → poll the file for external changes
    const r=await fetch('/api/source'); if(!r.ok) return;
    const disk=await r.text();
    if(disk!==lastSynced){
      lastSynced=disk; ORIGINAL=disk; srcEl.value=disk; setDirty(false);
      pendingDraft=null; try{localStorage.removeItem(DRAFTKEY);}catch(_){}
      const r2=document.getElementById('restore'); if(r2) r2.hidden=true;
      buildMap(); paint(pinnedRange());
      if(serverOK) rerender();
      setStatus('',L('updated_from_file')+' '+nowTime(),PATH);
    }
  }catch(_){}
  finally{ syncing=false; }
}
setInterval(syncTick,1000);
window.addEventListener('keydown',e=>{
  // ⌘Z / ⌘⇧Z undo-redo of structural edits — only when not typing in the
  // editor (the textarea keeps its own native text undo).
  if((e.metaKey||e.ctrlKey)&&(e.key==='z'||e.key==='Z')&&document.activeElement!==srcEl){
    e.preventDefault(); e.shiftKey?redo():undo(); return;
  }
  if((e.metaKey||e.ctrlKey)&&e.key==='Enter'){e.preventDefault();rerender();return;}
  if((e.metaKey||e.ctrlKey)&&e.key==='0'){e.preventDefault();fitView();return;}
  if((e.metaKey||e.ctrlKey)&&(e.key==='='||e.key==='+')){e.preventDefault();zoomBy(1.25);return;}
  if((e.metaKey||e.ctrlKey)&&e.key==='-'){e.preventDefault();zoomBy(0.8);return;}
  if(e.key==='Escape'&&edgeDrag&&edgeDrag.segmentEdit){e.preventDefault();cancelSegmentDrag();return;}
  if(e.key==='Escape'){document.getElementById('help').hidden=true;hideCtx();closeDetail();clearSelection();return;}
  // Delete / Backspace removes the multi-selection (one atomic batch = one
  // undo step) when there is one, else the pinned object — but only when not
  // typing in a text field and no modal is open, so it never eats a backspace.
  if((e.key==='Delete'||e.key==='Backspace')&&!inEditable()&&detailModal.hidden){
    if(selection.size>0){
      const ids=[...selection].filter(id=>!isContainerId(id));
      if(ids.length){ e.preventDefault(); deleteSelection(ids); return; }
    }
    if((pinId||pinCi!=null)&&deletePinned()){ e.preventDefault(); return; }
  }
  if(e.key==='?'&&document.activeElement!==srcEl){toggleHelp();}
});
function toggleHelp(){
  const h=document.getElementById('help');
  h.hidden=!h.hidden;
}
document.getElementById('help').addEventListener('click',e=>{
  if(e.target.id==='help') e.target.hidden=true;
});
srcEl.addEventListener('keydown',e=>{
  if(e.key==='Tab'&&!e.shiftKey){
    e.preventDefault();
    srcEl.setRangeText('  ',srcEl.selectionStart,srcEl.selectionEnd,'end');
    srcEl.dispatchEvent(new Event('input'));
  }
});

/* ── path display: middle-truncate by segment to fit ───────────── */
const fpathEl=document.getElementById('fpath');
function renderPath(){
  const segs=PATH.split('/').filter(Boolean);
  segs.pop(); // basename is rendered separately in bold
  fpathEl.title=PATH;
  const set=k=>{
    if(k>=segs.length){fpathEl.textContent='/'+segs.map(x=>x+'/').join('');return;}
    if(k<=0){fpathEl.textContent='…/';return;}
    const nt=Math.ceil(k/2), nh=k-nt;
    const head=segs.slice(0,nh).map(x=>x+'/').join('');
    const tail=segs.slice(-nt).map(x=>x+'/').join('');
    fpathEl.textContent=(nh>0?'/'+head:'')+'…/'+tail;
  };
  let k=segs.length;
  set(k);
  while(k>0 && fpathEl.scrollWidth>fpathEl.clientWidth){k--;set(k);}
}
window.addEventListener('resize',renderPath);

/* ── editor panel toggle (collapse/expand) ──────────────────────── */
const gridEl=document.getElementById('grid');
const sideEl=document.querySelector('.side'), splitEl=document.getElementById('split');
const toggleBtn=document.getElementById('paneltoggle');
let panelOpen=false;
function updateToggleIcon(){
  // A labeled toggle, not a bare arrow: shows what it controls (the code editor)
  // and whether it's on. "</>" glyph + word + a chevron that reflects state.
  toggleBtn.innerHTML='<span class="pt-ico">&lt;/&gt;</span><span class="pt-txt">'+esc(L('code'))+'</span><span class="pt-chev">'+(panelOpen?'▸':'◂')+'</span>';
  toggleBtn.classList.toggle('on',panelOpen);
  toggleBtn.title=panelOpen?L('panel_hide'):L('panel_show');
}
function setPanel(open,save){
  panelOpen=open;
  sideEl.classList.toggle('collapsed',!open);
  splitEl.style.display=open?'':'none';
  gridEl.classList.toggle('collapsed',!open);
  updateToggleIcon();
  if(save){try{localStorage.setItem('isotopo-panel',open?'1':'0');}catch(_){}}
}
function togglePanel(){setPanel(!panelOpen,true);}
window.addEventListener('keydown',e=>{
  if((e.metaKey||e.ctrlKey)&&e.key==='e'){e.preventDefault();togglePanel();}
});
(function(){
  let v='0';
  try{v=localStorage.getItem('isotopo-panel')??'0';}catch(_){}
  setPanel(v==='1',false);
})();

/* ── editor pane drag-resize ────────────────────────────────────── */
function setPaneWidth(w){
  w=Math.min(Math.max(w,320),Math.max(360,window.innerWidth*0.7));
  sideEl.style.flex='0 0 '+w+'px';
  sideEl.style.maxWidth='none';
}
let paneDrag=null;
splitEl.addEventListener('mousedown',e=>{
  e.preventDefault();
  paneDrag={x:e.clientX,w:sideEl.getBoundingClientRect().width};
  splitEl.classList.add('active');
  document.body.style.cursor='col-resize';
  document.body.style.userSelect='none';
});
window.addEventListener('mousemove',e=>{
  if(!paneDrag)return;
  setPaneWidth(paneDrag.w-(e.clientX-paneDrag.x));
  renderPath();
});
window.addEventListener('mouseup',()=>{
  if(!paneDrag)return;
  paneDrag=null;
  splitEl.classList.remove('active');
  document.body.style.cursor='';
  document.body.style.userSelect='';
  try{localStorage.setItem('isotopo-pane',String(Math.round(sideEl.getBoundingClientRect().width)));}catch(_){}
});
try{
  const pw=parseInt(localStorage.getItem('isotopo-pane'),10);
  if(pw>0) setPaneWidth(pw);
}catch(_){}
async function copyPath(){
  const b=document.getElementById('cppath'), keep=b.innerHTML;
  try{
    await navigator.clipboard.writeText(PATH);
    b.innerHTML='<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>';
    setTimeout(()=>{b.innerHTML=keep;},1200);
  }catch(_){}
}

/* ── export the CURRENT render (incl. unsaved edits) ───────────── */
function currentSVG(){
  const el=zoomer.querySelector('svg'); if(!el) return '';
  const c=el.cloneNode(true);
  c.querySelectorAll('[data-edge-hit],[data-seg-handle]').forEach(el=>el.remove());
  c.querySelectorAll('path[data-connector]').forEach(p=>{p.classList.remove('edge-hover');p.style.removeProperty('cursor');});
  c.querySelectorAll('[data-part-id]').forEach(g=>{
    g.classList.remove('hi');
    if(!g.getAttribute('class')) g.removeAttribute('class');
    g.style.removeProperty('cursor');
    if(!g.getAttribute('style')) g.removeAttribute('style');
  });
  return c.outerHTML;
}
function exportName(ext){
  return FILENAME.replace(/\.[a-z0-9]+$/i,'')+(dirty?'.edited':'')+'.'+ext;
}
function downloadBlob(blob,name){
  const a=document.createElement('a');
  a.href=URL.createObjectURL(blob); a.download=name; a.click();
  setTimeout(()=>URL.revokeObjectURL(a.href),5000);
}
function exportSVG(){
  const sv=currentSVG(); if(!sv) return;
  downloadBlob(new Blob([sv],{type:'image/svg+xml'}),exportName('svg'));
}
function exportPNG(){
  const sv=currentSVG(); if(!sv) return;
  const url=URL.createObjectURL(new Blob([sv],{type:'image/svg+xml'}));
  const img=new Image();
  img.onload=()=>{
    const c=document.createElement('canvas');
    c.width=img.width*2; c.height=img.height*2;
    const ctx=c.getContext('2d'); ctx.scale(2,2); ctx.drawImage(img,0,0);
    c.toBlob(b=>downloadBlob(b,exportName('png')),'image/png');
    URL.revokeObjectURL(url);
  };
  img.src=url;
}

/* ── misc ───────────────────────────────────────────────────────── */
function downloadCopy(){
  const blob=new Blob([srcEl.value],{type:'text/plain'});
  const a=document.createElement('a');
  a.href=URL.createObjectURL(blob);
  a.download=FILENAME.replace(/(\.[a-z0-9]+)$/i,'.edited$1');
  a.click();
}
// Persist the editor's current content back to the original file on disk —
// the only write path in Studio. On success the saved content becomes the new
// baseline (so the dirty flag clears and "revert" reverts to it) and the local
// draft is dropped. The server parse-guards, so a broken doc never clobbers.
async function overwriteFile(){
  if(!serverOK){ setStatus('err',L('offline'),L('t_offline_write')); return; }
  const btn=document.getElementById('overwrite'); const prev=btn?btn.textContent:'';
  if(btn){ btn.disabled=true; btn.textContent=L('saving'); }
  try{
    const r=await fetch('/api/save?format='+encodeURIComponent(LANG),{method:'POST',body:srcEl.value});
    if(!r.ok){ setStatus('err',L('not_saved'),(await r.text()).trim()); return; }
    ORIGINAL=srcEl.value;                       // the file now matches the editor
    setDirty(false);
    try{localStorage.removeItem(DRAFTKEY);}catch(_){}
    setStatus('ok',L('saved')+' '+nowTime(),L('wrote')+' '+PATH);
  }catch(e){ setStatus('err',L('save_failed'),String(e)); }
  finally{ if(btn){ btn.disabled=false; btn.textContent=prev||L('overwrite'); } }
}
/* ── #src= permalink decode (kept so older shared links still load) ─ */
async function pipeThrough(stream,bytes){
  const w=stream.writable.getWriter();
  w.write(bytes); w.close();
  const out=[];
  const rd=stream.readable.getReader();
  for(;;){const {done,value}=await rd.read(); if(done)break; out.push(value);}
  let n=0; out.forEach(c=>n+=c.length);
  const all=new Uint8Array(n); let o=0;
  out.forEach(c=>{all.set(c,o);o+=c.length;});
  return all;
}
async function inflateText(b64){
  const s2=atob(b64.replace(/-/g,'+').replace(/_/g,'/'));
  const bytes=new Uint8Array(s2.length);
  for(let i=0;i<s2.length;i++) bytes[i]=s2.charCodeAt(i);
  const out=await pipeThrough(new DecompressionStream('deflate-raw'),bytes);
  return new TextDecoder().decode(out);
}
try{
  // DEFAULT: show the disk version. If a divergent draft from a prior session
  // exists, keep it and offer one-click "restore" — don't auto-apply it (that
  // used to hide external file changes and read as "no change on reload").
  const d=localStorage.getItem(DRAFTKEY);
  if(d&&d!==ORIGINAL){ pendingDraft=d; const r=document.getElementById('restore'); if(r) r.hidden=false; }
}catch(_){}
/* a #src= permalink outranks the draft — explicit intent wins */
if(location.hash.indexOf('#src=')===0){
  inflateText(location.hash.slice(5)).then(t=>{
    srcEl.value=t; setDirty(t!==ORIGINAL);
    buildMap(); paint(null);
    if(serverOK) rerender();
  }).catch(()=>{});
}
buildMap(); paint(null); wireHover(); wireDrag(); renderPath(); adaptStage();
applyI18n();
probe().then(()=>{if(serverOK)rerender();});
if(location.protocol.startsWith('http')) setInterval(probe,5000);
