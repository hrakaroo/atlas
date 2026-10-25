const fs=require('fs'),vm=require('vm'),assert=require('assert');
const source=fs.readFileSync('web/app.js','utf8');
const rail={innerHTML:'',hidden:true,scrollTop:95,attributes:{},
 setAttribute(key,value){this.attributes[key]=value},
 focus(options){this.focused=true;assert.equal(options.preventScroll,true)}};
const reader={innerHTML:'Current meeting summary',scrollTop:140};
const sessions=[{id:'current',title:'Current meeting',date:'2026-10-14'},
 {id:'older',title:'Older meeting',date:'2026-10-01'}];
let resolve;
const context={console,$:key=>key==='#document-rail'?rail:reader,
 esc:s=>String(s),dateLabel:i=>i.date,
 showRightRail(html,label){rail.innerHTML=html;rail.hidden=false;rail.scrollTop=0;rail.setAttribute('aria-label',label)},
 api:async()=>({title:'Jane ↔ John',sessions})};
vm.createContext(context);
vm.runInContext("var active='current',loadSerial=0;",context);
vm.runInContext(source.slice(source.indexOf('function datedRail'),source.indexOf('function tagRail')),context);
vm.runInContext(source.slice(source.indexOf('async function showMeetingHistory'),source.indexOf('async function openSeries')),context);
(async()=>{
 await context.showMeetingHistory('jane');
 assert.equal(vm.runInContext('active',context),'current');
 assert.equal(reader.innerHTML,'Current meeting summary');assert.equal(reader.scrollTop,140);
 assert.equal(rail.hidden,false);assert(rail.focused);
 assert(rail.innerHTML.includes('Meeting history'));
 assert(rail.innerHTML.indexOf('2026-10-14')<rail.innerHTML.indexOf('2026-10-01'));
 assert(rail.innerHTML.includes('data-item="current" aria-current="true"'));
 assert(rail.innerHTML.includes('data-item="older"'));
 assert.equal(rail.attributes['aria-label'],'Meeting history: Jane ↔ John');
 assert.equal(rail.attributes.tabindex,'-1');
 // A slow history response must not replace a different note's rail.
 context.api=()=>new Promise(r=>{resolve=r});
 const pending=context.showMeetingHistory('jane');
 vm.runInContext("active='another-note';loadSerial++;",context);
 rail.innerHTML='Different note tags';rail.focused=false;
 resolve({title:'Jane ↔ John',sessions});await pending;
 assert.equal(rail.innerHTML,'Different note tags');assert.equal(rail.focused,false);
 console.log('Meeting history preserves the current reader, focuses dated sessions, and ignores stale responses.');
})().catch(e=>{console.error(e);process.exit(1)});
