const fs=require('fs'),vm=require('vm'),assert=require('assert');
const source=fs.readFileSync('web/app.js','utf8');
const nodes={'#tags':{},'#filters':{}};
const context={console,$:key=>nodes[key],
 esc:s=>String(s??'').replaceAll('&','&amp;').replaceAll('<','&lt;').replaceAll('"','&quot;')};
vm.createContext(context);
vm.runInContext("var entities=[],tags=[],filters=[];",context);
vm.runInContext(source.slice(source.indexOf('function sidebar(){'),source.indexOf('function kindLabel')),context);
vm.runInContext(source.slice(source.indexOf('function connectionLink('),source.indexOf('async function openItem')),context);
vm.runInContext(source.slice(source.indexOf('function tagRail('),source.indexOf('let pendingTagItem')),context);
const person={id:'alex',kind:'person',name:'Alex Taylor',profile_id:'profile'};
const team={id:'team',kind:'team',name:'Platform Team',profile_id:null,count:2,relation:'member_of'};
const project={id:'project',kind:'project',name:'Sample Project',profile_id:'sample-project',count:1,relation:'works_on'};
const topic={id:'topic',name:'Reliability',category:'topic'};
const profile={id:'profile',kind:'person',relationships:[person,team,project],tags:[
 {id:'alex',name:person.name,category:'person'}, {id:'ptype',name:'Person',category:'type'},
 {id:'team',name:team.name,category:'team'},topic]};
const html=context.relationshipContext(profile);
assert(!html.includes(person.name));assert(html.includes('Team'));
assert(html.includes('Platform Team'));assert(html.includes('Works on'));
assert(html.includes('data-open-entity="team"'));assert(html.includes('data-remove-entity="project"'));
assert.deepEqual(Array.from(context.visibleTags(profile),t=>t.name),['Reliability']);
const meeting=context.relationshipContext({id:'meeting',kind:'one-on-one',
 participants:[{id:'profile',name:person.name}],relationships:[person]});
assert(meeting.includes('Participants'));assert(!meeting.includes('Related people'));
assert.equal((meeting.match(/Alex Taylor/g)||[]).length,1);
const document=context.relationshipContext({id:'doc',kind:'document',authors:[],
 relationships:[person]});
assert(document.includes('Related people'));assert(document.includes('Authors'));
assert(document.includes('Not recorded'));
context.entities=[person,team,project,{id:'jane',kind:'person',name:'Jane Doe',profile_id:'jane-profile'},{id:'candidate',name:'Candidate',kind:'person',candidate:true}];
context.tags=[topic,{id:'ptype',name:'Person',category:'type'}];
context.sidebar();
assert(nodes['#tags'].innerHTML.includes('data-open-entity="alex"'));
assert(!nodes['#tags'].innerHTML.includes('data-tag="alex"'));
assert(!nodes['#tags'].innerHTML.includes('Candidate'));
assert(nodes['#tags'].innerHTML.includes('Topic tags'));
const owned=context.relationshipContext({id:'doc',kind:'document',authors:[],
 document_people:[{id:'profile',name:person.name,role:'owner'},{id:'jane-profile',name:'Jane Doe',role:'mentioned'}]});
assert(owned.includes('Owner'));assert(owned.includes('Mentioned people'));assert(owned.includes('Not recorded'));
const unavailable=context.relationshipContext({id:'doc',kind:'document',metadata:{document_people:{ownership_status:'unavailable'}}});
assert(unavailable.includes('Not provided by Google connector'));
const rail=context.tagRail(profile);
assert(rail.includes('Connections'));assert(rail.includes('Related people'));
assert(!rail.includes('data-entity-link="alex"'));assert(rail.includes('data-document-tag="topic"'));
console.log('Typed connections, self-tag removal, role distinction, sidebar navigation, and topic-only tag controls verified.');
