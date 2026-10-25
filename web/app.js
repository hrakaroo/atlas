'use strict';
const token=new URLSearchParams(location.search).get('token');
const $=s=>document.querySelector(s);
const esc=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
let catalogsLoaded=false;
let entities=[],blogFeeds=[],series=[],seriesFilter='',wikiContext=null,tags=[],items=[],active=null,view='all',filters=[],selected=new Set(),sourceMode=false,loadSerial=0,readerRevision=null;
async function api(path,data){const r=await fetch(path,{method:data?'POST':'GET',headers:{'X-Atlas-Token':token,'Content-Type':'application/json'},body:data?JSON.stringify(data):undefined});const out=await r.json();if(!r.ok)throw Error(out.error||'Request failed');return out;}
let toastTimer;function toast(msg){$('#toast').textContent=msg;$('#toast').classList.add('visible');clearTimeout(toastTimer);toastTimer=setTimeout(()=>$('#toast').classList.remove('visible'),4500);}
function guarded(fn){return async(...args)=>{try{await fn(...args);}catch(e){toast(e.message);}};}
function chip(t,remove=false){return `<button class="chip" data-category="${esc(t.category)}" data-tag="${t.id}" ${remove?'data-remove="1"':''} draggable="true">${esc(t.name)}${remove?' ×':''}</button>`;}
function nav(){
    const options=[['all','◫','All notes'],['changes','↻','What’s changed'],['meetings','◷','Meetings'],['projects','◇','Projects'],['documents','▤','Documents'],['people','♙','People'],['interviews','◇','Interviews'],['blogs','▤','Feeds'],['activity','≡','Activity'],['trash','⌫','Trash']];
    $('#navigation').innerHTML=options.map(([id,icon,label])=>`<button class="nav-item ${(view===id||(id==='blogs'&&view.startsWith('blog:')))?'active':''}" data-view="${id}"><span>${icon}</span>${label}</button>`).join('');
}
function sidebar(){
    const connections=[['team','Teams'],['project','Projects'],['person','People']];
    const connectionHtml=connections.map(([kind,label])=>{
        const group=entities.filter(e=>e.kind===kind&&!e.candidate);
        return group.length?'<div class="tag-category">'+label+'</div>'+group.map(e=>'<div class="sidebar-entity-row '+(filters.includes(e.id)?'selected':'')+'"><button class="sidebar-entity" data-open-entity="'+e.id+'"><span class="dot"></span>'+esc(e.name)+'</button><button class="entity-count" data-entity-filter="'+e.id+'" aria-label="Filter by '+esc(e.name)+'" title="Filter connected records">'+e.count+'</button></div>').join(''):'';
    }).join('');
    const tagHtml=[['topic','Topic tags'],['type','Types']].map(([category,label])=>{
        const group=tags.filter(t=>t.category===category&&!['person','team','project','interview candidate'].includes(t.name.toLowerCase()));
        return group.length?'<div class="tag-category">'+label+'</div>'+group.map(t=>'<button class="sidebar-tag '+(filters.includes(t.id)?'selected':'')+'" data-tag="'+t.id+'" data-category="'+category+'" draggable="true"><span class="dot"></span>'+esc(t.name)+'<small>'+t.count+'</small></button>').join(''):'';
    }).join('');
    $('#tags').innerHTML=connectionHtml+tagHtml;
    $('#filters').innerHTML=filters.map(id=>{
        const e=entities.find(e=>e.id===id);
        return e?'<button class="connection-filter" data-entity-filter="'+e.id+'" aria-label="Remove '+esc(e.name)+' filter">'+esc(e.name)+' ×</button>':tags.find(t=>t.id===id)?chip(tags.find(t=>t.id===id),true):'';
    }).join('');
}
function visibleTags(item){
    return (item.tags||[]).filter(t=>['topic','type'].includes(t.category)&&
        !['person','team','project','interview candidate'].includes(t.name.toLowerCase())&&
        !(t.category==='type'&&t.name.toLowerCase()===item.kind.replaceAll('-',' ')));
}
function listConnections(item){
    return [...new Set((item.relationships||[]).filter(r=>r.kind!=='person').map(r=>r.name))].join(' · ');
}

function kindLabel(kind){return kind==='blog'?'Feed update':kind.replaceAll('-',' ');}
function documentRefreshControl(i){
    const sources=(i.source_links||[]).filter(s=>s.primary&&/docs\.google\.com\/document\/(?:u\/\d+\/)?d\/(?!e\/)/.test(s.url));
    if(!['document','reference'].includes(i.kind)||new Set(sources.map(s=>s.url)).size!==1)return '';
    const sync=i.metadata?.google_sync;
    return `<label class="refresh-request"><input type="checkbox" id="refresh-request" ${i.metadata?.refresh_requested?'checked':''}> Refresh requested</label>${sync?.last_checked?`<span class="muted sync-checked">Checked ${esc(new Date(sync.last_checked).toLocaleDateString())}</span>`:''}`;
}
function dateLabel(i){return i.kind==='update'?`${i.period_start} → ${i.period_end}`:i.date?(i.date_basis==='added'?'Added · ':'')+i.date:'Undated';}
function groupedItems(){
    if(view==='blogs'&&!$('#search').value.trim())return blogFeeds.map(feed=>({id:'blog:'+feed.id,title:feed.name,kind:'feed',date:feed.latest_date||'',tags:[],preview:feed.edition_count?feed.edition_count+' dated roundups · newest first':'Your feed updates will appear here.'}));
    if(view==='trash'||view.startsWith('blog:')||$('#search').value.trim()||seriesFilter)return items;
    const grouped=new Map(),out=[];
    for(const item of [...items].sort((a,b)=>(b.date||b.created||'').localeCompare(a.date||a.created||''))){
        if(item.blog_feed){
            const key='blog:'+item.blog_feed.id;let row=grouped.get(key);
            if(!row){row={...item,id:key,title:item.blog_feed.name,kind:'feed',edition_ids:[],tags:[]};grouped.set(key,row);out.push(row);}
            row.starred=!!row.starred||!!item.starred;
            row.edition_ids.push(item.id);row.preview=row.edition_ids.length+' dated roundups · newest first';
            for(const tag of item.tags)if(!row.tags.some(t=>t.id===tag.id))row.tags.push(tag);
            continue;
        }
        if(!item.series){out.push(item);continue;}
        let row=grouped.get(item.series.id);
        if(!row){
            row={...item,id:'series:'+item.series.id,title:item.series.title,kind:'meeting series',session_ids:[],tags:[]};
            grouped.set(item.series.id,row);out.push(row);
        }
        row.starred=!!row.starred||!!item.starred;
        row.session_ids.push(item.id);
        for(const tag of item.tags)if(!row.tags.some(t=>t.id===tag.id))row.tags.push(tag);
    }
    return out;
}
function sortedRows(rows,useListControls=true){
    if(view==='changes')return [...rows].sort((a,b)=>b.latest_change_id-a.latest_change_id);
    if(!useListControls)return [...rows];
    const result=rows.filter(item=>view==='trash'||!$('#starred-only').checked||item.starred);
    result.sort((a,b)=>a.title.localeCompare(b.title,undefined,{sensitivity:'base'}));
    if($('#sort-order').value==='date')result.sort((a,b)=>(b.period_start||b.date||b.created||'').localeCompare(a.period_start||a.date||a.created||''));
    result.sort((a,b)=>Number(!!b.starred)-Number(!!a.starred));
    if($('#search').value.trim())result.sort((a,b)=>(b.search_title_rank||0)-(a.search_title_rank||0));
    return result;
}
function displayedItems(){return sortedRows(groupedItems());}
function starControl(i){return `<button class="item-star ${i.starred?'is-starred':''}" data-star="${i.id}" data-starred="${i.starred?'1':'0'}" aria-pressed="${!!i.starred}" aria-label="${i.starred?'Unstar':'Star'} ${esc(i.title)}" title="${i.starred?'Unstar':'Star'}">${i.starred?'★':'☆'}</button>`;}
function rowIds(id){
    if(id?.startsWith('blog:'))return items.filter(i=>i.blog_feed?.id===id.slice(5)).map(i=>i.id);
    if(id?.startsWith('series:'))return items.filter(i=>i.series?.id===id.slice(7)).map(i=>i.id);
    return id?[id]:[];
}
function documentChangeStatus(item){
    if(view!=='changes')return '';
    const label=item.unseen_updates>1?item.unseen_updates+' updates since last viewed':item.unseen_updates?'New update':'Seen';
    return '<div class="document-change-status '+(item.unseen_updates?'unseen':'')+'">'+esc(label)+'</div>';
}
function documentChangeDate(item){
    return 'Updated · '+new Date(item.changed_at).toLocaleDateString(undefined,{month:'short',day:'numeric'});
}
function list(){
    const shown=displayedItems();let lastWeek='';
    $('#items').innerHTML=shown.map(i=>{
        let group='';if(view==='timeline'&&i.period_start!==lastWeek){lastWeek=i.period_start;group=`<div class="week-heading">Week of ${esc(lastWeek)}</div>`;}
        const isSeries=!!i.session_ids;
        const isDocument=['document','reference'].includes(i.kind);
        return group+`<div class="item ${(documentBrowse?.id||active)===i.id||(i.session_ids||[]).includes(active)||(i.id.startsWith('blog:')&&items.some(n=>n.id===active&&n.blog_feed?.id===i.id.slice(5)))?'active':''}" data-item="${i.id}" tabindex="0" draggable="${view==='trash'||!rowIds(i.id).length?'false':'true'}"><div class="item-top"><span>${esc(i.is_interview_candidate?'Candidate':i.is_interview?'Interview':kindLabel(i.kind))}</span><span>${esc(view==='changes'?documentChangeDate(i):isSeries?'Latest · '+dateLabel(i):dateLabel(i))}</span></div><h3>${isDocument?`<button class="document-title" data-item="${i.id}">${esc(i.title)}</button>`:esc(i.title)}</h3>${view!=='trash'&&!i.id.startsWith('series:')&&!i.id.startsWith('blog:')?starControl(i):''}${view==='trash'||!rowIds(i.id).length?'':`<input class="select-box" aria-label="Select ${esc(i.title)}" type="checkbox" data-select="${i.id}" ${rowIds(i.id).every(id=>selected.has(id))?'checked':''}>`}${documentChangeStatus(i)}<p>${esc(i.search_excerpt?'Matching text: '+i.search_excerpt:isSeries?i.session_ids.length+' dated '+(i.session_ids.length===1?'session':'sessions')+' · '+(i.series.cadence||'Recurring meeting'):isDocument?i.document_blurb||i.preview:i.preview)}</p><div class="list-connections">${esc(listConnections(i))}</div><div class="chips">${visibleTags(i).filter(t=>t.category!=='type').slice(0,4).map(t=>chip(t)).join('')}</div></div>`;
    }).join('')||(view==='changes'?'<div class="empty"><h2>No matching document updates.</h2><p>Star a document to follow future source changes. Updates appear after Atlas refreshes it. Try clearing any search or filters.</p></div>':'<div class="empty"><h2>No notes here yet.</h2><p>Try a different filter, or add material to your library.</p></div>');
    $('#result-count').textContent=`${shown.length} ${shown.length===1?'entry':'entries'}${selected.size?' · '+selected.size+' selected':''}`;
    $('#clear-selection').hidden=!selected.size;
}
// Navigation history keeps the browse context together with the open item.
let navigationHistory=[],navigationIndex=-1;
let documentBrowse=null,railCollection=null;
function navigationState(){return {view,active,documentBrowse:documentBrowse?{...documentBrowse}:null,filters:[...filters],seriesFilter,search:$('#search').value,searchScope:$('#search-scope').value,kind:$('#kind').value,starred:$('#starred-only').checked,sort:$('#sort-order').value,readerScroll:$('#reader').scrollTop,listScroll:$('.results').scrollTop,railScroll:$('#document-rail').scrollTop,activityScroll:$('#activity-list').scrollTop};}
function updateHistoryControls(){
    $('#nav-back').disabled=navigationIndex<=0;
    $('#nav-forward').disabled=navigationIndex>=navigationHistory.length-1;
}
function saveCurrentLocation(){
    const state=navigationState();
    if(navigationIndex<0){navigationHistory=[state];navigationIndex=0;}
    else navigationHistory[navigationIndex]=state;
}
function appendLocation(){
    const state=navigationState(),current=navigationHistory[navigationIndex];
    if(current&&current.active===state.active&&current.view===state.view){navigationHistory[navigationIndex]=state;updateHistoryControls();return;}
    navigationHistory=navigationHistory.slice(0,navigationIndex+1);
    navigationHistory.push(state);navigationIndex++;updateHistoryControls();
}
async function navigateItem(id,documentView=false,interviewView=false,meetingView=false){
    saveCurrentLocation();documentBrowse=null;railCollection=null;
    if(documentView){view='documents';filters=[];seriesFilter='';$('#search').value='';$('#kind').value='';$('#starred-only').checked=false;selected.clear();await refresh();}
    if(meetingView){view='meetings';filters=[];seriesFilter='';$('#search').value='';$('#kind').value='';$('#starred-only').checked=false;selected.clear();await refresh();}
    if(interviewView){view='interviews';filters=[];seriesFilter='';$('#search').value='';$('#kind').value='';$('#starred-only').checked=false;selected.clear();await refresh();}
    const displayedChange=view==='changes'?items.find(item=>item.id===id)?.latest_change_id:null;
    await openItem(id);
    if(displayedChange&&active===id&&view==='changes'){
        await api('/api/changes-seen',{item_id:id,through_id:displayedChange});
        const row=items.find(item=>item.id===id);
        if(row&&row.latest_change_id===displayedChange){row.unseen_updates=0;list();}
    }
    appendLocation();
}
async function navigateRailDocument(id){return navigateRailEntry(id,'document');}
async function navigateRailMeeting(id){return navigateRailEntry(id,'meeting');}
async function navigateRailEntry(id,previewType){
    const context=documentBrowse||railCollection;
    if(!context||!['person','project','team'].includes(context.kind))return navigateItem(id,previewType==='document',false,previewType==='meeting');
    saveCurrentLocation();documentBrowse={...context,previewType};
    await openItem(id);
    if(active===id)appendLocation();
}
function browseDocumentActions(){
    if(!documentBrowse)return '';
    return documentBrowse.previewType==='meeting'?'<button data-open-in-meetings="'+active+'">Open in Meetings</button>':'<button data-open-in-documents="'+active+'">Open in Documents</button>';
}
function browseDocumentHeader(){
    return documentBrowse?'<div class="document-browse-context"><button data-return-context="'+esc(documentBrowse.id)+'">← '+esc(documentBrowse.title)+'</button><span>Browsing '+(documentBrowse.previewType==='meeting'?'meetings':'documents')+'</span></div>':'';
}
function collectionRailMarkup(data,kind){
    let html=documentRail(data,kind);
    if(!['person','project','team'].includes(kind))return html;
    html=html.replace(/<button class="rail-document-title"([^>]*data-item="([^"]+)"[^>]*data-context-(?:document|meeting)="true"[^>]*)>/g,
        (_,attributes,id)=>'<button class="rail-document-title'+(documentBrowse&&id===active?' is-current':'')+'"'+attributes+(documentBrowse&&id===active?' aria-current="true"':'')+'>');
    return '<div class="collection-rail-heading">'+esc(data.title||documentBrowse?.title||'')+(kind==='person'&&(data.meetings||[]).length?'’s documents and meetings':'’s documents')+'</div>'+html;
}
function showBrowseDocumentRail(){
    if(documentBrowse)showRightRail(collectionRailMarkup(documentBrowse.data,documentBrowse.kind),documentBrowse.title+' documents and meetings');
}
async function navigateView(nextView){
    saveCurrentLocation();view=nextView;seriesFilter='';filters=[];$('#kind').value='';selected.clear();closeReader();await refresh();
    if(view.startsWith('blog:'))await openBlogFeed(view.slice(5));
    appendLocation();
}
async function travelHistory(direction){
    const index=navigationIndex+direction;
    if(index<0||index>=navigationHistory.length)return;
    saveCurrentLocation();const state=navigationHistory[index];
    view=state.view;filters=[...state.filters];seriesFilter=state.seriesFilter;selected.clear();
    $('#search').value=state.search;$('#search-scope').value=state.searchScope||'title';updateSearchScope();$('#kind').value=state.kind;$('#starred-only').checked=state.starred;$('#sort-order').value=state.sort;
    closeReader();documentBrowse=state.documentBrowse?{...state.documentBrowse}:null;await refresh();if(state.active)await openItem(state.active);
    navigationIndex=index;$('#activity-list').scrollTop=state.activityScroll||0;$('#reader').scrollTop=state.readerScroll;$('.results').scrollTop=state.listScroll;$('#document-rail').scrollTop=state.railScroll;updateHistoryControls();
}
let rightRailContext=null,rightRailHTML=null;
function hideDocumentRail(){
    $('#document-rail').hidden=true;rightRailContext=null;
    $('.workspace').classList.remove('has-document-rail');
}
// Keep the rail's live nodes, focus and scroll stable; parse only in a detached template.
function railNodeKey(node){
    if(node.nodeType!==1)return null;
    const attributes=['id','data-item','data-meeting-item','data-star','data-entity-link','data-document-tag'];
    for(const name of attributes){
        if(node.hasAttribute(name))return name+':'+node.getAttribute(name);
    }
    if(node.matches('.rail-document,label.rail-tag')){
        const control=node.querySelector('[data-item],[data-meeting-item],[data-entity-link],[data-document-tag]');
        if(control)return node.nodeName+':'+railNodeKey(control);
    }
    return null;
}
function reconcileRailChildren(parent,incoming){
    const previous=Array.from(parent.childNodes),used=new Set();
    const keyed=new Map(previous.map(node=>[railNodeKey(node),node]).filter(([key])=>key));
    let cursor=parent.firstChild;
    for(const next of Array.from(incoming.childNodes)){
        const key=railNodeKey(next);
        let node=key?keyed.get(key):previous.find(old=>!used.has(old)&&!railNodeKey(old)&&old.nodeType===next.nodeType&&old.nodeName===next.nodeName);
        if(node&&(used.has(node)||node.nodeType!==next.nodeType||node.nodeName!==next.nodeName))node=null;
        if(!node){node=next.cloneNode(true);parent.insertBefore(node,cursor);}
        else{
            if(node!==cursor)parent.insertBefore(node,cursor);
            if(node.nodeType===1){
                for(const attribute of Array.from(node.attributes))if(!next.hasAttribute(attribute.name))node.removeAttribute(attribute.name);
                for(const attribute of Array.from(next.attributes))if(node.getAttribute(attribute.name)!==attribute.value)node.setAttribute(attribute.name,attribute.value);
                if(node.nodeName==='INPUT'&&node.checked!==next.checked)node.checked=next.checked;
                reconcileRailChildren(node,next);
            }else if(node.nodeValue!==next.nodeValue)node.nodeValue=next.nodeValue;
        }
        used.add(node);cursor=node.nextSibling;
    }
    for(const node of previous)if(!used.has(node))node.remove();
}
function documentSourceAction(item){
    const source=(item.source_links||[]).find(link=>link.kind==='document'&&link.primary&&/^https:\/\//.test(link.url||''));
    return source?`<a class="rail-document-source" href="${esc(source.url)}" target="_blank" rel="noopener noreferrer" aria-label="Open ${esc(item.title)} in ${source.url.startsWith('https://docs.google.com/')?'Google Docs':'Confluence'}" title="Open in ${source.url.startsWith('https://docs.google.com/')?'Google Docs':'Confluence'}">↗</a>`:'';
}
function documentPeopleRoles(item){
    const labels={owner:'Owner',creator:'Created by',mentioned:'Mentioned'};
    const roles=(item.connection_roles||[]).map(role=>labels[role]).filter(Boolean);
    return roles.length?'<small class="document-people-roles">'+esc(roles.join(' · '))+'</small>':'';
}
function documentRail(data,kind){
    const interviews=kind==='candidate'?[...(data.interviews||[])].sort((a,b)=>Number(!!b.starred)-Number(!!a.starred)||String(b.date).localeCompare(String(a.date))):[];
    const interviewHtml=kind==='candidate'?`<div class="document-rail-heading"><h3>Interviews <span>${interviews.length}</span></h3></div><div class="document-rail-list">${interviews.length?interviews.map(item=>`<div class="rail-document"><button class="rail-document-title" data-item="${item.id}" data-interview-view="true">${esc(item.title)}</button>${starControl(item)}</div>`).join(''):'<p class="muted">No interviews linked yet.</p>'}</div>`:'';
    const documentRows=kind==='person'?[...(data.owned_documents||[]),...(data.documents||[])]:data.documents||[];
    const uniqueDocuments=new Map();
    for(const item of documentRows){
        const previous=uniqueDocuments.get(item.id);
        uniqueDocuments.set(item.id,{...previous,...item,connection_roles:[...new Set([...(previous?.connection_roles||[]),...(item.connection_roles||[])])]});
    }
    const rows=[...uniqueDocuments.values()].sort((a,b)=>a.title.localeCompare(b.title,undefined,{sensitivity:'base'}));
    rows.sort((a,b)=>Number(!!b.starred)-Number(!!a.starred));
    if(kind==='candidate'&&!rows.length&&!(data.owned_documents||[]).length&&!(data.related_documents||[]).length)return interviewHtml;
    const ownedDocs=kind==='person'?[]:[...(data.owned_documents||[])].sort((a,b)=>Number(!!b.starred)-Number(!!a.starred)||a.title.localeCompare(b.title));
    const ownedHtml=ownedDocs.length?'<div class="document-rail-heading"><h3>Owned / created documents <span>'+ownedDocs.length+'</span></h3></div><div class="document-rail-list">'+ownedDocs.map(item=>'<div class="rail-document"><button class="rail-document-title" data-item="'+item.id+'" data-context-document="true">'+esc(item.title)+documentPeopleRoles(item)+'</button>'+documentSourceAction(item)+starControl(item)+'</div>').join('')+'</div>':'';
    const relatedDocs=kind==='person'?[...(data.mentioned_documents||[])].sort((a,b)=>Number(!!b.starred)-Number(!!a.starred)||a.title.localeCompare(b.title,undefined,{sensitivity:'base'})):[];
    const relatedHtml=relatedDocs.length?'<div class="document-rail-heading"><h3>Mentioned in <span>'+relatedDocs.length+'</span></h3></div><div class="document-rail-list">'+relatedDocs.map(item=>'<div class="rail-document"><button class="rail-document-title" data-item="'+item.id+'" data-context-document="true">'+esc(item.title)+documentPeopleRoles(item)+'</button>'+documentSourceAction(item)+starControl(item)+'</div>').join('')+'</div>':'';
    const members=data.members||[];
    const memberHtml=members.length?'<div class="document-rail-heading"><h3>Members <span>'+members.length+'</span></h3></div><div class="document-rail-list">'+members.map(item=>'<button class="rail-document-title" data-item="'+item.id+'">'+esc(item.title)+'</button>').join('')+'</div>':'';
    const meetings=kind==='person'?[...(data.meetings||[])].sort((a,b)=>String(b.date).localeCompare(String(a.date))):[];
    const meetingHtml=meetings.length?`<div class="document-rail-heading"><h3>Meetings <span>${meetings.length}</span></h3></div><div class="document-rail-list">${meetings.map(item=>`<div class="rail-document"><button class="rail-document-title" data-item="${item.id}" data-meeting-item="${item.id}" data-context-meeting="true">${esc(item.title)}<small class="person-meeting-date">${esc(dateLabel(item))}</small></button>${starControl(item)}</div>`).join('')}</div>`:'';
    const documentsHtml=`<div class="document-rail-heading"><h3>${kind==='person'?'Owned / Authored':'Documents'} <span>${rows.length}</span></h3></div><div class="document-rail-list">${rows.length?rows.map(item=>`<div class="rail-document"><button class="rail-document-title" data-item="${item.id}" data-context-document="true">${esc(item.title)}${documentPeopleRoles(item)}</button>${documentSourceAction(item)}${starControl(item)}</div>`).join(''):`<p class="muted">${kind==='person'?'No owned or authored documents here yet.':'No documents linked to this project yet.'}</p>`}</div>`;
    return kind==='person'?documentsHtml+relatedHtml+meetingHtml:interviewHtml+memberHtml+ownedHtml+documentsHtml;
}
function showDocumentRail(data,kind){
    if(['person','project','team'].includes(kind)&&!documentBrowse)railCollection={
        id:active,title:data.title||items.find(item=>item.id===active)?.title||'Record',kind,data,
        endpoint:active.startsWith('entity:')?'/api/entity/'+active.slice(7):'/api/'+kind+'/'+active};
    showRightRail(collectionRailMarkup(data,kind),kind==='candidate'?'Interviews':kind==='person'?'Person documents and meetings':'Connected records');
}
function showRightRail(html,label){
    const contextId=documentBrowse?.id||active;
    const rail=$('#document-rail'),sameContext=rightRailContext===contextId,scroll=rail.scrollTop;
    const focused=sameContext&&rail.contains(document.activeElement)?document.activeElement:null;
    if(html!==rightRailHTML){
        const template=document.createElement('template');template.innerHTML=html;
        reconcileRailChildren(rail,template.content);rightRailHTML=html;
        if(focused&&rail.contains(focused)&&document.activeElement!==focused)focused.focus({preventScroll:true});
    }
    if(rail.getAttribute('aria-label')!==label)rail.setAttribute('aria-label',label);
    rail.hidden=false;$('.workspace').classList.add('has-document-rail');
    rail.scrollTop=sameContext?scroll:0;rightRailContext=contextId;
}
function datedRail(rows,label,selectedId){
    const ordered=[...rows].sort((a,b)=>(b.date||'').localeCompare(a.date||'')||(b.created||'').localeCompare(a.created||''));
    return `<div class="document-rail-heading"><h3>${esc(label)} <span>${ordered.length}</span></h3><small>Newest first</small></div><div class="dated-rail-list">${ordered.map(i=>`<button class="dated-rail-entry ${i.id===selectedId?'active':''}" data-item="${i.id}" aria-current="${i.id===selectedId?'true':'false'}" title="${esc(i.title)}">${esc(dateLabel(i))}</button>`).join('')||'<p class="muted">No dated entries yet.</p>'}</div>`;
}
function tagRail(item){
    const applied=new Set((item.tags||[]).map(t=>t.id));
    const linked=new Set((item.relationships||[]).map(r=>r.id));
    const connections=[['project','Projects'],['team','Teams'],['person','Related people']].map(([kind,label])=>{
        const group=entities.filter(e=>e.kind===kind&&!e.candidate&&e.profile_id!==item.id);
        return group.length?'<section class="rail-tag-group"><h4>'+label+'</h4>'+group.map(e=>'<label class="rail-tag"><input type="checkbox" data-entity-link="'+e.id+'" data-document-id="'+item.id+'" '+(linked.has(e.id)?'checked':'')+'><span>'+esc(e.name)+'</span></label>').join('')+'</section>':'';
    }).join('');
    const groups=[['topic','Topics'],['type','Types']].map(([category,label])=>{
        const group=visibleTags({kind:item.kind,tags}).filter(t=>t.category===category).sort((a,b)=>a.name.localeCompare(b.name));
        return group.length?'<section class="rail-tag-group"><h4>'+label+'</h4>'+group.map(t=>'<label class="rail-tag"><input type="checkbox" data-document-tag="'+t.id+'" data-document-id="'+item.id+'" '+(applied.has(t.id)?'checked':'')+'><span>'+esc(t.name)+'</span></label>').join('')+'</section>':'';
    }).join('');
    return '<div class="document-rail-heading"><h3>Connections</h3><button data-edit-connections="'+item.id+'">＋ Connect</button></div><div class="rail-tag-list">'+(connections||'<p class="muted">No connection records yet.</p>')+'</div><div class="document-rail-heading"><h3>Tags</h3><button id="create-document-tag">＋ New tag</button></div><div class="rail-tag-list">'+(groups||'<p class="muted">No topic tags yet.</p>')+'</div>';
}

let pendingTagItem=null;
async function toggleDocumentTag(input){
    const itemId=input.dataset.documentId,scroll=$('#reader').scrollTop,railScroll=$('#document-rail').scrollTop;
    input.disabled=true;
    try{await api('/api/apply',{item_ids:[itemId],tag_id:input.dataset.documentTag,remove:!input.checked});await refresh();if(active===itemId){await openItem(itemId);$('#reader').scrollTop=scroll;$('#document-rail').scrollTop=railScroll;}}
    catch(error){input.checked=!input.checked;throw error;}
    finally{input.disabled=false;}
}
// End navigation history.
async function refresh(background=false,resultsOnly=false){
    updateSearchClear();
    const activityView=view==='activity';
    $('#activity-panel').hidden=!activityView;
    $('.workspace').hidden=activityView;
    $('.toolbar').hidden=activityView;
    $('#filters').hidden=activityView;
    $('.header-actions').hidden=activityView;
    if(activityView){
        ++loadSerial;
        $('#changes-hint').hidden=true;
        $('#view-title').textContent='Activity';
        nav();
        await refreshActivity();
        return;
    }
    background=background===true;
    resultsOnly=resultsOnly&&catalogsLoaded;
    const serial=++loadSerial;const p=new URLSearchParams();p.set('q',$('#search').value);p.set('search_scope',$('#search-scope').value);p.set('sort',$('#sort-order').value);filters.forEach(t=>p.append('tag',t));
    let kind=$('#kind').value;if(view==='projects')kind='project';if(view==='people')kind='person';if(view==='documents')kind='document';if(view==='timeline')kind='update';if(view==='blogs'||view.startsWith('blog:'))kind='blog';
    if(['people','interviews','meetings'].includes(view))p.set('collection',view);
    if(kind)p.set('kind',kind);if(seriesFilter)p.set('series_id',seriesFilter);if(view.startsWith('blog:'))p.set('blog_feed_id',view.slice(5));
    const itemPath=view==='changes'?'/api/changed-documents?':view==='trash'?'/api/trash?':'/api/items?';
    const [newTags,newItems,newSeries,newFeeds,newEntities]=await Promise.all(resultsOnly?
        [tags,api(itemPath+p),series,blogFeeds,entities]:
        [api('/api/tags'),api(itemPath+p),api('/api/series'),api('/api/blogs'),api('/api/entities')]);
    if(serial!==loadSerial||(background&&refreshPaused()))return;tags=newTags;series=newSeries;items=newItems;blogFeeds=newFeeds;entities=newEntities;if(!resultsOnly)catalogsLoaded=true;
    if(seriesFilter&&!series.some(i=>i.id===seriesFilter)){seriesFilter='';return refresh(background);}
    if(view==='trash'&&kind)items=items.filter(i=>i.kind===kind);

    selected=new Set([...selected].filter(id=>items.some(i=>i.id===id)));
    $('#series-filter').innerHTML='<option value="">All meeting series</option>'+series.map(i=>`<option value="${i.id}">${esc(i.title)}</option>`).join('');
    $('#series-filter').value=seriesFilter;
    $('#series-names').innerHTML=series.map(i=>`<option value="${esc(i.title)}"></option>`).join('');
    if(view==='changes')$('#kind').value='document';
    $('#kind').disabled=view==='changes'||view==='blogs'||view.startsWith('blog:');$('#series-filter').hidden=view==='changes'||view==='blogs'||view.startsWith('blog:');
    $('#starred-only').disabled=view==='trash'||view==='changes';
    $('.starred-filter').hidden=view==='changes';$('#sort-order').disabled=view==='changes';
    $('#changes-hint').hidden=view!=='changes';
    $('#import-projects').innerHTML=tags.filter(t=>t.category==='project').map(t=>`<option value="${esc(t.name)}">${esc(t.name)}</option>`).join('');
    if(!resultsOnly){nav();sidebar();}list();$('#view-title').textContent=({all:'All notes',changes:'What’s changed',meetings:'Meetings',projects:'Projects',documents:'Documents',people:'People',interviews:'Interviews',blogs:'Feeds',timeline:'Weekly updates',trash:'Trash'})[view]||(blogFeeds.find(f=>view==='blog:'+f.id)?.name+' feed');

}
async function refreshSearch(){return refresh(false,true);}
async function openBlogFeed(id){
    active='blog:'+id;list();const feed=await api('/api/blogs/'+id);if(active!=='blog:'+id)return;
    if(feed.latest)return openItem(feed.latest.id);
    wikiContext=null;$('#reader').innerHTML=`<div class="reader-top"><div class="reader-meta">Feeds</div><button class="reader-close" id="close-reader" aria-label="Close feed">×</button></div><h2 class="reader-title">${esc(feed.name)}</h2><p class="muted">No roundups yet.</p>`;$('#close-reader').onclick=closeReader;showRightRail(datedRail(feed.editions,'Roundups',null),'Feed dates');
}
function sourceTitle(i){return esc(i.title);}
function readerMoreMenu(item){
    return '<details class="reader-more" id="reader-more"><summary aria-label="More actions">More…</summary><div class="reader-more-panel">'+(item.source_path?'<button id="show-source">View saved original</button>':'')+'<button class="danger" id="delete-note">Remove…</button></div></details>';
}
function sourceButtons(i){
    return (i.source_links||[]).filter(link=>link.primary).map(link=>
        `<a class="source-button" href="${esc(link.url)}" target="_blank" rel="noopener noreferrer">${link.kind==='document'?(link.url.startsWith('https://docs.google.com/')?'Open original Google Doc':'Open original Confluence page'):esc(link.label||'Open Calendar event')} ↗</a>`
    ).join('');
}
function additionalDocuments(i){
    const links=(i.source_links||[]).filter(link=>!link.primary);
    const documents=links.filter(link=>link.kind==='document');
    const meetings=links.filter(link=>link.kind==='calendar');
    const documentRows=documents.map(link=>`<div class="reference-row">${link.atlas_id?`<button class="reference-title" data-item="${esc(link.atlas_id)}" data-document-view="true">${esc(link.label)}</button><a class="reference-external" href="${esc(link.url)}" target="_blank" rel="noopener noreferrer" aria-label="Open ${esc(link.label)} in ${link.url.startsWith('https://docs.google.com/')?'Google Docs':'Confluence'}" title="Open in ${link.url.startsWith('https://docs.google.com/')?'Google Docs':'Confluence'}">↗</a>`:`<a class="reference-title" href="${esc(link.url)}" target="_blank" rel="noopener noreferrer">${esc(link.label)} ↗</a>`}</div>`).join('');
    const meetingRows=meetings.map(link=>`<div class="reference-row"><a class="reference-title" href="${esc(link.url)}" target="_blank" rel="noopener noreferrer">${link.date?`<span class="reference-date">${esc(new Date(link.date+'T12:00:00').toLocaleDateString(undefined,{month:'short',day:'numeric',year:'numeric'}))}</span>`:''}${esc(link.label||'Calendar event')} ↗</a></div>`).join('');
    return (documents.length?`<details class="reference-section"><summary>Referenced documents <span>(${documents.length})</span></summary><div class="reference-list">${documentRows}</div></details>`:'')+
        (meetings.length?`<details class="reference-section"><summary>Referenced meetings <span>(${meetings.length})</span></summary><div class="reference-list">${meetingRows}</div></details>`:'');
}
function renderedSummary(i){
    const leading=i.summary.match(/^\s*#{1,6}[ \t]+([^\r\n]+)(?:\r?\n|$)/);
    const normalize=title=>title.trim().replace(/[ \t]+#+$/,'').replace(/\*\*|__|`/g,'').replace(/\[([^\]]+)\]\([^)]*\)/g,'$1').replace(/\s+/g,' ').toLocaleLowerCase();
    const summary=leading&&normalize(leading[1])===normalize(i.title)?i.summary.slice(leading[0].length).replace(/^\s*\n/,''):i.summary;
    return markdown(summary);
}
function summaryExcerpt(i){
    const match=i.summary.match(/^## (?:Summary|Purpose)\s*\n([\s\S]*?)(?=^## |$(?![\s\S]))/mi);
    return match?match[1].trim():i.summary.split('\n').filter(line=>!line.startsWith('#')).join('\n').trim().slice(0,800);
}
async function showMeetingHistory(id){
    const opened=active,serial=loadSerial;
    const group=await api('/api/series/'+id);
    if(active!==opened||loadSerial!==serial)return;
    showRightRail(datedRail(group.sessions,'Meeting history',opened),'Meeting history: '+group.title);
    const rail=$('#document-rail');
    rail.setAttribute('tabindex','-1');
    rail.focus({preventScroll:true});
}
async function openSeries(id){
    active='series:'+id;list();const group=await api('/api/series/'+id);if(active!=='series:'+id)return;
    if(group.latest)return openItem(group.latest.id);
    wikiContext=null;$('#reader').innerHTML=`<div class="reader-top"><div class="reader-meta">Meeting series</div><button class="reader-close" id="close-reader" aria-label="Close series">×</button></div><h2 class="reader-title">${esc(group.title)}</h2><p class="muted">No sessions yet.</p>`;$('#close-reader').onclick=closeReader;showRightRail(datedRail(group.sessions,'Meetings',null),'Meeting dates');
}
function inline(s){let out=esc(s);out=out.replace(/`([^`]+)`/g,'<code>$1</code>').replace(/\*\*([^*]+)\*\*/g,'<strong>$1</strong>');out=out.replace(/\[\[([^\]|]+)(?:\|([^\]]+))?\]\]/g,(_,target,label)=>`<a class="wikilink" data-wiki="${target}">${label||target.split('/').pop()}</a>`);out=out.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g,'<a href="$2" target="_blank" rel="noopener noreferrer">$1 ↗</a>');return out;}
function markdown(text){const lines=text.split('\n');let html='',listOpen=false,code=false,table=false;const close=()=>{if(listOpen){html+='</ul>';listOpen=false;}if(table){html+='</tbody></table>';table=false;}};for(let n=0;n<lines.length;n++){const line=lines[n];if(line.startsWith('```')){close();html+=code?'</code></pre>':'<pre><code>';code=!code;continue;}if(code){html+=esc(line)+'\n';continue;}if(/^\|/.test(line)){if(/^\|\s*:?-/.test(line))continue;if(!table){close();html+='<table><tbody>';table=true;}const cells=line.replace(/^\||\|$/g,'').split('|');const isHead=n+1<lines.length&&/^\|\s*:?-/.test(lines[n+1]);html+='<tr>'+cells.map(c=>`<${isHead?'th':'td'}>${inline(c.trim())}</${isHead?'th':'td'}>`).join('')+'</tr>';continue;}if(/^\s*[-*] /.test(line)){if(table)close();if(!listOpen){html+='<ul>';listOpen=true;}let content=line.replace(/^\s*[-*] /,'');content=content.replace(/^\[([ xX])\] /,(_,v)=>v===' '?'☐ ':'☑ ');html+='<li>'+inline(content)+'</li>';continue;}close();const h=line.match(/^(#{1,6}) (.+)$/);if(h){html+=`<h${h[1].length}>${inline(h[2])}</h${h[1].length}>`;}else if(line.startsWith('> ')){html+='<blockquote>'+inline(line.slice(2))+'</blockquote>';}else if(line.trim()){html+='<p>'+inline(line)+'</p>';}}close();if(code)html+='</code></pre>';return html;}
function closeReader(){
    documentBrowse=null;railCollection=null;hideDocumentRail();active=null;
    wikiContext=null;
    sourceMode=false;
    $('#reader').innerHTML='<div class="empty"><span>◎</span><h2>Your work has a home.</h2><p>Select a note to explore its context and connections.</p></div>';
    $('#reader').scrollTop=0;
    list();
}
function projectMaterial(data){
    const grouped=new Map(),meetings=[];
    for(const item of [...data.meetings].sort((a,b)=>(b.date||'').localeCompare(a.date||''))){
        if(!item.series){meetings.push(item);continue;}
        let group=grouped.get(item.series.id);
        if(!group){group={...item,id:'series:'+item.series.id,title:item.series.title,kind:'meeting series',session_count:0};grouped.set(item.series.id,group);meetings.push(group);}
        group.starred=!!group.starred||!!item.starred;
        group.session_count++;
    }
    const sections=[['Meetings',sortedRows(meetings,false)],['Updates',sortedRows(data.updates,false)]];
    if(data.other_notes.length)sections.push(['Other notes',sortedRows(data.other_notes,false)]);
    return sections.map(([label,rows])=>`<section class="project-section"><h3>${label}<span>${rows.length}</span></h3>${rows.length?rows.map(item=>`<div class="authored-row"><button class="session-row" data-item="${item.id}"><span class="session-date">${esc(dateLabel(item))}</span><span>${esc(item.title)}${item.session_count?`<small> ${item.session_count} dated sessions</small>`:''}</span><span>→</span></button>${!item.id.startsWith('series:')?starControl(item):''}</div>`).join(''):`<p class="muted">No ${label.toLowerCase()} linked yet.</p>`}</section>`).join('');
}
function readerTag(t,projectId=null){
    const label=projectId?`<button class="context-link" data-item="${projectId}">${esc(t.name)}</button>`:esc(t.name);
    return `<span class="chip" data-category="${esc(t.category)}">${label}<button class="chip" data-remove-tag="${t.id}" aria-label="Remove ${esc(t.name)} tag" title="Remove tag">×</button></span>`;
}
async function openEntityRecord(id){
    const data=await api('/api/entity/'+id);
    if(active!=='entity:'+id)return;
    if(data.entity.profile_id)return openItem(data.entity.profile_id);
    $('#reader').innerHTML='<div class="reader-top"><div class="reader-meta">'+esc(data.entity.kind)+'</div><button class="reader-close" id="close-reader" aria-label="Close record">×</button></div><h2 class="reader-title">'+esc(data.title)+'</h2><p class="muted">Connected records appear alongside this entry.</p>';
    $('#close-reader').onclick=closeReader;
    $('#reader').scrollTop=0;
    showDocumentRail(data,data.entity.kind);
}
function connectionLink(record){
    return record.profile_id||record.id?'<button class="context-link" data-item="'+esc(record.profile_id||record.id)+'">'+esc(record.name)+'</button>':'<span>'+esc(record.name)+'</span>';
}
function relationshipContext(i){
    const filePeople=i.document_people||[];
    const explicitPeople=new Set([...(i.authors||[]),...(i.participants||[]),...filePeople].map(p=>p.id).filter(Boolean));
    const related=(i.relationships||[]).filter(r=>r.profile_id!==i.id&&!(r.kind==='person'&&explicitPeople.has(r.profile_id)));
    const row=(label,values,removable=false)=>values.length?'<div class="relationship-row"><span class="context-label">'+label+'</span><div>'+values.map(r=>'<span class="relationship-value">'+(r.profile_id?connectionLink(r):r.kind?'<button class="context-link" data-open-entity="'+r.id+'">'+esc(r.name)+'</button>':connectionLink(r))+(removable?'<button class="relationship-remove" data-remove-entity="'+r.id+'" aria-label="Disconnect '+esc(r.name)+'">×</button>':'')+'</span>').join('')+'</div></div>':'';
    const teams=related.filter(r=>r.kind==='team'),projects=related.filter(r=>r.kind==='project');
    let html=row('Participants',(i.participants||[]));
    if(['document','reference'].includes(i.kind)){
        html+='<div class="relationship-row"><span class="context-label">Authors</span><div>'+((i.authors||[]).map(connectionLink).join(', ')||'<span class="muted">Not recorded</span>')+' <button class="context-edit" id="edit-authors">Edit</button></div></div>';
    }
    html+=row('Owner',filePeople.filter(p=>p.role==='owner'));
    html+=row('Created by',filePeople.filter(p=>p.role==='creator'));
    html+=row('Mentioned people',filePeople.filter(p=>p.role==='mentioned'));
    if(!filePeople.some(p=>p.role==='owner')&&i.metadata?.document_people?.ownership_status){
        const status=i.metadata.document_people.ownership_status;
        html+='<div class="relationship-row"><span class="context-label">Owner</span><span class="muted">'+(status==='no_individual_owner'?'No individual owner reported':status==='available'?'Identity needs review':'Not provided by Google connector')+'</span></div>';
    }
    html+=row('Team'+(teams.length>1?'s':''),teams,true);
    html+=row(i.kind==='person'?'Works on':'Projects',projects,true);
    html+=row('Related people',related.filter(r=>r.kind==='person'&&!r.candidate),true);
    html+=row('Candidates',related.filter(r=>r.kind==='person'&&r.candidate));
    return '<section class="relationship-context" aria-label="Connections">'+html+'<button class="context-edit" data-edit-connections="'+i.id+'">Connect…</button></section>';
}
function documentContext(i){return relationshipContext(i);}

function setupSummaryToggle(item){
    let draft=item.summary,editing=false;
    const toggle=$('#summary-toggle');
    const capture=()=>{if(editing)draft=$('#note-content textarea').value;};
    const showSummary=()=>{
        capture();editing=false;sourceMode=false;
        toggle.textContent='Edit summary';toggle.setAttribute('aria-pressed','false');
        $('#note-content').innerHTML=(draft!==item.summary?'<p class="muted">Unsaved preview</p>':'')+renderedSummary({...item,summary:draft});
    };
    toggle.onclick=()=>{
        if(editing||sourceMode){showSummary();return;}
        editing=true;sourceMode=false;toggle.textContent='Summary';toggle.setAttribute('aria-pressed','true');
        const text=document.createElement('textarea');text.value=draft;
        text.setAttribute('aria-label','Edit summary');
        text.style.cssText='width:100%;height:50vh;font:12px/1.7 ui-monospace,monospace;padding:12px';
        const save=document.createElement('button');save.textContent='Save summary';save.className='primary';
        save.onclick=guarded(async()=>{
            save.disabled=true;
            try{
                await api('/api/summary',{item_id:item.id,summary:text.value,expected_revision:item.revision});
                await refresh();if(active===item.id)await openItem(item.id);
                toast('Summary saved. Previous version preserved.');
            }finally{save.disabled=false;}
        });
        $('#note-content').replaceChildren(text,save);text.focus();
    };
    return {showSource(text){
        capture();editing=false;sourceMode=true;toggle.textContent='Summary';toggle.setAttribute('aria-pressed','false');
        $('#note-content').innerHTML='<pre class="source-text">'+esc(text)+'</pre>';
    }};
}
async function openItem(id,background=false){if(view==='trash'){hideDocumentRail();return openTrashedItem(id);}if(id.startsWith('entity:')){active=id;list();return openEntityRecord(id.slice(7));}if(id.startsWith('blog:'))return openBlogFeed(id.slice(5));if(id.startsWith('series:'))return openSeries(id.slice(7));active=id;wikiContext=id;sourceMode=false;list();const i=await api('/api/item/'+id);if(active!==id||(background&&refreshPaused()))return;readerRevision=i.revision;$('#reader').innerHTML=`${browseDocumentHeader()}<div class="reader-top"><div class="reader-meta">${esc(i.is_interview_candidate?'Candidate':i.is_interview?'Interview':kindLabel(i.kind))} · <button class="date-edit" id="edit-date" title="Edit date">${esc(dateLabel(i))}</button>${i.source_channel?' · '+esc(i.source_channel):''}</div><button type="button" class="reader-close" id="close-reader" aria-label="Close note" title="Close note">×</button></div><h2 class="reader-title">${sourceTitle(i)}</h2>${['document','reference'].includes(i.kind)?'':`<div class="reader-tags">${visibleTags(i).map(t=>readerTag(t)).join('')}</div>`}<div class="reader-actions">${browseDocumentActions()}${sourceButtons(i)}<button id="star-note" aria-pressed="${!!i.starred}">${i.starred?'★ Starred':'☆ Star'}</button>${i.blog_feed?`<button data-open-blog="${i.blog_feed.id}">← ${esc(i.blog_feed.name)} feed</button>`:''}${i.series&&!documentBrowse?`<button data-meeting-history="${i.series.id}" title="View meeting history for ${esc(i.series.title)}" aria-controls="document-rail">View meeting history</button>`:''}<button id="summary-toggle" aria-pressed="false">Edit summary</button>${['meeting','one-on-one'].includes(i.kind)?'<button id="assign-series">Meeting series…</button>':''}${readerMoreMenu(i)}${documentRefreshControl(i)}</div>${relationshipContext(i)}${additionalDocuments(i)}<div class="content" id="note-content">${renderedSummary(i)}</div>${['project','person','team'].includes(i.kind)?'<div id="project-material"></div>':''}<div class="related"><h4>Connections & sources</h4>${i.links.map(l=>`<button data-wiki="${esc(l.target)}" ${l.id?'data-related="'+l.id+'"':''}>${esc(l.label)} ↗</button>`).join('')||'<p class="muted">Drag a team or project tag onto this note to connect it.</p>'}</div>`;$('#reader').scrollTop=0;$('#close-reader').onclick=closeReader;$('#edit-date').onclick=()=>{const form=$('#date-form');form.elements.item_id.value=id;form.elements.expected_revision.value=i.revision;form.elements.date.value=i.date;$('#date-dialog').showModal();};$('#star-note').onclick=guarded(()=>toggleStar(id,!!i.starred));if($('#refresh-request'))$('#refresh-request').onchange=guarded(async()=>{try{await api('/api/refresh-request',{item_id:id,requested:$('#refresh-request').checked});toast($('#refresh-request').checked?'Queued for the next hourly refresh.':'Refresh request canceled.');}finally{await openItem(id);}});if($('#edit-authors'))$('#edit-authors').onclick=()=>{const form=$('#authors-form');form.elements.item_id.value=id;form.elements.names.value=i.authors.map(a=>a.name).join(', ');$('#authors-dialog').showModal();};$('#delete-note').onclick=guarded(()=>{$('#reader-more').open=false;return reviewDelete(id);});if($('#assign-series'))$('#assign-series').onclick=()=>{const form=$('#series-form');form.elements.item_id.value=id;form.elements.title.value=i.series?.title||'';form.elements.cadence.value=i.series?.cadence||'';$('#series-dialog').showModal();};const summaryController=setupSummaryToggle(i);if($('#show-source'))$('#show-source').onclick=guarded(async()=>{$('#reader-more').open=false;const r=await fetch('/api/source/'+id,{headers:{'X-Atlas-Token':token}});if(!r.ok)throw Error('Could not open saved original.');const text=await r.text();if(active!==id)return;summaryController.showSource(text);});if(['project','person','team'].includes(i.kind)){const material=await api('/api/'+i.kind+'/'+id);if(active===id){$('#project-material').innerHTML=i.kind==='project'?projectMaterial(material):'';showDocumentRail(material,i.is_interview_candidate?'candidate':i.kind);}}else if(documentBrowse&&['meeting','one-on-one'].includes(i.kind)){showBrowseDocumentRail();}else if(['document','reference'].includes(i.kind)){if(documentBrowse){showBrowseDocumentRail();}else{showRightRail(tagRail(i),'Connections and tags');$('#create-document-tag').onclick=()=>{pendingTagItem=id;$('#tag-dialog').showModal();};}}else if(i.blog_feed){const feed=await api('/api/blogs/'+i.blog_feed.id);if(active===id)showRightRail(datedRail(feed.editions,'Roundups',id),'Feed dates');}else if(i.series){const group=await api('/api/series/'+i.series.id);if(active===id)showRightRail(datedRail(group.sessions,'Meetings',id),'Meeting dates');}else{hideDocumentRail();}}
async function reviewDelete(id){
    const preview=await api('/api/delete-preview/'+id);
    const form=$('#delete-form');form.elements.item_id.value=id;form.elements.expected_revision.value=preview.revision;
    $('#delete-title').textContent=preview.title;
    const affected=preview.affected_notes;
    $('#delete-impact').textContent=`This entry will be removed from Atlas and search results. The original Google document or source file is unchanged. Links to it in ${affected.length} other ${affected.length===1?'note':'notes'} will become plain text. You can restore it from Trash.`;
    $('#delete-affected').innerHTML=affected.length?'<ul>'+affected.map(i=>'<li>'+esc(i.title)+'</li>').join('')+'</ul>':'';
    $('#delete-dialog').showModal();
}
async function openTrashedItem(id){
    active=id;wikiContext=null;list();const i=await api('/api/trash/'+id);if(active!==id)return;
    $('#reader').innerHTML=`<div class="reader-top"><div class="reader-meta">In Trash · ${esc(i.deleted_at.slice(0,10))}</div><button type="button" class="reader-close" id="close-reader" aria-label="Close note" title="Close note">×</button></div><h2 class="reader-title">${esc(i.title)}</h2><div class="reader-actions"><button class="primary" id="restore-note">Restore note</button></div><p class="muted">This note is hidden from your library. Its sources and organization are retained for recovery.</p><div class="content">${renderedSummary(i).replace(/<a class="wikilink"[^>]*>(.*?)<\/a>/g,'$1')}</div>`;
    $('#reader').scrollTop=0;$('#close-reader').onclick=closeReader;
    $('#restore-note').onclick=guarded(async()=>{
        const result=await api('/api/restore',{item_id:id});view='all';filters=[];seriesFilter='';$('#search').value='';$('#kind').value='';await refresh();await openItem(id);
        toast(result.unchanged_edited_notes?'Note restored. Links in '+result.unchanged_edited_notes+' changed or deleted notes were left untouched.':'Note restored, including its original organization and recoverable links.');
    });
}
async function resolveWiki(target){const base=target.split('#')[0];const current=await api('/api/item/'+wikiContext);const link=current.links.find(l=>l.target===target);if(link&&link.id)return navigateItem(link.id);if(/\.(txt|vtt)$/i.test(base)||base.includes('Attachments/')){const r=await fetch('/api/attachment/'+wikiContext+'?target='+encodeURIComponent(base),{headers:{'X-Atlas-Token':token}});if(!r.ok)throw Error('This attachment is not available as text.');$('#note-content').innerHTML='<pre class="source-text">'+esc(await r.text())+'</pre>';return;}toast('No imported note matches “'+base+'”.');}
async function apply(ids,tagId,remove=false){const r=await api('/api/apply',{item_ids:ids,tag_id:tagId,remove});await refresh();if(active)await openItem(active);toast(r.changed?`${remove?'Removed':'Applied'} tag on ${r.changed} ${r.changed===1?'item':'items'}.`:'Tags already match.');}
async function toggleStar(id,wasStarred){
    const opened=active,scroll=$('#reader').scrollTop;await api('/api/starred',{item_id:id,starred:!wasStarred});await refresh();if(active===opened&&active){await openItem(active);if(documentBrowse)await refreshOpenContext(active);$('#reader').scrollTop=scroll;}
}
document.addEventListener('click',guarded(async e=>{const meetingsView=e.target.closest('[data-open-in-meetings]');if(meetingsView)return navigateItem(meetingsView.dataset.openInMeetings,false,false,true);const contextMeeting=e.target.closest('[data-context-meeting]');if(contextMeeting)return navigateRailMeeting(contextMeeting.dataset.item);const documentsView=e.target.closest('[data-open-in-documents]');if(documentsView)return navigateItem(documentsView.dataset.openInDocuments,true);const returnContext=e.target.closest('[data-return-context]');if(returnContext)return navigateItem(returnContext.dataset.returnContext);const contextDocument=e.target.closest('[data-context-document]');if(contextDocument)return navigateRailDocument(contextDocument.dataset.item);const more=$('#reader-more');if(more?.open&&!more.contains(e.target))more.open=false;const entityOpen=e.target.closest('[data-open-entity]');if(entityOpen)return navigateItem('entity:'+entityOpen.dataset.openEntity);const connect=e.target.closest('[data-edit-connections]');if(connect)return openConnectionDialog(connect.dataset.editConnections);
const disconnect=e.target.closest('[data-remove-entity]');if(disconnect)return changeConnection(active,disconnect.dataset.removeEntity,true);
const entityFilter=e.target.closest('[data-entity-filter]');if(entityFilter){const id=entityFilter.dataset.entityFilter;filters=filters.includes(id)?filters.filter(f=>f!==id):[...filters,id];return refresh();}
const meeting=e.target.closest('[data-meeting-item]');if(meeting){await navigateView('meetings');return navigateItem(meeting.dataset.meetingItem);}const activity=e.target.closest('[data-activity-item]');if(activity)return openActivityItem(activity.dataset.activityItem);const star=e.target.closest('[data-star]');if(star){e.preventDefault();return toggleStar(star.dataset.star,star.dataset.starred==='1');}const v=e.target.closest('[data-view]');if(v)return navigateView(v.dataset.view);const blogButton=e.target.closest('[data-open-blog]');if(blogButton)return navigateItem('blog:'+blogButton.dataset.openBlog);const historyButton=e.target.closest('[data-meeting-history]');if(historyButton)return showMeetingHistory(historyButton.dataset.meetingHistory);const seriesButton=e.target.closest('[data-open-series]');if(seriesButton)return navigateItem('series:'+seriesButton.dataset.openSeries);const select=e.target.closest('[data-select]');if(select){for(const id of rowIds(select.dataset.select)){if(select.checked)selected.add(id);else selected.delete(id);}list();return;}const remove=e.target.closest('[data-remove-tag]');if(remove)return apply([active],remove.dataset.removeTag,true);const tag=e.target.closest('[data-tag]');if(tag){if(view==='trash'||view==='blogs')view='all';const id=tag.dataset.tag;filters=filters.includes(id)?filters.filter(t=>t!==id):[...filters,id];return refresh();}const related=e.target.closest('[data-related]');if(related)return navigateItem(related.dataset.related);const wiki=e.target.closest('[data-wiki]');if(wiki)return resolveWiki(wiki.dataset.wiki);const item=e.target.closest('[data-item]');if(item)return navigateItem(item.dataset.item,item.dataset.documentView==='true',item.dataset.interviewView==='true');if(e.target.closest('[data-close]'))e.target.closest('dialog').close();}));
document.addEventListener('change',guarded(async e=>{if(e.target.matches('[data-entity-link]')){const input=e.target;input.disabled=true;try{await changeConnection(input.dataset.documentId,input.dataset.entityLink,!input.checked);}catch(error){input.checked=!input.checked;throw error;}finally{input.disabled=false;}}
else if(e.target.matches('[data-document-tag]'))await toggleDocumentTag(e.target);}));
document.addEventListener('keydown',guarded(async e=>{if(e.key==='Escape'){const more=$('#reader-more');if(more?.open){more.open=false;more.querySelector('summary').focus();return;}}if((e.metaKey||e.ctrlKey)&&e.key==='f'){e.preventDefault();$('#search').focus();}if(e.key==='Enter'&&e.target.matches('[data-item]')){e.preventDefault();if(e.target.dataset.contextMeeting==='true')await navigateRailMeeting(e.target.dataset.item);else if(e.target.dataset.contextDocument==='true')await navigateRailDocument(e.target.dataset.item);else await navigateItem(e.target.dataset.item,e.target.dataset.documentView==='true',e.target.dataset.interviewView==='true');}}));
document.addEventListener('dragstart',e=>{if(view==='trash'){e.preventDefault();return;}const tag=e.target.closest('[data-tag]');if(tag){e.dataTransfer.setData('application/x-atlas-tag',tag.dataset.tag);e.dataTransfer.effectAllowed='copy';return;}const item=e.target.closest('[data-item]');if(item){const row=rowIds(item.dataset.item);const ids=row.every(id=>selected.has(id))?[...selected]:row;e.dataTransfer.setData('application/x-atlas-items',JSON.stringify(ids));e.dataTransfer.effectAllowed='copy';}});
document.addEventListener('dragover',e=>{const target=e.target.closest('[data-item],.sidebar-tag,#reader');if(target&&[...e.dataTransfer.types].some(t=>t.startsWith('application/x-atlas-'))){e.preventDefault();target.classList.add('drop-target');}});
document.addEventListener('dragleave',e=>{const target=e.target.closest('.drop-target');if(target&&!target.contains(e.relatedTarget))target.classList.remove('drop-target');});
document.addEventListener('dragend',()=>document.querySelectorAll('.drop-target').forEach(t=>t.classList.remove('drop-target')));
document.addEventListener('drop',guarded(async e=>{if(view==='trash')return;document.querySelectorAll('.drop-target').forEach(t=>t.classList.remove('drop-target'));const tagId=e.dataTransfer.getData('application/x-atlas-tag'),ids=e.dataTransfer.getData('application/x-atlas-items');const item=e.target.closest('[data-item]'),tag=e.target.closest('.sidebar-tag');if(tagId&&(item||e.target.closest('#reader'))){e.preventDefault();const id=item?item.dataset.item:active;if(!id)return;const row=rowIds(id);if(!row.length)return toast('This collection has no notes yet.');await apply(item&&row.every(id=>selected.has(id))?[...selected]:row,tagId);}else if(ids&&tag){e.preventDefault();await apply(JSON.parse(ids),tag.dataset.tag);}}));
$('#nav-back').onclick=guarded(()=>travelHistory(-1));$('#nav-forward').onclick=guarded(()=>travelHistory(1));
let connectionItem=null;
function openConnectionDialog(itemId=null){
    connectionItem=itemId;
    const form=$('#connection-form');form.reset();
    $('#connection-existing').innerHTML='<option value="">Choose an existing record…</option>'+['person','team','project'].map(kind=>{
        const group=entities.filter(e=>e.kind===kind&&!e.candidate&&e.profile_id!==itemId);
        return '<optgroup label="'+(kind==='person'?'Related people':kind==='team'?'Teams':'Projects')+'">'+group.map(e=>'<option value="'+e.id+'">'+esc(e.name)+'</option>').join('')+'</optgroup>';
    }).join('');
    $('#connection-dialog').showModal();
}
async function changeConnection(itemId,entityId,remove=false){
    const readerScroll=$('#reader').scrollTop,railScroll=$('#document-rail').scrollTop;
    await api('/api/link-entity',{item_ids:[itemId],entity_id:entityId,remove});
    await refresh();if(active===itemId){await openItem(itemId);$('#reader').scrollTop=readerScroll;$('#document-rail').scrollTop=railScroll;}
}
$('#add-connection').onclick=()=>openConnectionDialog();
$('#connection-form').onsubmit=guarded(async event=>{
    event.preventDefault();
    const data=Object.fromEntries(new FormData(event.target));
    const target=connectionItem;
    const entity=data.name.trim()?await api('/api/entity',{name:data.name,kind:data.kind}):entities.find(e=>e.id===data.entity_id);
    if(!entity)throw Error('Choose an existing record or enter a name to create one.');
    if(target)await changeConnection(target,entity.id);else{await refresh();if(entity.profile_id)await navigateItem(entity.profile_id);}
    $('#connection-dialog').close();
});
function updateSearchClear(){
    $('#search-clear').hidden=!$('#search').value;
}
$('#search-clear').onclick=guarded(async()=>{
    clearTimeout(timer);
    $('#search').value='';
    updateSearchClear();
    $('#search').focus();
    await refreshSearch();
});
function updateSearchScope(){
    const label=$('#search-scope').value==='full_text'?'Search full text…':'Search titles and names…';
    $('#search').placeholder=label;
    $('#search').setAttribute('aria-label',label.replace('…',''));
}
$('#search-scope').onchange=guarded(async()=>{clearTimeout(timer);updateSearchScope();await refreshSearch();});
let timer;$('#search').oninput=()=>{updateSearchClear();clearTimeout(timer);timer=setTimeout(guarded(refreshSearch),180);};$('#kind').onchange=guarded(refresh);$('#series-filter').onchange=guarded(async()=>{seriesFilter=$('#series-filter').value;await refresh();});$('#clear-selection').onclick=()=>{selected.clear();list();};$('#undo').onclick=guarded(async()=>{const r=await api('/api/undo',{});await refresh();if(active)await openItem(active);toast(r.changed?'Last tag change undone.':'No tag changes to undo.');});$('#backup').onclick=guarded(async()=>{const r=await api('/api/backup',{});toast('Backup saved: '+r.path);});$('#add-tag').onclick=()=>{pendingTagItem=null;$('#tag-dialog').showModal();};$('#tag-form').onsubmit=guarded(async e=>{e.preventDefault();const target=pendingTagItem;const created=await api('/api/tag',Object.fromEntries(new FormData(e.target)));if(target)await api('/api/apply',{item_ids:[target],tag_id:created.id,remove:false});pendingTagItem=null;e.target.reset();$('#tag-dialog').close();await refresh();if(active)await openItem(active);toast(target?'Tag created and added to this document.':'Tag created. Drag it onto a note.');});$('#new-note').onclick=()=>{if(view==='documents')$('#import-kind').value='document';const context=items.find(i=>i.id===active&&i.kind==='project');for(const option of $('#import-projects').options)option.selected=filters.some(id=>tags.find(t=>t.id===id)?.category==='project'&&tags.find(t=>t.id===id)?.name===option.value)||(context&&(context.metadata.obsidian_stem||context.title)===option.value);if(view==='blogs'||view.startsWith('blog:')){$('#import-kind').value='blog';$('#import-form [name=blog_feed]').value=blogFeeds.find(f=>view==='blog:'+f.id)?.name||'AI';$('#blog-fields').hidden=false;const d=new Date();$('#import-form [name=date]').value=d.getFullYear()+'-'+String(d.getMonth()+1).padStart(2,'0')+'-'+String(d.getDate()).padStart(2,'0');}$('#import-dialog').showModal();};$('#import-kind').onchange=()=>{$('#period-fields').hidden=$('#import-kind').value!=='update';$('#blog-fields').hidden=$('#import-kind').value!=='blog';};$('#source-file').onchange=guarded(async()=>{const f=$('#source-file').files[0];if(!f)return;$('#import-form [name=title]').value=f.name.replace(/\.[^.]+$/,'');$('#import-form [name=summary]').value=await f.text();});$('#import-form').onsubmit=guarded(async e=>{e.preventDefault();const data=Object.fromEntries(new FormData(e.target));delete data[''];const file=$('#source-file').files[0];if(file){data.original_text=await file.text();data.original_name=file.name;const hash=await crypto.subtle.digest('SHA-256',new TextEncoder().encode(data.original_text));data.source_key='file:'+Array.from(new Uint8Array(hash),b=>b.toString(16).padStart(2,'0')).join('');}if(data.kind!=='update'){delete data.period_start;delete data.period_end;delete data.source_channel;}if(data.kind!=='blog')delete data.blog_feed;if(data.doc_url)data.doc_urls=[data.doc_url];delete data.doc_url;if(!data.calendar_url)delete data.calendar_url;if(!['meeting','one-on-one'].includes(data.kind)||!data.series_title){delete data.series_title;delete data.series_cadence;}data.authors=(data.author_names||'').split(',').map(name=>name.trim()).filter(Boolean);delete data.author_names;data.projects=Array.from($('#import-projects').selectedOptions,option=>option.value);data.tags=filters.map(id=>tags.find(t=>t.id===id)).filter(Boolean).map(t=>({name:t.name,category:t.category}));const r=await api('/api/import',data);$('#import-dialog').close();e.target.reset();$('#period-fields').hidden=true;$('#blog-fields').hidden=true;view='all';seriesFilter='';filters=[];if(r.trashed)view='trash';await refresh();await openItem(r.id);toast(r.trashed?'This source is in Trash. Restore it to return it to your library.':r.duplicate?'This source is already in Atlas.':'Saved to Atlas.');});
$('#delete-form').onsubmit=guarded(async e=>{e.preventDefault();const data=Object.fromEntries(new FormData(e.target));data.expected_revision=Number(data.expected_revision);const button=e.target.querySelector('[type=submit]');button.disabled=true;try{const result=await api('/api/delete',data);$('#delete-dialog').close();selected.delete(data.item_id);closeReader();await refresh();toast('Removed from Atlas. Cleaned links in '+result.cleaned_notes+' other notes. Restore it from Trash.');}finally{button.disabled=false;}});
$('#series-form').onsubmit=guarded(async e=>{e.preventDefault();const data=Object.fromEntries(new FormData(e.target));await api('/api/series',data);$('#series-dialog').close();await refresh();await openItem(data.item_id);toast(data.title?'Session grouped into '+data.title+'.':'Session removed from its series.');});
// Activity view.
let activityEvents=[],activityHasMore=false,activityLoading=false,activitySerial=0;
function activityOrder(a,b){return b.created.localeCompare(a.created)||b.id-a.id;}
function renderActivity(){
    const box=$('#activity-list'),scroll=box.scrollTop;
    const labels={imported:'Added',refreshed:'Refreshed',summary_updated:'Summary updated',updated:'Content updated',people_linked:'People linked',series_linked:'Meeting grouped',removed:'Moved to Trash',restored:'Restored'};
    box.innerHTML=activityEvents.length?activityEvents.map(event=>{
        const when=new Date(event.created).toLocaleString(undefined,{month:'short',day:'numeric',hour:'numeric',minute:'2-digit'});
        const title=event.historical?'Earlier addition reconstructed from its saved creation timestamp':labels[event.action]||event.action;
        return `<div class="activity-row"><time datetime="${esc(event.created)}" title="${esc(new Date(event.created).toLocaleString())}">${esc(when)}</time><span class="activity-category">${esc(event.category)}</span><span class="activity-action" title="${esc(title)}">${esc(labels[event.action]||event.action)}${event.historical?' · earlier':''}</span><button class="activity-item" data-activity-item="${esc(event.item_id)}" title="${esc(event.title)}">${esc(event.title)}</button></div>`;
    }).join(''):'<p class="muted activity-empty">Successful imports and content updates will appear here.</p>';
    box.scrollTop=scroll;
    $('#activity-more').hidden=!activityHasMore;
    $('#activity-more').disabled=activityLoading;
}
async function refreshActivity(older=false){
    if(older&&activityLoading)return;
    const serial=++activitySerial;
    activityLoading=true;
    try{
        const cursor=older&&activityEvents.length?activityEvents.at(-1).id:null;
        const page=await api('/api/activity?limit=50'+(cursor?'&before_id='+cursor:''));
        if(serial!==activitySerial)return;
        const previous=JSON.stringify(activityEvents);
        const merged=new Map(activityEvents.map(event=>[event.id,event]));
        page.events.forEach(event=>merged.set(event.id,event));
        activityEvents=[...merged.values()].sort(activityOrder);
        // A refreshed first page must not erase the pagination state of loaded history.
        if(older||activityEvents.length<=50)activityHasMore=page.has_more;
        if(previous!==JSON.stringify(activityEvents))renderActivity();
        $('#activity-status').textContent='Successful changes · newest first';
    }catch(error){
        if(serial===activitySerial)$('#activity-status').textContent='Activity unavailable — retrying on the next refresh';
        console.debug('Atlas activity deferred:',error.message);
    }finally{
        if(serial===activitySerial){activityLoading=false;$('#activity-more').disabled=false;$('#activity-more').hidden=!activityHasMore;}
    }
}
async function openActivityItem(id){
    let item,trashed=false;
    try{item=await api('/api/item/'+id);}
    catch(error){item=await api('/api/trash/'+id);trashed=true;}
    const target=trashed?'trash':item.is_interview_candidate||item.is_interview?'interviews':['document','reference'].includes(item.kind)?'documents':item.kind==='person'?'people':item.kind==='project'?'projects':item.kind==='blog'?'blogs':['meeting','one-on-one'].includes(item.kind)?'meetings':'all';
    await navigateView(target);
    await navigateItem(id);
}
$('#activity-more').onclick=()=>refreshActivity(true);
// End Activity view.

guarded(async()=>{await refresh();if(items.length)await openItem(displayedItems().find(i=>i.session_ids)?.id||items[0].id);})();

function updateBrowse(){list();}
$('#starred-only').onchange=guarded(updateBrowse);
$('#sort-order').onchange=guarded(updateBrowse);
$('#authors-form').onsubmit=guarded(async e=>{e.preventDefault();const form=e.target;const id=form.elements.item_id.value;await api('/api/authors',{item_id:id,authors:form.elements.names.value.split(',').map(name=>name.trim()).filter(Boolean)});$('#authors-dialog').close();await refresh();await openItem(id);toast('Authors saved.');});

$('#date-form').onsubmit=guarded(async e=>{e.preventDefault();const data=Object.fromEntries(new FormData(e.target));data.expected_revision=Number(data.expected_revision);await api('/api/date',data);$('#date-dialog').close();await refresh();await openItem(data.item_id);toast('Date updated.');});

// Automatic library refresh.
let libraryVersion=null,autoRefreshRunning=false;
function refreshPaused(){
    return document.hidden||!!document.querySelector('dialog[open], #note-content textarea')||
        document.activeElement?.matches('input, select, textarea, [contenteditable="true"]');
}
async function refreshOpenContext(id){
    if(!id||view==='trash')return;
    if(id.startsWith('blog:')){await openBlogFeed(id.slice(5));return;}
    if(id.startsWith('entity:')){await openEntityRecord(id.slice(7));return;}
    if(id.startsWith('series:')){await openSeries(id.slice(7));return;}
    const item=await api('/api/item/'+id);
    if(active!==id||refreshPaused())return;
    // Preserve source/attachment text while still updating the surrounding lists.
    if(item.revision!==readerRevision&&!$('#note-content pre')){
        await openItem(id,true);return;
    }
    if(documentBrowse){
        const context=documentBrowse,material=await api(context.endpoint);
        if(active!==id||documentBrowse?.id!==context.id||refreshPaused())return;
        documentBrowse={...context,data:material,title:material.title||context.title};
        showBrowseDocumentRail();return;
    }
    if(['project','person','team'].includes(item.kind)){
        const material=await api('/api/'+item.kind+'/'+id);
        if(active!==id||refreshPaused())return;
        if(item.kind==='project')$('#project-material').innerHTML=projectMaterial(material);
        showDocumentRail(material,item.is_interview_candidate?'candidate':item.kind);
    }else if(['document','reference'].includes(item.kind)){
        showRightRail(tagRail(item),'Connections and tags');
        $('#create-document-tag').onclick=()=>{pendingTagItem=id;$('#tag-dialog').showModal();};
    }else if(item.blog_feed){
        const feed=await api('/api/blogs/'+item.blog_feed.id);
        if(active===id&&!refreshPaused())showRightRail(datedRail(feed.editions,'Roundups',id),'Feed dates');
    }else if(item.series){
        const group=await api('/api/series/'+item.series.id);
        if(active===id&&!refreshPaused())showRightRail(datedRail(group.sessions,'Meetings',id),'Meeting dates');
    }
}
async function autoRefresh(){
    if(autoRefreshRunning||refreshPaused())return;
    autoRefreshRunning=true;
    try{
        if(view==='activity'){await refreshActivity();return;}
        const version=JSON.stringify(await api('/api/changes'));
        if(version===libraryVersion||refreshPaused())return;
        const state=navigationState(),serial=loadSerial;
        await refresh(true);
        if(loadSerial!==serial+1||active!==state.active||view!==state.view||refreshPaused())return;
        try{await refreshOpenContext(state.active);}
        finally{
            if(active===state.active&&view===state.view){
                $('#reader').scrollTop=state.readerScroll;
                $('.results').scrollTop=state.listScroll;
                $('#document-rail').scrollTop=state.railScroll;
            }
        }
        libraryVersion=version;
    }catch(error){
        // A sleeping Mac or temporarily unavailable service retries on the next check.
        console.debug('Atlas automatic refresh deferred:',error.message);
    }finally{autoRefreshRunning=false;}
}
setInterval(autoRefresh,15000);
window.addEventListener('focus',autoRefresh);
document.addEventListener('visibilitychange',()=>{if(!document.hidden)autoRefresh();});
// End automatic library refresh.
