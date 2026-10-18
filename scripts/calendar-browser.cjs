// Synthetic Calendar browser qualification. Start TestCalendarBrowserFixture with
// ALEXANDRIA_CALENDAR_BROWSER set to the same empty output directory.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs=require('fs');const dir=process.env.ALEXANDRIA_CALENDAR_BROWSER;const base=fs.readFileSync(dir+'/url','utf8');
(async()=>{
 const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});const results=[];
 for(const [device,width,height] of [['desktop',1440,1000],['phone',390,844],['palma',412,824],['lumi',1100,1467]]) {
  const ctx=await browser.newContext({viewport:{width,height}});const page=await ctx.newPage();const errors=[];page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>{if(m.type()==='error'&&!m.text().includes('favicon'))errors.push(m.text())});
  for(const lang of ['en','es'])for(const order of ['upcoming','calendar'])for(const view of ['month','week','day']){
   await ctx.addCookies([{name:'alexandria_lang',value:lang,url:base}]);await page.goto(`${base}/calendar?view=${view}&order=${order}`);
   if(await page.locator('h1').count()!==1)throw Error('Render failed');
   const selected=await page.locator('#calendar-grid .button[aria-current=true]').evaluate(e=>getComputedStyle(e).backgroundColor);
   const unselected=await page.locator('#calendar-grid .button[aria-current=false]').first().evaluate(e=>getComputedStyle(e).backgroundColor);
   if(selected===unselected)throw Error('Selected view lacks visible styling');
   if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Page overflow '+device+'/'+view);
   const a=await page.locator('#upcoming').boundingBox(),b=await page.locator('#calendar-grid').boundingBox();if((a.y<b.y)!==(order==='upcoming'))throw Error('Section order');
   if(view==='month'&&await page.locator('.calendar-more').count()===0)throw Error('Missing overflow');
   if(view==='week'&&width<1000&&!await page.locator('.calendar-time-scroll').evaluate(e=>e.scrollWidth>e.clientWidth))throw Error('Week should scroll');
   if(view==='day') {const blocks=await page.locator('.calendar-block').count();if(blocks<5)throw Error('Missing timed events');const positions=await page.locator('.calendar-block').evaluateAll(es=>es.slice(0,5).map(e=>e.getBoundingClientRect().x));if(new Set(positions).size<4)throw Error('Overlaps not partitioned');}
   if(lang==='en'&&order==='calendar')await page.screenshot({path:`${dir}/${device}-${view}.png`,fullPage:true});
   results.push({device,lang,order,view,overflow:false});
  }
  if(errors.length)throw Error(errors.join('\n'));await ctx.close();
 }
 const ctx=await browser.newContext({viewport:{width:390,height:844},javaScriptEnabled:false});const page=await ctx.newPage();await page.goto(base+'/calendar?view=month&order=calendar&week=monday');
 await page.locator('.calendar-more').first().click();if(!page.url().includes('view=day'))throw Error('More link');
 const returnURL=page.url();await page.locator('.calendar-slot').nth(10).click({position:{x:15,y:10}});await page.getByRole('link',{name:'Cancel',exact:true}).click();if(new URL(page.url()).search!==new URL(returnURL).search)throw Error('Cancel return');
 await page.locator('.calendar-slot').nth(10).click({position:{x:15,y:10}});await page.locator('input[name=title]').fill('Browser synthetic event');await page.getByRole('button',{name:'Save',exact:true}).click();
 if(!page.url().includes('view=day')||!page.url().includes('order=calendar'))throw Error('Create return');
 const event=page.locator('#upcoming li').filter({hasText:'Browser synthetic event'});await event.getByRole('link',{name:'Edit',exact:true}).click();await page.locator('input[name=title]').fill('Browser synthetic updated');await page.getByRole('button',{name:'Save',exact:true}).click();
 await page.locator('#upcoming li').filter({hasText:'Browser synthetic updated'}).getByRole('button',{name:'Delete',exact:true}).click();if(await page.locator('#upcoming').getByText('Browser synthetic updated').count())throw Error('Delete failed');
 await page.goto(base+'/calendar');if(!await page.locator('a[aria-current=true]').filter({hasText:'Day'}).count())throw Error('Preference not remembered');
 await page.locator('#upcoming li').filter({hasText:'Synthetic recurring appointment'}).first().getByRole('link',{name:'Event details'}).click();if(await page.locator('input[name=title]').count())throw Error('Series editable');
 await ctx.close();
 const c2=await browser.newContext({viewport:{width:390,height:844}});const p2=await c2.newPage();await p2.goto(base+'/calendar');if(!await p2.getByRole('link',{name:'Month',exact:true}).getAttribute('aria-current').then(x=>x==='true'))throw Error('Cookie isolation');
 await p2.locator('.caldav-connection summary').click();await p2.evaluate(()=>Object.defineProperty(navigator,'clipboard',{value:{writeText:()=>Promise.reject(Error('denied'))},configurable:true}));await p2.locator('[data-copy]').first().click();if(!await p2.locator('[data-copy-status]').textContent().then(x=>x.includes('selected')))throw Error('Clipboard fallback');
 const selection=await p2.locator('.copy-address input').first().evaluate(e=>e.selectionEnd-e.selectionStart);if(selection===0)throw Error('Clipboard text not selected');
 await p2.evaluate(()=>Object.defineProperty(navigator,'clipboard',{value:{writeText:async value=>{window.copiedAddress=value}},configurable:true}));await p2.locator('[data-copy]').first().click();if(await p2.evaluate(()=>window.copiedAddress)!==base+'/caldav/')throw Error('Clipboard value');
 await p2.keyboard.press('Tab');if(!await p2.evaluate(()=>document.activeElement.matches(':focus-visible')))throw Error('Keyboard focus');
 await p2.emulateMedia({colorScheme:'dark'});await p2.goto(base+'/calendar?view=month&order=calendar');await p2.screenshot({path:dir+'/phone-dark.png',fullPage:true});
 results.push({noJavaScript:'create/edit/delete/navigation/recurring details passed',preferences:'persistence and isolation passed',clipboard:'success and fallback selected text',keyboard:'visible focus',dark:'rendered'});
 fs.writeFileSync(dir+'/results.json',JSON.stringify(results,null,2));console.log('Passed '+results.length+' browser checks');await browser.close();fs.writeFileSync(dir+'/stop','done');
})().catch(e=>{console.error(e);fs.writeFileSync(dir+'/stop','failed');process.exit(1)});
