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
(async()=>{
 await context.refreshSearch();
 assert.equal(calls.length,1);assert(calls[0].path.startsWith('/api/items?'));
 assert.equal(activity,0);assert.equal(catalogRenders,0);assert.equal(lists,1);
 calls=[];await context.refresh();
 assert.equal(calls.length,5);assert.equal(activity,0);assert.equal(catalogRenders,2);
 vm.runInContext("view='changes'",context);calls=[];
 await context.refreshSearch();
 assert.equal(calls.length,1);assert(calls[0].path.startsWith('/api/changed-documents?'));
 assert.equal(nodes['#kind'].value,'document');assert.equal(nodes['#sort-order'].disabled,true);
 assert.equal(nodes['.starred-filter'].hidden,true);assert.equal(nodes['#changes-hint'].hidden,false);
 assert(context.documentChangeStatus(doc).includes('2 updates since last viewed'));
 assert(context.documentChangeStatus({...doc,unseen_updates:0}).includes('Seen'));
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
 console.log('Search fetches only results; changed documents use the dedicated view and explain new versus seen updates.');
})().catch(e=>{console.error(e);process.exit(1)});
