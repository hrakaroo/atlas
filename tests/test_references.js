const fs=require('fs'),vm=require('vm'),assert=require('assert');
const source=fs.readFileSync('web/app.js','utf8');
const context={esc:s=>String(s??'').replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('"','&quot;'),Date};
vm.createContext(context);
vm.runInContext(source.slice(source.indexOf('function sourceTitle(i){'),source.indexOf('function renderedSummary(i){')),context);
const item={kind:'document',title:'Migration Plan',source_links:[
 {kind:'document',url:'https://docs.google.com/document/d/original/edit',label:'Migration Plan',primary:true},
 {kind:'document',url:'https://docs.google.com/document/d/ref/edit',label:'Model <Rollouts>',atlas_id:'cached'},
 {kind:'document',url:'https://docs.google.com/document/d/other/edit',label:'Missing resources'},
 {kind:'calendar',url:'https://www.google.com/calendar/event?eid=meeting',label:'Migration Planning',date:'2026-10-13'}]};
const buttons=context.sourceButtons(item),refs=context.additionalDocuments(item);
assert.equal(context.sourceTitle(item),'Migration Plan');
assert(buttons.includes('Open original Google Doc'));
assert(!buttons.includes('Calendar'));
assert(refs.includes('Referenced documents <span>(2)'));
assert(refs.includes('Referenced meetings <span>(1)'));
assert(!refs.includes(' open'));
assert(refs.includes('data-item="cached" data-document-view="true"'));
assert(refs.includes('Model &lt;Rollouts>'));
assert(refs.includes('Open Model &lt;Rollouts> in Google Docs'));
assert(refs.includes('Oct 13, 2026'));
assert(!refs.includes('Google document 2'));
assert(!refs.includes('Additional sources'));
assert(!refs.includes('/original/'));
assert.equal(context.additionalDocuments({kind:'project',source_links:[]}), '');
console.log('Original-source action, collapsed named references, dated meetings, cached navigation and external shortcuts verified.');
