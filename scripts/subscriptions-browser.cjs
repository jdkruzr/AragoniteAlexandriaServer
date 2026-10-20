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
   if(errors.length)throw Error(errors.join('\n'));await ctx.close();
  }
  const ctx=await browser.newContext({viewport:{width:390,height:844},javaScriptEnabled:false});const page=await ctx.newPage();
  await page.goto(base+'/calendar?view=day&order=calendar&week=monday');
  const entry=page.locator('#upcoming li').filter({hasText:'Subscribed event 0'});
  await entry.getByRole('link',{name:'Event details',exact:true}).click();
  if(!await page.getByText('This event belongs to a subscribed calendar and is read-only.',{exact:true}).isVisible()||await page.locator('input[name=title]').count())throw Error('Read-only details');
  await page.getByRole('link',{name:'Back to calendar',exact:true}).click();
  if(!page.url().includes('view=day')||!page.url().includes('order=calendar'))throw Error('Return state');
  await page.locator('.calendar-sources summary').focus();await page.keyboard.press('Enter');
  if(!await page.locator('.calendar-sources').evaluate(e=>e.open))throw Error('Keyboard disclosure');
  await page.getByLabel('Community calendar',{exact:true}).uncheck();await page.getByRole('button',{name:'Apply',exact:true}).click();
  if(await page.locator('#upcoming').getByText('Subscribed event 0',{exact:true}).count())throw Error('Hide source');
  await page.reload();if(await page.locator('#upcoming').getByText('Subscribed event 0',{exact:true}).count())throw Error('Cookie persistence');
  const isolated=await browser.newContext();const other=await isolated.newPage();await other.goto(base+'/calendar');
  if(!await other.locator('#upcoming').getByText('Subscribed event 0',{exact:true}).count())throw Error('Browser isolation');await isolated.close();
  await page.locator('.calendar-sources summary').click();await page.getByLabel('Community calendar',{exact:true}).check();await page.getByRole('button',{name:'Apply',exact:true}).click();
  await page.getByRole('link',{name:'Subscribe to calendar',exact:true}).click();
  const card=page.locator('.subscription-card');await card.getByRole('button',{name:'Refresh now',exact:true}).click();
  if(!page.url().includes('section=subscriptions'))throw Error('Refresh return');
  await page.locator('.subscription-card').getByLabel('Enable calendar and automatic refresh').uncheck();await page.locator('.subscription-card').getByRole('button',{name:'Save',exact:true}).click();
  if(!await page.locator('.subscription-card').getByText('Paused',{exact:true}).isVisible())throw Error('Pause');
  await page.locator('.subscription-card').getByLabel('Enable calendar and automatic refresh').check();await page.locator('.subscription-card').getByRole('button',{name:'Save',exact:true}).click();
  await page.locator('.subscription-card').getByLabel('Remove this subscription and its cached events').check();await page.locator('.subscription-card').getByRole('button',{name:'Unsubscribe',exact:true}).click();
  if(await page.locator('.subscription-card').count())throw Error('Unsubscribe');
  await page.locator('#subscriptions input[name=name]').fill('Browser synthetic feed');await page.locator('input[name=feed_url]').fill('https://example.org/synthetic.ics');
  await page.getByRole('button',{name:'Subscribe to calendar',exact:true}).click();
  if(!await page.getByText('Waiting for the first calendar refresh.',{exact:true}).isVisible())throw Error('Subscribe');
  await ctx.close();results.push({noJavaScript:true,keyboardDisclosure:true,details:true,visibility:true,isolation:true,subscribe:true,pause:true,refresh:true,unsubscribe:true});
  const dark=await browser.newContext({viewport:{width:390,height:844},colorScheme:'dark'});const dp=await dark.newPage();await dp.goto(base+'/calendar?view=month&order=calendar');
  await dp.screenshot({path:dir+'/phone-dark.png'});await dark.close();results.push({dark:true});
  fs.writeFileSync(dir+'/browser.json',JSON.stringify({checks:results.length,results},null,2));
  console.log(JSON.stringify({checks:results.length,passed:true}));
 } finally {await browser.close();fs.writeFileSync(dir+'/stop','done');}
})().catch(e=>{console.error(e);process.exit(1)});
