const fs=require('fs'),vm=require('vm'),assert=require('assert');
const src=fs.readFileSync('web/app.js','utf8');let calls=[],renders=0;
const ctx={console,api:async(path,data)=>{calls.push({path,data});return {latest_entry_id:8,unseen_entries:1}},list(){renders++}};
vm.createContext(ctx);vm.runInContext("var active='edition',blogFeeds=[{id:'feed',unseen_entries:2}];",ctx);
vm.runInContext(src.slice(src.indexOf('function feedUnreadStatus'),src.indexOf('function documentChangeStatus')),ctx);
(async()=>{
 assert(ctx.feedUnreadStatus({kind:'feed',unseen_entries:2}).includes('aria-label="New feed entries"'));
 assert.equal(ctx.feedUnreadStatus({kind:'blog',unseen_entries:2}),'');
 assert.equal(ctx.feedUnreadStatus({kind:'feed',unseen_entries:0}),'');
 await ctx.acknowledgeFeed({id:'feed',latest_entry_id:7,unseen_entries:2},'edition');
 assert.equal(calls[0].data.through_id,7);assert.equal(calls[0].path,'/api/feed-seen');
 assert.equal(vm.runInContext('blogFeeds[0].unseen_entries',ctx),1); // Concurrent new arrival remains dotted.
 await ctx.acknowledgeFeed({id:'feed',latest_entry_id:8,unseen_entries:1},'another');
 assert.equal(calls.length,1);assert.equal(renders,1);
 assert(src.includes('if(!background)await acknowledgeFeed(feed,id)'));
 console.log('Feed dots acknowledge only displayed arrival cursors; background reads never clear them.');
})().catch(e=>{console.error(e);process.exit(1)});
