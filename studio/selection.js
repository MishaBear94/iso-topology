// Pure geometry and identity helpers shared by Studio and regression tests.
function polygonTouchesRect(poly,r){
  if(poly.length<3) return false;
  let inside=false;
  for(let i=0,j=poly.length-1;i<poly.length;j=i++){
    const a=poly[j],b=poly[i];
    if(b[0]>=r.left&&b[0]<=r.right&&b[1]>=r.top&&b[1]<=r.bottom) return true;
    if((a[1]>r.top)!==(b[1]>r.top)&&r.left<(b[0]-a[0])*(r.top-a[1])/(b[1]-a[1])+a[0]) inside=!inside;
    let lo=0,hi=1;
    const clip=(v,d,min,max)=>{
      if(Math.abs(d)<1e-12) return v>=min&&v<=max;
      let x=(min-v)/d,y=(max-v)/d;if(x>y)[x,y]=[y,x];
      lo=Math.max(lo,x);hi=Math.min(hi,y);return lo<=hi;
    };
    if(clip(a[0],b[0]-a[0],r.left,r.right)&&clip(a[1],b[1]-a[1],r.top,r.bottom)) return true;
  }
  return inside;
}

// Keep every displayed instance of the moved source and its descendants in
// the same preview. The source edit still moves only the requested root(s).
function dragMemberIds(model,roots){
  const ids=new Set(roots);
  for(let changed=true;changed;){
    changed=false;
    for(const pm of model||[]){
      if(pm.parentId&&ids.has(pm.parentId)&&!ids.has(pm.id)){
        ids.add(pm.id);changed=true;
      }
    }
  }
  return ids;
}
