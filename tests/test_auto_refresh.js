const fs=require('fs'),vm=require('vm'),assert=require('assert');
const source=fs.readFileSync('web/app.js','utf8');
let paused=false,hidden=false,version=1,refreshes=0,rails=0,opens=0,calls=0;
const nodes=Object.fromEntries(['#reader','.results','#document-rail','#project-material','#note-content pre'].map(k=>[k,k.endsWith('pre')?null:{scrollTop:37,innerHTML:''}]));
const context={console,document:{get hidden(){return hidden},querySelector(){return paused?{}:null},activeElement:{matches(){return false}},addEventListener(){}},window:{addEventListener(){}},setInterval(fn,ms){assert.equal(ms,15000)},$:k=>nodes[k]};
vm.createContext(context);
vm.runInContext(`var documentBrowse=null,active='project',view='projects',loadSerial=0,readerRevision=1,pendingTagItem=null;
function navigationState(){return {active,view,readerScroll:37,listScroll:37,railScroll:37}}
function projectMaterial(){return 'new meetings'};function showDocumentRail(){rails++;nodes['#document-rail'].scrollTop=0}
async function openItem(){opens++}
async function refresh(){refreshes++;loadSerial++;nodes['.results'].scrollTop=0}
async function api(path){calls++;if(path==='/api/changes')return {version};if(path.startsWith('/api/item/'))return {kind:'project',revision:1};return {documents:[]}}
`,context);
Object.assign(context,{nodes});
Object.defineProperties(context,{version:{get:()=>version},calls:{get:()=>calls,set:v=>calls=v},rails:{get:()=>rails,set:v=>rails=v},opens:{get:()=>opens,set:v=>opens=v},refreshes:{get:()=>refreshes,set:v=>refreshes=v}});
vm.runInContext(source.slice(source.indexOf('// Automatic library refresh.'),source.indexOf('// End automatic library refresh.')),context);
(async()=>{
 await context.autoRefresh();assert.equal(refreshes,1);assert.equal(rails,1);assert.equal(opens,0);
 assert.equal(nodes['.results'].scrollTop,37);assert.equal(nodes['#document-rail'].scrollTop,37);
 await context.autoRefresh();assert.equal(refreshes,1);
 version++;paused=true;const before=calls;await context.autoRefresh();assert.equal(calls,before);
 paused=false;hidden=true;await context.autoRefresh();assert.equal(calls,before);
 hidden=false;await context.autoRefresh();assert.equal(refreshes,2);assert.equal(rails,2);
 // Background changes refresh the originating person's collection, not document tags.
 vm.runInContext("active='preview';view='people';readerRevision=1;documentBrowse={id:'person',kind:'person',title:'Person',endpoint:'/api/person/person',data:{documents:[]}};showBrowseDocumentRail=()=>{rails++};api=async path=>{if(path==='/api/item/preview')return {kind:'document',revision:1};if(path==='/api/person/person')return {title:'Person',documents:[{id:'new',title:'New doc'}]};throw Error(path)}",context);
 await context.refreshOpenContext('preview');
 assert.equal(vm.runInContext('documentBrowse.data.documents[0].id',context),'new');
 // The same background path preserves meeting preview collections.
 vm.runInContext("active='meeting-preview';documentBrowse.previewType='meeting';api=async path=>path==='/api/item/meeting-preview'?{kind:'one-on-one',revision:1}:{title:'Person',meetings:[{id:'new-meeting',title:'New meeting'}]}",context);
 await context.refreshOpenContext('meeting-preview');
 assert.equal(vm.runInContext('documentBrowse.data.meetings[0].id',context),'new-meeting');
 assert.equal(vm.runInContext('documentBrowse.previewType',context),'meeting');
 const afterPreview=rails;
 // A late collection response must not restore a context the user left.
 vm.runInContext("api=async path=>{if(path==='/api/item/preview')return {kind:'document',revision:1};documentBrowse=null;active='other';return {documents:[]}}",context);
 vm.runInContext("documentBrowse={id:'person',endpoint:'/api/person/person'};active='preview'",context);
 await context.refreshOpenContext('preview');assert.equal(rails,afterPreview);
 vm.runInContext("active='project';view='projects';documentBrowse=null;api=async path=>path==='/api/changes'?{version}:{kind:'project',revision:1}",context);
 // Navigation that occurs during the request must not reopen the old reader.
 version++;vm.runInContext("refresh=async()=>{refreshes++;loadSerial++;active='other'}",context);
 await context.autoRefresh();assert.equal(rails,afterPreview);assert.equal(opens,0);
 // A transient failure must release the lock for the next check.
 vm.runInContext("api=async()=>{throw Error('offline')}",context);await context.autoRefresh();
 assert.equal(vm.runInContext('autoRefreshRunning',context),false);
 console.log('Automatic refresh: external changes, unchanged polling, scroll retention, editing/hidden pauses, navigation races and retry verified.');
})().catch(e=>{console.error(e);process.exit(1)});
