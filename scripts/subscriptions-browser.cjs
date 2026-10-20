// Start TestSubscriptionBrowserFixture with ALEXANDRIA_SUBSCRIPTIONS_BROWSER
// pointing to an empty scratch directory, then run this driver against its URL.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs=require('fs');
const dir=process.env.ALEXANDRIA_SUBSCRIPTIONS_BROWSER;
const base=fs.readFileSync(dir+'/url','utf8');
(async()=>{
 const browser=await chromium.launch({executablePath:process.env.CHROME_PATH || '/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
 const results=[];
 try {
  for(const [device,width,height] of [['desktop',1440,1000],['phone',390,844],['palma',412,824],['lumi',1100,1467]]){
   const ctx=await browser.newContext({viewport:{width,height}}), page=await ctx.newPage();
   const errors=[];page.on('pageerror',e=>errors.push(e.message));
   for(const lang of ['en','es'])for(const order of ['upcoming','calendar'])for(const view of ['month','week','day']){
    await ctx.addCookies([{name:'alexandria_lang',value:lang,url:base}]);
    await page.goto(`${base}/calendar?view=${view}&order=${order}`);
    if(await page.locator('h1').count()!==1 || !await page.locator('#upcoming').getByText('Subscribed event 0',{exact:true}).count())throw Error('Calendar render');
    if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Overflow '+device+'/'+view);
    const a=await page.locator('#upcoming').boundingBox(),b=await page.locator('#calendar-grid').boundingBox();
    if((a.y<b.y)!==(order==='upcoming'))throw Error('Section order');
    if(view==='week'&&width<1000&&!await page.locator('.calendar-time-scroll').evaluate(e=>e.scrollWidth>e.clientWidth))throw Error('Week scroll');
    if(view==='day'&&await page.locator('.calendar-block .calendar-source-name').count()<2)throw Error('Source labels missing');
    if(lang==='en'&&order==='calendar'&&((device==='desktop'&&view==='day')||(device==='phone'&&view==='month')||(device==='lumi'&&view==='week'))){
     await page.locator('#calendar-grid').scrollIntoViewIfNeeded();await page.screenshot({path:`${dir}/${device}-${view}.png`});
    }
    results.push({device,lang,order,view,overflow:false});
   }
   for(const lang of ['en','es']){
    await ctx.addCookies([{name:'alexandria_lang',value:lang,url:base}]);
    await page.goto(base+'/settings/integrations');
    if(await page.locator('#subscriptions').getAttribute('open')!==null)throw Error('Tile starts expanded');
    await page.locator('#subscriptions summary').click();
    if(!await page.locator('#subscriptions input[name=feed_url]').isVisible())throw Error('Tile inaccessible');
    if(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+1))throw Error('Settings overflow');
    results.push({device,lang,settings:true});
   }
   await ctx.addCookies([{name:'alexandria_lang',value:'en',url:base}]);
   await page.goto(base+'/calendar?view=month&order=calendar');
   const selected=await page.locator('input[name=date]').inputValue();
   const nextDate=new Date(selected+'T12:00:00Z');nextDate.setUTCDate(nextDate.getUTCDate()+3);
   await page.goto(base+'/calendar?view=month&order=calendar&date='+nextDate.toISOString().slice(0,10));
   const longTitle=page.locator('.calendar-entry-title').filter({hasText:'Introduction to woodworking'}).first();
   if(!await longTitle.count())throw Error('Missing long title fixture');
   const dimensions=await longTitle.evaluate(e=>({height:e.getBoundingClientRect().height,font:parseFloat(getComputedStyle(e).fontSize),whiteSpace:getComputedStyle(e).whiteSpace}));
   if(dimensions.whiteSpace==='nowrap'||dimensions.height<dimensions.font*2)throw Error('Title does not wrap '+device);
   await page.locator('#calendar-grid').scrollIntoViewIfNeeded();
   if(device==='desktop'||device==='phone')await page.screenshot({path:dir+'/'+device+'-month-readable.png'});
   results.push({device,wrappedMonthTitle:true});
   if(errors.length)throw Error(errors.join('\n'));await ctx.close();
  }
  const ctx=await browser.newContext({viewport:{width:390,height:844},javaScriptEnabled:false});const page=await ctx.newPage();
  await page.goto(base+'/calendar?view=day&order=calendar&week=monday');
  if(await page.locator('#upcoming').getByText('Beyond the first seven days',{exact:true}).count())throw Error('Default is not seven days');
  if(await page.locator('#past').evaluate(e=>e.open)||await page.locator('#upcoming .event-description[open]').count())throw Error('Long content not folded');
  await page.getByRole('link',{name:'14 days',exact:true}).click();
  if(!await page.locator('#upcoming').getByText('Beyond the first seven days',{exact:true}).count())throw Error('Range did not expand');
  await page.getByRole('link',{name:'Next 7 days',exact:true}).click();
  const entry=page.locator('#upcoming li').filter({hasText:'Subscribed event 0'});
  await entry.getByRole('link',{name:'Event details',exact:true}).click();
  if(!await page.getByText('This event belongs to a subscribed calendar and is read-only.',{exact:true}).isVisible()||await page.locator('input[name=title]').count())throw Error('Read-only details');
  await page.getByRole('link',{name:'Back to calendar',exact:true}).click();
  if(!page.url().includes('view=day')||!page.url().includes('order=calendar'))throw Error('Return state');
  await page.locator('#list-filters summary').focus();await page.keyboard.press('Enter');
  if(!await page.locator('#list-filters').evaluate(e=>e.open))throw Error('Keyboard disclosure');
  await page.locator('#list-filters').getByLabel('Community calendar',{exact:true}).uncheck();await page.getByRole('button',{name:'Apply',exact:true}).click();
  if(await page.locator('#upcoming').getByText('Subscribed event 0',{exact:true}).count())throw Error('Hide source');
  await page.reload();if(await page.locator('#upcoming').getByText('Subscribed event 0',{exact:true}).count())throw Error('Cookie persistence');
  const isolated=await browser.newContext();const other=await isolated.newPage();await other.goto(base+'/calendar');
  if(!await other.locator('#upcoming').getByText('Subscribed event 0',{exact:true}).count())throw Error('Browser isolation');await isolated.close();
  if(!await page.locator('#list-filters').evaluate(e=>e.open))await page.locator('#list-filters summary').click();await page.locator('#list-filters').getByLabel('Community calendar',{exact:true}).check();await page.getByRole('button',{name:'Apply',exact:true}).click();
  const filters=scope=>page.locator('#'+scope+'-filters');
  const openFilters=async scope=>{if(!await filters(scope).evaluate(e=>e.open))await filters(scope).locator('summary').first().click();};
  await openFilters('grid');await filters('grid').getByLabel('Local calendar',{exact:true}).uncheck();await filters('grid').getByRole('button',{name:'Apply',exact:true}).click();
  if(await page.locator('#calendar-grid').getByText('Synthetic appointment 1',{exact:true}).count()||!await page.locator('#upcoming').getByText('Synthetic appointment 1',{exact:true}).count())throw Error('Grid filter changed list');
  await openFilters('list');await filters('list').getByLabel('Search events',{exact:true}).fill('Subscribed event 0');await filters('list').getByRole('button',{name:'Apply',exact:true}).click();
  if(!await page.locator('#upcoming').getByText('Subscribed event 0',{exact:true}).count()||await page.locator('#upcoming').getByText('Subscribed event 1',{exact:true}).count()||!await page.locator('#calendar-grid').getByText('Subscribed recurring event',{exact:true}).count())throw Error('Text search not independent');
  await page.getByRole('link',{name:'Next',exact:true}).click();
  if(!await page.locator('#upcoming').getByText('Subscribed event 0',{exact:true}).count()||await filters('grid').getByLabel('Local calendar',{exact:true}).isChecked())throw Error('Navigation lost filters');
  await page.getByRole('link',{name:'Today',exact:true}).click();
  await openFilters('grid');await filters('grid').getByRole('button',{name:'Clear filters',exact:true}).click();
  if(!await page.locator('#calendar-grid').getByText('Synthetic appointment 1',{exact:true}).count()||await page.locator('#upcoming').getByText('Synthetic appointment 1',{exact:true}).count())throw Error('Reset affected other section');
  await openFilters('list');await filters('list').getByRole('button',{name:'Clear filters',exact:true}).click();
  results.push({independentSourceFilters:true,independentTextSearch:true,filterNavigation:true,filterReset:true,sevenDayDefault:true,expandableRange:true,collapsedDescriptionsAndPast:true});
  await page.getByRole('link',{name:'Subscribe to calendar',exact:true}).click();
  const card=page.locator('.subscription-card');await card.getByRole('button',{name:'Refresh now',exact:true}).click();
  if(!page.url().includes('section=subscriptions'))throw Error('Refresh return');
  await page.locator('.subscription-card').getByLabel('Enable calendar and automatic refresh').uncheck();await page.locator('.subscription-card').getByRole('button',{name:'Save',exact:true}).click();
  if(!await page.locator('.subscription-card').getByText('Paused',{exact:true}).isVisible())throw Error('Pause');
  await page.locator('.subscription-card').getByLabel('Enable calendar and automatic refresh').check();await page.locator('.subscription-card').getByRole('button',{name:'Save',exact:true}).click();
  await page.locator('.subscription-card').getByLabel('Remove this subscription and its cached events').check();await page.locator('.subscription-card').getByRole('button',{name:'Unsubscribe',exact:true}).click();
  if(await page.locator('.subscription-card').count())throw Error('Unsubscribe');
  for(const [path,message] of [['page','No calendar feed was found'],['multiple','This page contains several calendars']]) {
   await page.locator('#subscriptions input[name=name]').fill('Rejected synthetic feed');
   await page.getByLabel('Calendar link',{exact:true}).fill('https://example.org/'+path);
   await page.getByRole('button',{name:'Subscribe to calendar',exact:true}).click();
   if(!await page.getByText(message,{exact:false}).isVisible() || await page.locator('.subscription-card').count())throw Error('Discovery error '+path);
  }
  for(const link of ['https://example.org/synthetic.ics','https://calendar.google.com/calendar?cid=YUBi','https://example.org/calendar-page']) {
   await page.locator('#subscriptions input[name=name]').fill('Browser synthetic feed');
   await page.getByLabel('Calendar link',{exact:true}).fill(link);
   await page.getByRole('button',{name:'Subscribe to calendar',exact:true}).click();
   if(!await page.getByText('Waiting for the first calendar refresh.',{exact:true}).isVisible())throw Error('Subscribe '+link);
   await page.locator('.subscription-card').getByLabel('Remove this subscription and its cached events').check();
   await page.locator('.subscription-card').getByRole('button',{name:'Unsubscribe',exact:true}).click();
  }
  await ctx.close();results.push({noJavaScript:true,keyboardDisclosure:true,details:true,visibility:true,isolation:true,subscribe:true,googleShare:true,webpageDiscovery:true,discoveryErrors:true,pause:true,refresh:true,unsubscribe:true});
  const dark=await browser.newContext({viewport:{width:390,height:844},colorScheme:'dark'});const dp=await dark.newPage();await dp.goto(base+'/calendar?view=month&order=calendar');
  await dp.screenshot({path:dir+'/phone-dark.png'});await dark.close();results.push({dark:true});
  fs.writeFileSync(dir+'/browser.json',JSON.stringify({checks:results.length,results},null,2));
  console.log(JSON.stringify({checks:results.length,passed:true}));
 } finally {await browser.close();fs.writeFileSync(dir+'/stop','done');}
})().catch(e=>{console.error(e);process.exit(1)});
