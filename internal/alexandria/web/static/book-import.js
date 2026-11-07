(() => {
  'use strict';
  const root = document.getElementById('book-import');
  if (!root) return;
  const el = id => document.getElementById('import-' + id);
  const words = Object.fromEntries([...el('strings').children].map(n => [n.dataset.key, n.textContent]));
  const files = new Map(), choices = {}, failures = new Map(), transfers = new Map();
  let batch = new URL(location.href).searchParams.get('import') || '', requestId = crypto.randomUUID();
  let items = [], ready = false, running = false, timer, previewVersion = 0;
  const api = '/boox/books/imports';
  const message = text => { el('message').textContent = text; };
  const json = async (url, data) => {
    const response = await fetch(url, data === undefined ? {cache: 'no-store'} : {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(data)});
    let body; try { body = await response.json(); } catch (_) { throw new Error(words.failed); }
    if (!response.ok) throw new Error(body.error || words.failed);
    return body;
  };
  const size = n => n >= 1048576 ? (n / 1048576).toFixed(1) + ' MB' : Math.max(1, Math.round(n / 1024)) + ' KB';
  const tagClass = state => ({published: 'tag-accent', blocked: 'tag-accent-2', failed: 'tag-accent-2', skipped: 'tag-outline', duplicate: 'tag-outline'})[state] || 'tag-neutral';
  function render() {
    const tbody = el('table').tBodies[0]; tbody.replaceChildren(); el('selection').hidden = !items.length;
    const counts = {};
    for (const item of items) {
      if (item.kind === 'file') counts[item.state] = (counts[item.state] || 0) + 1;
      const row = tbody.insertRow(), name = row.insertCell(), bytes = row.insertCell(), status = row.insertCell(), action = row.insertCell();
      const parts = item.path.split('/'), kind = document.createElement('span');
      kind.className = 'tag tag-neutral'; kind.textContent = item.kind === 'folder' ? words.folder : (parts.at(-1).split('.').pop() || '').toUpperCase();
      name.className = 'upload-name'; name.style.paddingLeft = (parts.length - 1) * 20 + 'px'; name.append(kind, ' ', parts.at(-1));
      bytes.className = 'num'; bytes.textContent = item.kind === 'file' && item.size ? size(item.size) : '';
      const error = failures.get(item.path) || item.error, transfer = transfers.get(item.path);
      const state = transfer !== undefined ? 'uploading' : failures.has(item.path) ? 'failed' : item.duplicate && item.state === 'waiting' ? 'duplicate' : item.state;
      const tag = document.createElement('span'); tag.className = 'tag ' + tagClass(state);
      tag.textContent = transfer !== undefined ? words.uploading + ' ' + transfer + '%' : item.kind === 'folder' && item.state === 'waiting' ? words.folder : words[state] || item.state;
      if (item.kind !== 'folder' || item.state !== 'waiting') status.append(tag);
      if (transfer !== undefined) { const bar = document.createElement('div'), fill = document.createElement('span'); bar.className = 'bar'; bar.setAttribute('role', 'progressbar'); bar.setAttribute('aria-label', item.path); bar.setAttribute('aria-valuenow', transfer); fill.style.width = transfer + '%'; bar.append(fill); status.append(bar); }
      if (error || state === 'duplicate') { const note = document.createElement('p'); note.className = 'small'; note.textContent = error || words.dupnote; status.append(note); }
      if (item.choices?.length) {
        const select = document.createElement('select'); select.className = 'input'; select.setAttribute('aria-label', words.choose + ' ' + item.path);
        const placeholder = new Option(words.choose, ''); select.add(placeholder);
        for (const f of item.choices) select.add(new Option(f.Path + ' · ' + f.ID, f.ID));
        select.value = choices[item.path] || ''; select.addEventListener('change', () => { choices[item.path] = select.value; preview(); }); action.append(select);
      }
      if (batch && item.kind === 'file' && !['published', 'skipped'].includes(item.state)) {
        if (item.state === 'waiting' && !files.has(item.path)) action.textContent = words.reselect;
        else if (!running && (error || item.state === 'blocked')) { const button = document.createElement('button'); button.type = 'button'; button.className = 'btn btn-ghost'; button.textContent = words.retry; button.addEventListener('click', async () => { if(running) return; running=true; render(); try { if (item.operation) await json(api + '/retry', {id: batch, path: item.path}); else { failures.delete(item.path); await upload(item); } await refresh(); } catch (e) { message(e.message); } finally {running=false;render();} }); action.append(button); }
      }
    }
    el('summary').textContent = Object.entries(counts).map(([state,n]) => `${words[state] || state}: ${n}`).join(' · ');
    const books = items.filter(i => i.kind === 'file' && i.state !== 'skipped'), published = books.filter(i => i.state === 'published').length;
    el('progress').hidden = !batch || !books.length;
    el('progress').querySelector('.bar span').style.width = (books.length ? Math.round(100 * published / books.length) : 0) + '%';
    el('progress-line').textContent = words.ofpublished.replace('{done}', published).replace('{total}', books.length);
    el('done').hidden = !batch || !items.some(i => i.kind === 'file') || !items.filter(i => i.kind === 'file').every(i => ['published', 'skipped'].includes(i.state));
    el('start').disabled = running || (!batch && !ready) || (!!batch && !items.some(i => i.kind === 'file' && i.state === 'waiting' && files.has(i.path)));
    el('start').querySelector('span').textContent = batch ? words.continue : words.start;
    el('drop').hidden = !!batch;
    el('destination').disabled = !!batch || running; el('preview').disabled = !!batch || running;
    el('files').disabled = running; el('folder').disabled = running;
  }
  function manifest() { return {requestId, destination: el('destination').value, files: [...files].map(([path,f]) => ({path, size:f.size})), choices}; }
  async function preview() {
    if (batch) { render(); return; }
    const version = ++previewVersion; ready = false; render();
    if (!files.size) { message(words.select); return; }
    try { const result = await json(api + '/preview', manifest()); if (version !== previewVersion) return; items = result.items; ready = result.ready; render(); message(items.some(i => i.kind === 'file' && i.state !== 'skipped') ? words.preview : words.empty); } catch(e) { if (version === previewVersion) message(e.message); }
  }
  // Takes [path, File] pairs; a path's first folder becomes the top-level shelf.
  function select(incoming) {
    if (!batch && new Set([...files.keys(), ...incoming.map(([p]) => p)]).size > 1000) { message(words.limit); return; }
    for (const [p,f] of incoming) { if (!batch || items.some(i => i.path === p && i.size === f.size && i.state === 'waiting')) { files.set(p,f); failures.delete(p); } }
    if (batch) { render(); message(words.resume); } else { requestId = crypto.randomUUID(); preview(); }
  }
  function upload(item) {
    return new Promise((resolve,reject) => {
      const file = files.get(item.path); if (!file) { reject(new Error(words.reselect)); return; }
      const form = new FormData(); form.set('id',batch); form.set('path',item.path); form.set('book',file,file.name);
      const xhr = new XMLHttpRequest(); xhr.open('POST',api + '/file'); xhr.timeout = 10 * 60 * 1000;
      transfers.set(item.path,0); render();
      xhr.upload.onprogress = e => { if(e.lengthComputable) { transfers.set(item.path,Math.round(100*e.loaded/e.total)); render(); } };
      const fail = text => { transfers.delete(item.path); failures.set(item.path,text); render(); reject(new Error(text)); };
      xhr.onerror = xhr.ontimeout = () => fail(words.failed);
      xhr.onload = () => { let result; try { result = JSON.parse(xhr.responseText); } catch (_) { fail(words.failed); return; } if(xhr.status !== 200) { fail(result.error || words.failed); return; } transfers.delete(item.path); failures.delete(item.path); item.state='queued'; item.operation=result.operation; files.delete(item.path); render(); resolve(); };
      xhr.send(form);
    });
  }
  function schedule() { clearTimeout(timer); if(batch && items.some(i => i.kind === 'file' && i.state === 'queued')) timer=setTimeout(() => refresh().catch(e => {message(e.message); schedule();}),2500); }
  async function refresh() { const result=await json(api+'?id='+encodeURIComponent(batch)); items=result.items; render(); if(items.filter(i=>i.kind==='file').every(i=>['published','skipped'].includes(i.state))) message(words.done); schedule(); }
  const picked = list => [...list].map(f => [f.webkitRelativePath || f.name, f]);
  el('files').addEventListener('change',e=>{ select(picked(e.target.files)); e.target.value=''; });
  el('folder').addEventListener('change',e=>{ select(picked(e.target.files)); e.target.value=''; });
  // Dropped folders are walked so their paths match a "Choose folder" selection.
  const walk = async (entry, found) => {
    if (entry.isFile) { found.push([entry.fullPath.replace(/^\//, ''), await new Promise((ok, fail) => entry.file(ok, fail))]); return; }
    const reader = entry.createReader();
    for (;;) { const batch = await new Promise((ok, fail) => reader.readEntries(ok, fail)); if (!batch.length) return; for (const child of batch) await walk(child, found); }
  };
  root.addEventListener('dragover', e => { if (running || batch) return; e.preventDefault(); el('drop').classList.add('import-drop'); });
  root.addEventListener('dragleave', e => { if (!root.contains(e.relatedTarget)) el('drop').classList.remove('import-drop'); });
  root.addEventListener('drop', async e => {
    el('drop').classList.remove('import-drop');
    if (running || batch) return;
    e.preventDefault();
    const entries = [...e.dataTransfer.items].map(i => i.webkitGetAsEntry && i.webkitGetAsEntry()).filter(Boolean);
    if (!entries.length) { select(picked(e.dataTransfer.files)); return; }
    const found = [];
    try { for (const entry of entries) await walk(entry, found); } catch (_) { message(words.failed); return; }
    select(found);
  });
  el('destination').addEventListener('change',()=>{ for(const k of Object.keys(choices)) delete choices[k]; requestId=crypto.randomUUID(); preview(); });
  el('preview').addEventListener('click',preview);
  el('start').addEventListener('click',async()=>{
    if(running) return; running=true; render();
    try {
      if(!batch) { const result=await json(api,manifest()); items=result.items; if(!result.id) {ready=result.ready;message(words.choose);return;} batch=result.id; history.replaceState(null,'','/boox/books/upload?import='+encodeURIComponent(batch)); try{localStorage.setItem('alexandria_last_book_import',batch);}catch(_){} }
      message(words.running);
      const queue=items.filter(i=>i.kind==='file'&&i.state==='waiting'&&files.has(i.path));
      const worker=async()=>{let item;while((item=queue.shift())){try{await upload(item);}catch(_){/* Keep the rest moving. */}}};
      await Promise.all([worker(),worker()]); await refresh();
    }catch(e){message(e.message);}finally{running=false;render();}
  });
  root.hidden=false; el('fallback').open=false;
  if(batch) refresh().then(()=>message(words.resume)).catch(e=>message(e.message));
  else {message(words.select);try{const last=localStorage.getItem('alexandria_last_book_import');if(last){el('resume').href='/boox/books/upload?import='+encodeURIComponent(last);el('resume').hidden=false;}}catch(_){} }
})();
