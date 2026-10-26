const fs=require('fs'),vm=require('vm'),assert=require('assert');
const src=fs.readFileSync('web/app.js','utf8');let acknowledgements=0,rail='',scrolls=0,focuses=0;
const nodes={'#reader':{innerHTML:'',scrollTop:90},'#close-reader':{}};
const archive={open:false};const section={closest:()=>archive,scrollIntoView:()=>scrolls++,focus:()=>focuses++};
const ctx={console,Number,Date,$:key=>nodes[key],esc:s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])),markdown:s=>'<p>'+String(s).replace(/</g,'&lt;')+'</p>',closeReader(){},showRightRail:html=>{rail=html},acknowledgeFeed:async()=>{acknowledgements++},document:{getElementById:id=>id==='feed-entry-one'?section:null}};
vm.createContext(ctx);vm.runInContext("var active='blog:feed',wikiContext=null,sourceMode=false;",ctx);
vm.runInContext(src.slice(src.indexOf('function feedEntryFields'),src.indexOf('async function openBlogFeed')),ctx);
const item={id:'one',title:'<script>Bad title</script>',summary:'Short synopsis',metadata:{feed_entry:{observed_at:'2026-10-26T00:00:00Z',source_url:'https://example.com',fields:{external_id:'123',severity:2,status:'stable'}}}};
const feed={id:'feed',name:'Incidents',entry_label:'Incidents',checked_at:'2026-10-26T00:00:00Z',overview_entries:[item],archived_entries:[]};
(async()=>{
 await ctx.showFeedOverview(feed,true);assert.equal(acknowledgements,0);assert.equal(nodes['#reader'].scrollTop,90);
 assert(rail.includes('data-feed-jump="one"'));assert(rail.includes('#123'));assert(rail.includes('SEV-2'));assert(rail.includes('Stable'));
 assert(!nodes['#reader'].innerHTML.includes('<script>'));assert(nodes['#reader'].innerHTML.includes('id="feed-entry-one"'));
 await ctx.jumpToFeedEntry('feed','one');assert(archive.open);assert.equal(scrolls,1);assert.equal(focuses,1);
 await ctx.showFeedOverview(feed,false);assert.equal(acknowledgements,1);assert.equal(nodes['#reader'].scrollTop,0);
 assert(ctx.feedStatusBadge({}).includes('Unknown'));assert.equal(ctx.feedSeverity({}),'Severity unknown');
 assert(src.includes("openBlogFeed(id.slice(5),true)"));assert(src.includes("'data-feed-jump'"));
 console.log('Overview status index, section jumps, escaping, stable rail keys and background unread preservation verified.');
})().catch(e=>{console.error(e);process.exit(1)});
