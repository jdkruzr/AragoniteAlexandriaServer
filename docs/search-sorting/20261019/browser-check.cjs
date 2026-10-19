const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs=require('fs'),assert=require('assert');
const dir=process.env.ALEXANDRIA_SEARCH_BROWSER || '/tmp/search-sorting-browser',base=fs.readFileSync(dir+'/url','utf8').trim(),checks=[];
const push=s=>{checks.push(s);fs.writeFileSync(dir+'/progress.json',JSON.stringify({checks:checks.length,passed:checks},null,2)+'\n')};
const sizes={desktop:{width:1440,height:1000},phone:{width:390,height:844},Palma:{width:412,height:824},Lumi:{width:1100,height:1467}};
const firstTitle=page=>page.locator('#titles-results article p a').first().textContent();
(async()=>{const browser=await chromium.launch({executablePath:'/usr/bin/google-chrome',headless:true,args:['--no-sandbox']});try{
 for(const [size,viewport] of Object.entries(sizes))for(const language of ['en','es']){
  const context=await browser.newContext({viewport,locale:language,javaScriptEnabled:false});const page=await context.newPage();
  for(const order of ['relevance','title_asc','title_desc','created_asc','created_desc']){
   const res=await page.goto(base+'/search?q=needle&mode=keyword&sort='+order);
   assert.equal(res.status(),200);assert.equal(await page.locator('select[name=sort]').inputValue(),order);assert.equal(await page.locator('html').getAttribute('lang'),language);
   for(const group of ['pages','titles','annotations'])assert.equal(await page.locator('#'+group+'-results article').count(),50);
   if(order!=='relevance')assert.equal(await firstTitle(page),['title_desc','created_desc'].includes(order)?'Needle 120':'Needle 1');
   if(order==='title_asc')assert.equal(await page.locator('#pages-results article p a').first().textContent(),language==='en'?'Client needle 1, page 1':'Client needle 1, página 1');
   assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'overflow '+size+' '+language+' '+order);
   push(`${size} ${language}: ${order}, three paginated groups, ordering and no horizontal overflow without JS`);
  }
  await page.goto(base+'/search?q=needle&mode=keyword&sort=title_asc&source=boox');await page.screenshot({path:dir+`/${size}-${language}.png`,fullPage:false});await context.close();
 }
 const context=await browser.newContext({viewport:sizes.phone,javaScriptEnabled:false});const page=await context.newPage();
 await page.goto(base+'/search?q=needle&mode=keyword&sort=title_asc&source=boox');assert.equal(await page.locator('#annotations-results').count(),0);
 for(const group of ['pages','titles']){
  await page.locator('#'+group+'-results nav a', {hasText:'Next'}).click();
  const q=new URL(page.url()).searchParams;assert.equal(q.get('sort'),'title_asc');assert.equal(q.get('mode'),'keyword');assert.equal(q.get('source'),'boox');assert.equal(q.get(group==='pages'?'pages_offset':'titles_offset'),'50');
 }
 assert.equal(await firstTitle(page),'Needle 51');
 assert((await page.locator('#pages-results article p a').first().textContent()).startsWith('Needle 51,'));
 await page.locator('#titles-results nav a',{hasText:'Next'}).click();assert.equal(await firstTitle(page),'Needle 101');assert.equal(await page.locator('#titles-results article').count(),20);
 await page.goBack();assert.equal(await firstTitle(page),'Needle 51');
 push('Independent pagination retains query, mode, source, sort, and other group positions; browser Back restores the previous page');
 await page.selectOption('select[name=sort]','title_desc');await page.locator('form.search button').click();assert.equal(await firstTitle(page),'Needle 120');assert.equal(new URL(page.url()).searchParams.has('pages_offset'),false);assert.equal(new URL(page.url()).searchParams.has('titles_offset'),false);
 await page.locator('[name=q]').fill('Needle 2');await page.locator('form.search button').click();assert.equal(await page.locator('select[name=sort]').inputValue(),'title_desc');assert.equal(await page.locator('select[name=source]').inputValue(),'boox');
 push('Changing sort resets all pages; submitting a new query retains the selected sort and source');
 await page.goto(base+'/search?q=needle&source=client&sort=created_asc&mode=keyword');assert.equal(await page.locator('#titles-results').count(),0);assert(await page.locator('#annotations-results').getByText('Creation date unavailable').first().isVisible());
 push('Client scope excludes BOOX titles and clearly identifies missing annotation creation dates');
 await page.goto(base+'/search?q=needle&source=boox&sort=title_desc&mode=keyword&pages_offset=999999&titles_offset=999999');assert.equal(await firstTitle(page),'Needle 120');assert.equal(await page.locator('#pages-results article').count(),50);
 push('Out-of-range bookmarks recover the first page');
 await page.goto(base+'/search?q=needle&sort=invalid&pages_offset=-1');assert.equal(await page.locator('select[name=sort]').inputValue(),'relevance');
 await page.selectOption('select[name=sort]','title_asc');await page.locator('form.search button').click();assert.equal(await page.locator('select[name=mode]').inputValue(),'');assert.equal(await firstTitle(page),'Needle 1');
 push('Invalid navigation falls back safely; hybrid mode keeps its selection when sorting');
 await page.goto(base+'/search?q=no-matching-synthetic-record&sort=created_desc&mode=keyword');assert.equal(await page.locator('article.result').count(),0);assert.equal(await page.locator('select[name=sort]').inputValue(),'created_desc');
 const select=page.locator('select[name=sort]');await select.focus();assert.notEqual(await select.evaluate(el=>getComputedStyle(el).outlineStyle),'none');await page.keyboard.press('ArrowUp');await page.keyboard.press('Enter');
 push('Empty results retain sorting; sort control has visible keyboard focus');
 await context.close();const dark=await browser.newContext({viewport:sizes.phone,colorScheme:'dark'});const dp=await dark.newPage();await dp.goto(base+'/search?q=needle&sort=created_desc&mode=keyword&source=boox');await dp.screenshot({path:dir+'/phone-dark.png'});push('Dark mode on phone');await dark.close();
 fs.writeFileSync(dir+'/results.json',JSON.stringify({checks:checks.length,passed:checks},null,2)+'\n');console.log('PASS',checks.length,'checks');
}finally{await browser.close()}})().catch(e=>{console.error(e);process.exitCode=1});
