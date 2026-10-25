const fs=require('fs'),vm=require('vm'),assert=require('assert');
const source=fs.readFileSync('web/app.js','utf8');
let mutations=0,parses=0;
class Node {
 constructor(name,attrs={},children=[]){
  this.nodeName=name;this.nodeType=name==='#text'?3:1;this.attrs={...attrs};
  this.nodeValue=name==='#text'?attrs.text:null;this.childNodes=[];this.parentNode=null;
  this.checked=Object.hasOwn(attrs,'checked');this.scrollTop=0;this.hidden=false;
  for(const child of children){this.childNodes.push(child);child.parentNode=this;}
 }
 get attributes(){return Object.entries(this.attrs).map(([name,value])=>({name,value}));}
 get firstChild(){return this.childNodes[0]||null;}
 get nextSibling(){return this.parentNode?.childNodes[this.parentNode.childNodes.indexOf(this)+1]||null;}
 hasAttribute(name){return Object.hasOwn(this.attrs,name);}
 getAttribute(name){return this.attrs[name]??null;}
 setAttribute(name,value){mutations++;this.attrs[name]=value;}
 removeAttribute(name){mutations++;delete this.attrs[name];}
 matches(selector){return selector.includes('.rail-document')&&this.attrs.class==='rail-document'||selector.includes('label.rail-tag')&&this.nodeName==='LABEL';}
 querySelector(){return this.childNodes.find(n=>['data-item','data-meeting-item','data-entity-link','data-document-tag'].some(a=>n.hasAttribute(a)))||null;}
 contains(node){return this===node||this.childNodes.some(child=>child.contains(node));}
 cloneNode(){return new Node(this.nodeName,this.attrs,this.childNodes.map(n=>n.cloneNode()));}
 insertBefore(node,cursor){
  mutations++;node.remove();const at=cursor?this.childNodes.indexOf(cursor):this.childNodes.length;
  this.childNodes.splice(at,0,node);node.parentNode=this;
 }
 remove(){if(this.parentNode){mutations++;const siblings=this.parentNode.childNodes;siblings.splice(siblings.indexOf(this),1);this.parentNode=null;}}
 focus(){document.activeElement=this;}
}
const text=s=>new Node('#text',{text:s});
const row=(id,title)=>new Node('DIV',{class:'rail-document'},[new Node('BUTTON',{'data-item':id},[text(title)])]);
const checkbox=checked=>new Node('LABEL',{class:'rail-tag'},[new Node('INPUT',{'data-document-tag':'tag','data-document-id':'doc',...(checked?{checked:''}:{})}),new Node('SPAN',{},[text('Topic')])]);
const rail=new Node('ASIDE'),workspace={classList:{add(){},remove(){}}};
const fixtures={
 initial:new Node('FRAGMENT',{},[row('a','Alpha'),row('b','Beta'),checkbox(false)]),
 changed:new Node('FRAGMENT',{},[row('b','Beta updated'),row('a','Alpha'),row('c','New document'),checkbox(true)]),
 removed:new Node('FRAGMENT',{},[row('a','Alpha'),checkbox(true)]),
 other:new Node('FRAGMENT',{},[row('z','Other person document')])
};
const document={activeElement:null,createElement(name){assert.equal(name,'template');parses++;return {set innerHTML(value){this.content=fixtures[value].cloneNode(true);}}}};
const context={document,$:selector=>selector==='#document-rail'?rail:workspace};
vm.createContext(context);
vm.runInContext("var documentBrowse=null,active='person';",context);
vm.runInContext(source.slice(source.indexOf('let rightRailContext='),source.indexOf('function documentSourceAction')),context);
vm.runInContext(source.slice(source.indexOf('function showRightRail('),source.indexOf('function datedRail(')),context);
context.showRightRail('initial','Person documents');
const first=rail.childNodes[0],second=rail.childNodes[1],label=rail.childNodes[2],input=label.childNodes[0];
second.childNodes[0].focus();rail.scrollTop=137;
const before=mutations,parsed=parses;
context.showRightRail('initial','Person documents');
assert.equal(mutations,before,'unchanged refresh must not mutate live DOM');
assert.equal(parses,parsed,'unchanged rail must not be reparsed');
assert.equal(rail.scrollTop,137);assert.equal(document.activeElement,second.childNodes[0]);
context.showRightRail('changed','Person documents');
assert.equal(rail.childNodes[0],second,'reordered row must retain its DOM identity');
assert.equal(rail.childNodes[1],first);
assert.equal(rail.childNodes[3],label);assert.equal(label.childNodes[0],input);
assert.equal(input.checked,true,'existing checkbox property must update');
assert.equal(second.childNodes[0].childNodes[0].nodeValue,'Beta updated');
assert.equal(rail.scrollTop,137);assert.equal(document.activeElement,second.childNodes[0]);
context.showRightRail('removed','Person documents');
assert.equal(rail.childNodes[0],first);assert.equal(rail.childNodes[1],label);
assert.equal(rail.childNodes.length,2);assert.equal(second.parentNode,null);
// Different preview documents retain their originating person's scroll context.
vm.runInContext("documentBrowse={id:'person'};active='preview-a'",context);
rail.scrollTop=211;
context.showRightRail('removed','Person documents');
assert.equal(rail.scrollTop,211);
vm.runInContext("active='preview-b'",context);
context.showRightRail('changed','Person documents');
assert.equal(rail.scrollTop,211);
vm.runInContext("documentBrowse=null;active='other-person'",context);
context.showRightRail('other','Person documents');
assert.equal(rail.scrollTop,0,'new context starts at the top');
assert.equal(rail.hidden,false);
const retained=rail.firstChild;
context.hideDocumentRail();
assert.equal(rail.hidden,true);assert.equal(rail.firstChild,retained,'closing must not destroy rail DOM');
context.showRightRail('other','Person documents');
assert.equal(rail.hidden,false);assert.equal(rail.firstChild,retained);
assert(!source.includes("if(!background)hideDocumentRail();"),'item refresh must not clear the rail while loading');
console.log('Stable right rail: unchanged DOM, keyed updates/reordering, checkbox state, focus, scroll, removal and context changes verified.');
