const fs=require('fs'),vm=require('vm'),assert=require('assert');
const source=fs.readFileSync('web/app.js','utf8');
const toggle={textContent:'Edit summary',attributes:{},setAttribute(k,v){this.attributes[k]=v}};
const content={children:[],_html:'Saved summary',replaceChildren(...nodes){this.children=nodes},
 set innerHTML(value){this._html=value;this.children=[]},get innerHTML(){return this._html}};
const element=tag=>({tag,value:'',style:{},attributes:{},setAttribute(k,v){this.attributes[k]=v},focus(){this.focused=true}});
let saved=null,opened=0;
const context={console,$:key=>key==='#summary-toggle'?toggle:key==='#note-content textarea'?content.children.find(n=>n.tag==='textarea'):content,
 document:{createElement:element},esc:s=>String(s).replaceAll('<','&lt;'),
 renderedSummary:i=>'render:'+i.summary,guarded:fn=>fn,refresh:async()=>{},
 api:async(path,data)=>{saved={path,data};return {}},openItem:async()=>{opened++},toast(){}};
vm.createContext(context);vm.runInContext("var sourceMode=false,active='note';",context);
vm.runInContext(source.slice(source.indexOf('function readerMoreMenu'),source.indexOf('function sourceButtons')),context);
vm.runInContext(source.slice(source.indexOf('function setupSummaryToggle'),source.indexOf('async function openItem')),context);
(async()=>{
 const controller=context.setupSummaryToggle({id:'note',summary:'Saved summary',revision:4});
 toggle.onclick();assert.equal(toggle.textContent,'Summary');assert.equal(toggle.attributes['aria-pressed'],'true');
 let text=content.children[0];text.value='My unsaved edit';
 toggle.onclick();assert.equal(toggle.textContent,'Edit summary');assert(content.innerHTML.includes('Unsaved preview'));assert(content.innerHTML.includes('My unsaved edit'));assert.equal(saved,null);
 toggle.onclick();assert.equal(content.children[0].value,'My unsaved edit');
 controller.showSource('<Original source>');
 assert.equal(toggle.textContent,'Summary');assert(content.innerHTML.includes('&lt;Original source>'));
 toggle.onclick();assert(content.innerHTML.includes('My unsaved edit'));assert.equal(toggle.textContent,'Edit summary');
 toggle.onclick();text=content.children[0];await content.children[1].onclick();
 assert.equal(saved.path,'/api/summary');assert.equal(saved.data.summary,'My unsaved edit');assert.equal(saved.data.expected_revision,4);assert.equal(opened,1);
 const menu=context.readerMoreMenu({source_path:'original.txt'});
 assert(menu.includes('View saved original'));assert(menu.includes('Remove…'));assert(menu.indexOf('reader-more-panel')<menu.indexOf('delete-note'));
 const noSource=context.readerMoreMenu({});assert(noSource.includes('More…'));assert(noSource.includes('Remove…'));assert(!noSource.includes('View saved original'));
 console.log('Summary toggle retains drafts and source navigation, saves explicitly, and places Remove in More.');
})().catch(e=>{console.error(e);process.exit(1)});
