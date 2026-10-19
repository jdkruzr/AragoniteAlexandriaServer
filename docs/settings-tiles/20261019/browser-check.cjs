const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs=require('fs'), assert=require('assert');
const dir=process.env.ALEXANDRIA_SETTINGS_BROWSER;
if (!dir) throw new Error('Set ALEXANDRIA_SETTINGS_BROWSER to the running fixture directory');
const base=fs.readFileSync(dir+'/url','utf8').trim();
const checks=[];
const routes={ '/settings':['password','language','about'], '/settings/search':['semantic-search','embedding-model'], '/settings/recognition':['page-limit','provider','minutes','embedding','automatic','note-actions','reprocess'], '/settings/integrations':['caldav','api'], '/sources/client':['devices','recognition-search'], '/boox/settings':['connection','maintenance','diagnostics'] };
const viewports={desktop:{width:1440,height:1000},phone:{width:390,height:844},Palma:{width:412,height:824},Lumi:{width:1100,height:1467}};
const options={httpCredentials:{username:'author',password:'a-long-test-password'}};
(async()=>{
 const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});
 try {
 for (const [size,viewport] of Object.entries(viewports)) for(const lang of ['en','es']) {
  const context=await browser.newContext({...options,viewport,locale:lang,javaScriptEnabled:false});
  const page=await context.newPage();
  for(const [route,ids] of Object.entries(routes)) {
   const res=await page.goto(base+route);assert.equal(res.status(),200,route);
   assert.equal(await page.locator('html').getAttribute('lang'),lang);
   assert.equal(await page.locator('details.settings-tile').count(),ids.length,route);
   assert.equal(await page.locator('details.settings-tile[open]').count(),0);
   assert(await page.locator('.settings-description').allTextContents().then(xs=>xs.every(x=>x.length>15)));
   for(const id of ids) {
    const tile=page.locator('#'+id+'.settings-tile');
    await tile.locator(':scope > summary').click();
    assert(await tile.getAttribute('open')!==null,id+' did not open');
    assert(await tile.locator('.settings-body').isVisible());
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),route+' overflow expanded '+id+' '+size);
    await tile.locator(':scope > summary').click();
   }
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),route+' overflow '+size);
   checks.push(`${size} ${lang}: ${route}, all ${ids.length} categories collapsed then expanded without JS`);
  }
  await page.goto(base+'/settings/recognition');
  await page.screenshot({path:`${dir}/${size}-${lang}.png`,fullPage:true});
  await context.close();
 }
 // Follow every explicit category link through the server, not a script.
 const context=await browser.newContext({...options,viewport:viewports.phone,javaScriptEnabled:false});const page=await context.newPage();
 for(const [route,ids] of Object.entries(routes)) for(const id of ids) {
  await page.goto(base+route+'?section='+id+'#'+id);
  assert.equal(await page.locator('details.settings-tile[open]').count(),1);
  assert.notEqual(await page.locator('#'+id).getAttribute('open'),null);
 }
 checks.push('All 19 server-readable category links open only their target without JS');
 await page.goto(base+'/settings/recognition?section=api');assert.equal(await page.locator('details.settings-tile[open]').count(),0);checks.push('Invalid or cross-page section names are ignored');
 // Persistent settings posts, result notices, and stale input on disposable DB.
 await page.goto(base+'/settings/recognition?section=page-limit');
 await page.locator('input[name=limit]').fill('125');await page.locator('#page-limit button').click();
 assert.notEqual(await page.locator('#page-limit').getAttribute('open'),null);
 assert(await page.locator('#page-limit .notice[role=status]').isVisible());assert.equal(await page.locator('input[name=limit]').inputValue(),'125');
 checks.push('Page-limit save returns to expanded category with its visible result notice');
 // POST invalid values directly to bypass HTML constraints and exercise the handler.
 const resp=await context.request.post(base+'/settings/recognition-limit',{headers:{Origin:base},form:{limit:'0',expected:'125'},maxRedirects:0});assert.equal(resp.status(),303);
 await page.goto(base+resp.headers().location);assert(await page.locator('#page-limit .notice[role=status]').isVisible());
 checks.push('Server validation returns to expanded page-limit category');
 await page.goto(base+'/settings?section=language');await page.selectOption('select[name=language]','es');await page.locator('#language button').click();
 assert.equal(await page.locator('html').getAttribute('lang'),'es');assert(await page.locator('#language .notice[role=status]').isVisible());
 checks.push('Language save reopens its category and translates the result');
 await page.selectOption('select[name=language]','en');await page.locator('#language button').click();
 for(const [id,field,value] of [['provider','ocr_model','unsaved-vision'],['minutes','s_model','unsaved-summary'],['embedding','embed_model','unsaved-embedding']]) {
  await page.goto(base+'/settings/recognition?section='+id);
  // Invalid empty URL with enabled model generates a server validation response.
  if(id==='provider')await page.locator('#provider [name=ocr_enabled]').check();
  if(id==='minutes')await page.locator('[name=s_enabled]').check();
  await page.locator('#'+id+' [name='+field+']').fill(value);
  if(id==='embedding') { await page.locator('#embedding [name=embed_url]').fill('ftp://invalid.example'); }
  await page.locator('#'+id+' button[value=save]').click();
  assert.notEqual(await page.locator('#'+id).getAttribute('open'),null,id);
  assert(await page.locator('#'+id+' .notice[role=status]').isVisible(),id); assert.equal(await page.locator('nav[aria-label=Settings] [aria-current=page]').getAttribute('href'),'/settings/recognition');
  assert.equal(await page.locator('#'+id+' [name='+field+']').inputValue(),value);
 }
 checks.push('Provider, audio, and embedding validation preserve unsaved fields in the open category');
 await page.goto(base+'/settings/recognition?section=automatic');await page.locator('#automatic form button:not([type])').click();assert(await page.locator('#automatic .notice[role=status]').isVisible());
 await page.goto(base+'/settings/recognition?section=note-actions');await page.locator('#note-actions form button:not([type])').click();assert(await page.locator('#note-actions .notice[role=status]').isVisible());
 checks.push('Automatic processing and note-action saves reopen their own categories');
 await page.goto(base+'/settings/integrations?section=caldav');await page.locator('[name=collection]').fill('Synthetic tasks');await page.locator('#caldav form button:not([type])').click();assert(await page.locator('#caldav .notice[role=status]').isVisible());assert.equal(await page.locator('[name=collection]').inputValue(),'Synthetic tasks');
 await page.goto(base+'/settings/integrations?section=api');await page.locator('[name=label]').fill('Synthetic browser token');await page.locator('#api form[action$=create] button').click();await page.locator('main a.button').click();assert.notEqual(await page.locator('#api').getAttribute('open'),null);
 await page.locator('#api form[action$=revoke] button').click();assert(await page.locator('#api .notice[role=status]').isVisible());checks.push('CalDAV save, token creation/Done, and revocation return to the correct expanded categories');
 await page.goto(base+'/settings/recognition');assert.equal(await page.locator('details.settings-tile[open]').count(),0);checks.push('A fresh visit collapses every category again');
 // Native keyboard controls work with JS disabled, including visible focus.
 const summary=page.locator('#page-limit > summary');await summary.focus();assert.notEqual(await summary.evaluate(el=>getComputedStyle(el).outlineStyle),'none');await page.keyboard.press('Enter');assert.notEqual(await page.locator('#page-limit').getAttribute('open'),null);await page.keyboard.press('Space');assert.equal(await page.locator('#page-limit').getAttribute('open'),null);checks.push('Native Enter/Space toggling and visible keyboard focus without JS');
 await context.close();
 const js=await browser.newContext({...options,viewport:viewports.phone,colorScheme:'dark'});const jp=await js.newPage();
 await jp.goto(base+'/settings/recognition#embedding');assert.notEqual(await jp.locator('#embedding').getAttribute('open'),null);await jp.goto(base+'/settings/recognition#ocr-prompt');assert(await jp.locator('#ocr-prompt').isVisible());
 await jp.evaluate(()=>{location.hash='minutes'});await jp.waitForTimeout(100);assert.notEqual(await jp.locator('#minutes').getAttribute('open'),null);
 checks.push('Legacy fragments and nested-field bookmarks reveal categories; hash changes also work');
 await jp.goto(base+'/settings/recognition');await jp.locator('#page-limit > summary').click();await jp.locator('[name=limit]').fill('0');await jp.locator('#page-limit > summary').click();await jp.evaluate(()=>document.querySelector('#page-limit form').requestSubmit());assert.notEqual(await jp.locator('#page-limit').getAttribute('open'),null);assert(await jp.locator('[name=limit]').evaluate(el=>el===document.activeElement));checks.push('Invalid hidden fields are revealed and focused before browser validation');
 await jp.goto(base+'/settings/recognition');await jp.screenshot({path:dir+'/phone-dark.png',fullPage:true});checks.push('Dark mode rendered on phone with all categories collapsed');await js.close();
 fs.writeFileSync(dir+'/results.json',JSON.stringify({checks:checks.length,passed:checks},null,2)+'\n');console.log('PASS',checks.length,'checks');
 }finally{await browser.close();}
})().catch(e=>{console.error(e);process.exitCode=1});
