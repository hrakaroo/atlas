const fs=require('fs'),vm=require('vm'),assert=require('assert');
const source=fs.readFileSync('web/app.js','utf8');
const nodes=new Proxy({}, {get(target,key){return target[key]||(target[key]={value:'',checked:false,hidden:false,disabled:false,scrollTop:0})}});
nodes['#search'].value='Roadmap';nodes['#search-scope'].value='title';nodes['#sort-order'].value='date';
let calls=[],activity=0,lists=0,catalogRenders=0;
const doc={id:'doc',title:'Roadmap',latest_change_id:8,unseen_updates:2,changed_at:'2026-10-15T18:00:00Z'};
const context={URLSearchParams,console,$:s=>nodes[s],esc:s=>String(s),
 updateSearchClear(){nodes['#search-clear'].hidden=!nodes['#search'].value},
 nav(){catalogRenders++},sidebar(){catalogRenders++},list(){lists++},
 refreshActivity:async()=>{activity++},
 api:async(path,data)=>{calls.push({path,data});if(path.startsWith('/api/items?')||path.startsWith('/api/changed-documents?'))return [doc];return [];}};
vm.createContext(context);
vm.runInContext("var catalogsLoaded=true,view='documents',active=null,loadSerial=0,filters=[],tags=[],series=[],seriesFilter='',blogFeeds=[],entities=[],items=[],selected=new Set();",context);
vm.runInContext(source.slice(source.indexOf('async function refresh(background='),source.indexOf('async function openBlogFeed')),context);
vm.runInContext(source.slice(source.indexOf('function documentChangeStatus'),source.indexOf('function list(){')),context);
vm.runInContext(source.slice(source.indexOf('function sortedRows'),source.indexOf('function displayedItems')),context);
const updateRows=[{id:'updated',title:'Old updated document',latest_change_id:9,date:'2026-01-01'},
 {id:'starred',title:'Starred document',latest_change_id:0,date:'2026-01-02',starred:true},
 {id:'new',title:'New import',latest_change_id:0,date:'2026-10-25'}];
assert.deepEqual(Array.from(context.sortedRows(updateRows),r=>r.id),['updated','new','starred']);
nodes['#starred-only'].checked=true;
assert.deepEqual(Array.from(context.sortedRows(updateRows),r=>r.id),['starred']);
updateRows[0].starred=true;
assert.deepEqual(Array.from(context.sortedRows(updateRows),r=>r.id),['updated','starred']);
nodes['#starred-only'].checked=false;
(async()=>{
 await context.refreshSearch();
 assert.equal(calls.length,1);assert(calls[0].path.startsWith('/api/items?'));
 assert.equal(activity,0);assert.equal(catalogRenders,0);assert.equal(lists,1);
 assert(calls[0].path.includes('sort=updates'));assert.equal(nodes['#sort-order'].hidden,true);
 assert.equal(nodes['#changes-hint'].hidden,false);
 assert(context.documentChangeStatus(doc).includes('document-unread-dot'));
 calls=[];await context.refresh();
 assert.equal(calls.length,5);assert.equal(activity,0);assert.equal(catalogRenders,2);
 vm.runInContext("view='changes'",context);calls=[];
 await context.refreshSearch();
 assert.equal(calls.length,1);assert(calls[0].path.startsWith('/api/changed-documents?'));
 assert.equal(nodes['#kind'].value,'document');assert.equal(nodes['#sort-order'].disabled,true);
 assert.equal(nodes['.starred-filter'].hidden,true);assert.equal(nodes['#changes-hint'].hidden,false);
 assert(context.documentChangeStatus(doc).includes('document-unread-dot'));
 assert.equal(context.documentChangeStatus({...doc,unseen_updates:0}),'');
 vm.runInContext("view='documents'",context);
 updateRows[0].unseen_updates=2;
 assert(context.documentChangeStatus(updateRows[0]).includes('document-unread-dot'));
 const beforeSeen=Array.from(context.sortedRows(updateRows),r=>r.id);
 updateRows[0].unseen_updates=0;
 assert.equal(context.documentChangeStatus(updateRows[0]),'');
 assert.deepEqual(Array.from(context.sortedRows(updateRows),r=>r.id),beforeSeen);
 vm.runInContext("catalogsLoaded=false",context);calls=[];
 await context.refreshSearch();
 assert.equal(calls.length,5);assert.equal(vm.runInContext('catalogsLoaded',context),true);

 vm.runInContext("view='activity'",context);calls=[];
 await context.refresh();
 assert.equal(calls.length,0);assert.equal(activity,1);
 assert.equal(nodes['#activity-panel'].hidden,false);
 assert.equal(nodes['.workspace'].hidden,true);assert.equal(nodes['.toolbar'].hidden,true);
 assert.equal(nodes['#view-title'].textContent,'Activity');
 vm.runInContext("view='documents'",context);await context.refresh();
 assert.equal(nodes['#activity-panel'].hidden,true);assert.equal(nodes['.workspace'].hidden,false);
 console.log('Search fetches only results; Documents combines update ordering with normal browsing and explain new versus seen updates.');
})().catch(e=>{console.error(e);process.exit(1)});
