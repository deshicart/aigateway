/* OpenBridge dashboard — vanilla JS, tiny bundle, mobile-first. */
const $ = (s, el=document) => el.querySelector(s);
const state = { page: 'overview', token: localStorage.getItem('obg_session') || '', status: null, providers: [], models: [], keys: [] };

function toast(msg){ const t=document.createElement('div'); t.textContent=msg; $('#toast').appendChild(t); setTimeout(()=>t.remove(),2600); }
async function api(path, opts={}){
  opts.headers = Object.assign({'Content-Type':'application/json'}, opts.headers||{});
  if(state.token) opts.headers['Authorization'] = 'Bearer '+state.token;
  const r = await fetch(path, opts);
  if(r.status===401 && !path.includes('/login')){ showLogin(); throw new Error('auth'); }
  const ct = r.headers.get('content-type')||'';
  const body = ct.includes('json') ? await r.json() : await r.text();
  if(!r.ok) throw new Error((body&&body.error&&body.error.message)||('HTTP '+r.status));
  return body;
}
function showLogin(){ $('#login').classList.remove('hidden'); }
function hideLogin(){ $('#login').classList.add('hidden'); }

$('#loginForm').addEventListener('submit', async e=>{
  e.preventDefault();
  try{
    const b = await api('/api/admin/login',{method:'POST',body:JSON.stringify({username:$('#loginUser').value,password:$('#loginPass').value})});
    state.token=b.token; localStorage.setItem('obg_session',b.token); hideLogin(); toast('Signed in'); render();
  }catch(err){ $('#loginErr').textContent = err.message; }
});
document.querySelectorAll('nav button').forEach(b=>b.addEventListener('click',()=>{
  document.querySelectorAll('nav button').forEach(x=>x.classList.remove('active'));
  b.classList.add('active'); state.page=b.dataset.page; $('#sidebar').classList.remove('open'); render();
}));
$('#menuBtn').addEventListener('click',()=>$('#sidebar').classList.toggle('open'));
$('#themeBtn').addEventListener('click',()=>{
  const h=document.documentElement; h.dataset.theme = h.dataset.theme==='dark'?'light':'dark';
  try{localStorage.setItem('obg_theme',h.dataset.theme);}catch(e){}
});
try{const t=localStorage.getItem('obg_theme'); if(t) document.documentElement.dataset.theme=t;}catch(e){}

async function render(){
  const p=$('#page');
  try{
    if(state.page==='overview') return p.innerHTML=await vOverview();
    if(state.page==='providers') return p.innerHTML=await vProviders(), bindProviders();
    if(state.page==='models') return p.innerHTML=await vModels(), bindModels();
    if(state.page==='routing') return p.innerHTML=await vRouting(), bindRouting();
    if(state.page==='keys') return p.innerHTML=await vKeys(), bindKeys();
    if(state.page==='playground') return p.innerHTML=vPlayground(), bindPlayground();
    if(state.page==='analytics') return p.innerHTML=await vAnalytics();
    if(state.page==='settings') return p.innerHTML=await vSettings(), bindSettings();
  }catch(err){ if(err.message!=='auth') p.innerHTML=`<div class="card"><h3>Error</h3><p>${esc(err.message)}</p></div>`; }
}
function esc(s){ return String(s??'').replace(/[&<>"]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c])); }
function pill(h){
  const m={healthy:'ok',unknown:'',disabled:'',rate_limited:'warn',invalid:'bad',timeout:'warn',error:'bad'};
  return `<span class="pill ${m[h]||''}">${esc(h||'unknown')}</span>`;
}

async function vOverview(){
  let s; try{ s=await api('/api/admin/status'); }catch(e){ return '<p>Sign in required.</p>'; }
  state.status=s; $('#ver').textContent='v'+(s.version||'');
  const prov = s.providers||{};
  return `<h2>Overview</h2>
  <div class="grid">
    <div class="card"><h3>Gateway</h3><div class="big">● Running</div><div class="muted small">v${esc(s.version||'')}</div></div>
    <div class="card"><h3>Endpoint</h3><div class="mono small">${esc(s.endpoint||'')}</div><div class="row" style="margin-top:8px"><button class="btn ghost" onclick="navigator.clipboard.writeText('${esc(s.endpoint||'')}');toast('Copied')">Copy</button></div></div>
    <div class="card"><h3>API Key</h3><div class="mono">${esc(s.api_key_masked||'—')}</div><div class="muted small">Manage in API Keys</div></div>
    <div class="card"><h3>Providers</h3><div class="big">${prov.connected??0} Connected</div><div class="muted small">${prov.unhealthy??0} unhealthy · ${prov.keys??0} keys</div></div>
    <div class="card"><h3>Requests</h3><div class="big">${s.requests??0}</div><div class="muted small">success ${(s.success_rate??0).toFixed(1)}%</div></div>
    <div class="card"><h3>Latency</h3><div class="big">${((s.avg_latency_ms??0)/1000).toFixed(2)}s</div><div class="muted small">avg · fallbacks ${s.fallbacks??0}</div></div>
  </div>
  <div class="card"><h3>First-run wizard</h3>
    <p class="muted">Add your first provider to get started. Beginner flow: start → dashboard → add key → Test → copy unified key.</p>
    <div class="row"><button class="btn" data-goto="providers">Connect provider</button><button class="btn ghost" data-goto="playground">Open playground</button></div>
  </div>
  <script>document.querySelectorAll('[data-goto]').forEach(b=>b.onclick=()=>{document.querySelector(`nav button[data-page="${b.dataset.goto}"]`).click()})<\/script>`;
}

const PTYPES=[['google','Google Gemini'],['groq','Groq'],['openrouter','OpenRouter'],['cerebras','Cerebras'],['mistral','Mistral'],['nvidia','NVIDIA'],['github','GitHub Models'],['cloudflare','Cloudflare Workers AI'],['huggingface','Hugging Face'],['ollama','Ollama (local)'],['custom','Custom OpenAI-compatible']];
async function vProviders(){
  const b=await api('/api/providers'); state.providers=b.providers||[];
  const cards=state.providers.map(p=>`<div class="card"><div class="row" style="justify-content:space-between"><strong>${esc(p.display_name||p.type)}</strong>${pill(p.health)}</div>
    <div class="muted small">${esc(p.type)} · ${p.keys} keys · ${p.enabled?'enabled':'disabled'}</div>
    <div class="row" style="margin-top:8px"><button class="btn ghost" data-edit="${p.id}">Edit</button><button class="btn ghost" data-test="${p.id}">Test</button><button class="btn danger" data-del="${p.id}">Delete</button></div></div>`).join('')||'<p class="muted">No providers yet.</p>';
  return `<h2>Providers</h2><div class="card"><h3>Add provider</h3>
    <div class="row"><select id="npType">${PTYPES.map(([v,l])=>`<option value="${v}">${l}</option>`).join('')}</select></div>
    <label>Display name<input id="npName" placeholder="e.g. My Google key"></label>
    <label>Base URL (optional for presets; required for custom)<input id="npBase" placeholder="https://... or http://localhost:11434/v1"></label>
    <label>API key(s) — one per line for rotation<input id="npKey" type="password" placeholder="sk-..."></label>
    <label>Model IDs (custom only, comma separated)<input id="npModels" placeholder="llama3.3, qwen2.5"></label>
    <div class="row"><button class="btn" id="npAdd">Add provider</button><button class="btn ghost" id="npTest">Test connection</button></div>
    <p class="muted small" id="npMsg"></p></div>
    <h3>Configured</h3><div class="grid">${cards}</div>`;
}
function bindProviders(){
  const g=id=>$(id);
  if(!g('#npAdd')) return;
  g('#npAdd').onclick=async()=>{
    const payload={type:g('#npType').value,display_name:g('#npName').value,base_url:g('#npBase').value,api_key:g('#npKey').value,
      model_ids:g('#npModels').value.split(',').map(s=>s.trim()).filter(Boolean)};
    try{ await api('/api/providers',{method:'POST',body:JSON.stringify(payload)}); toast('Provider added'); render(); }
    catch(e){ g('#npMsg').textContent=e.message; }
  };
  g('#npTest').onclick=async()=>{
    try{ const r=await api('/api/providers/test',{method:'POST',body:JSON.stringify({type:g('#npType').value,base_url:g('#npBase').value,api_key:g('#npKey').value})});
      g('#npMsg').textContent=(r.ok?'✓ Success: ':'✕ '+(r.state+': '))+(r.message||''); }
    catch(e){ g('#npMsg').textContent=e.message; }
  };
  document.querySelectorAll('[data-del]').forEach(b=>b.onclick=async()=>{ if(!confirm('Delete provider?'))return; await api('/api/providers/'+b.dataset.del,{method:'DELETE'}); toast('Deleted'); render(); });
  document.querySelectorAll('[data-test]').forEach(b=>b.onclick=async()=>{
    const p=state.providers.find(x=>x.id===b.dataset.test);
    try{ const r=await api('/api/providers/test',{method:'POST',body:JSON.stringify({type:p.type,base_url:p.base_url,api_key:''})}); toast('Health: '+r.state); }
    catch(e){ toast(e.message); }
  });
  document.querySelectorAll('[data-edit]').forEach(b=>b.onclick=async()=>{
    const en=confirm('OK = enable, Cancel = disable'); await api('/api/providers/'+b.dataset.edit,{method:'PUT',body:JSON.stringify({enabled:en})}); render();
  });
}

async function vModels(){
  const b=await api('/api/models'); state.models=b.models||[];
  const rows=state.models.map(m=>`<tr><td><strong>${esc(m.display_name||m.id)}</strong><br><span class="muted small mono">${esc(m.id)}</span></td>
    <td>${esc(m.provider)}</td><td class="small">${[m.supports_vision&&'Vision',m.supports_tools&&'Tools',m.supports_streaming&&'Streaming',m.supports_embeddings&&'Embed'].filter(Boolean).join(' · ')}</td>
    <td>${m.enabled?'● Enabled':'○ Disabled'}</td>
    <td><button class="btn ghost" data-tgl="${esc(m.id)}">${m.enabled?'Disable':'Enable'}</button></td></tr>`).join('');
  return `<h2>Models</h2><div class="card"><input id="mq" placeholder="Search models..." aria-label="Search models"></div>
  <div class="card" style="margin-top:12px;overflow:auto"><table><thead><tr><th>Model</th><th>Provider</th><th>Caps</th><th>Status</th><th></th></tr></thead><tbody id="mrows">${rows}</tbody></table></div>`;
}
function bindModels(){
  const q=$('#mq'); if(!q) return;
  q.oninput=()=>{ const s=q.value.toLowerCase(); document.querySelectorAll('#mrows tr').forEach(tr=>{ tr.style.display=tr.textContent.toLowerCase().includes(s)?'':'none'; }); };
  document.querySelectorAll('[data-tgl]').forEach(b=>b.onclick=async()=>{
    const m=state.models.find(x=>x.id===b.dataset.tgl);
    await api('/api/models/'+encodeURIComponent(b.dataset.tgl),{method:'PUT',body:JSON.stringify({enabled:!m.enabled})});
    render();
  });
}

async function vRouting(){
  const r=await api('/api/routing');
  return `<h2>Routing</h2><div class="card">
    <label>Strategy<select id="rtStr">${['auto','priority','fastest','cheapest','balanced'].map(s=>`<option ${s===r.strategy?'selected':''}>${s}</option>`).join('')}</select></label>
    <label>Max attempts (1–20)<input id="rtMax" type="number" min="1" max="20" value="${r.max_attempts}"></label>
    <label class="row" style="flex-direction:row"><input type="checkbox" id="rtSticky" ${r.sticky_sessions?'checked':''} style="width:auto"> Sticky sessions (30 min default)</label>
    <div class="row"><button class="btn" id="rtSave">Save</button></div></div>
    <div class="card" style="margin-top:12px"><h3>Virtual models</h3><p class="muted small"><span class="mono">auto</span>, <span class="mono">auto:fast</span>, <span class="mono">auto:balanced</span>, <span class="mono">auto:priority</span> — plus <span class="mono">provider/model</span> or bare IDs.</p></div>`;
}
function bindRouting(){
  if(!$('#rtSave')) return;
  $('#rtSave').onclick=async()=>{
    await api('/api/routing',{method:'PUT',body:JSON.stringify({strategy:$('#rtStr').value,max_attempts:+$('#rtMax').value,sticky_sessions:$('#rtSticky').checked})});
    toast('Routing saved');
  };
}

async function vKeys(){
  const b=await api('/api/keys'); state.keys=b.keys||[];
  const rows=state.keys.map(k=>`<div class="card"><strong>${esc(k.name)}</strong><div class="mono small">${esc(k.prefix)}••••••••</div>
    <div class="muted small">Created ${esc(k.created_at||'')} · Last used ${esc(k.last_used_at||'never')}</div>
    <div class="row" style="margin-top:8px"><button class="btn danger" data-kdel="${k.id}">Revoke</button></div></div>`).join('')||'<p class="muted">No keys.</p>';
  return `<h2>API Keys</h2><div class="card"><h3>New key</h3><div class="row"><input id="nkName" placeholder="My Application" style="flex:1"><button class="btn" id="nkAdd">Create</button></div>
    <p class="mono small" id="nkOut"></p><p class="muted small">Clients use <span class="mono">Authorization: Bearer obg_...</span> against <span class="mono">/v1</span>.</p></div>
    <div class="grid" style="margin-top:12px">${rows}</div>`;
}
function bindKeys(){
  if(!$('#nkAdd')) return;
  $('#nkAdd').onclick=async()=>{
    const b=await api('/api/keys',{method:'POST',body:JSON.stringify({name:$('#nkName').value})});
    $('#nkOut').textContent=b.key; try{await navigator.clipboard.writeText(b.key);}catch(e){} toast('Key created & copied');
    render();
  };
  document.querySelectorAll('[data-kdel]').forEach(x=>x.onclick=async()=>{ if(!confirm('Revoke key?'))return; await api('/api/keys/'+x.dataset.kdel,{method:'DELETE'}); render(); });
}

function vPlayground(){
  return `<h2>Playground</h2><div class="card">
    <div class="row"><select id="pgModel"><option>auto</option><option>auto:fast</option><option>auto:balanced</option>${(state.models||[]).map(m=>`<option>${esc(m.id)}</option>`).join('')}</select>
    <input id="pgTemp" type="number" step="0.1" min="0" max="2" value="0.7" style="max-width:100px" aria-label="Temperature"></div>
    <label>System prompt<textarea id="pgSys" rows="2"></textarea></label>
    <label>User<textarea id="pgUser" rows="4"></textarea></label>
    <div class="row"><button class="btn" id="pgSend">Send</button></div>
    <div id="pgOut" style="margin-top:12px"></div></div>`;
}
async function bindPlayground(){
  if(!$('#pgSend')) return;
  if(!state.models.length){ try{ state.models=(await api('/api/models')).models||[]; }catch(e){} }
  $('#pgSend').onclick=async()=>{
    $('#pgOut').innerHTML='<p class="muted">…</p>';
    try{
      const r=await api('/api/playground',{method:'POST',body:JSON.stringify({model:$('#pgModel').value,system:$('#pgSys').value,user:$('#pgUser').value,temperature:+$('#pgTemp').value})});
      $('#pgOut').innerHTML=`<pre>${esc(r.response)}</pre><div class="kv"><span>Provider</span><b>${esc(r.provider)}</b></div><div class="kv"><span>Model</span><b class="mono">${esc(r.model)}</b></div><div class="kv"><span>Latency</span><b>${esc(r.latency)}</b></div><div class="kv"><span>Tokens</span><b>${r.tokens.total} (in ${r.tokens.prompt} / out ${r.tokens.completion})</b></div><div class="kv"><span>Fallbacks</span><b>${r.fallbacks}</b></div>`;
    }catch(e){ $('#pgOut').innerHTML=`<p class="err">✕ Failed: ${esc(e.message)}</p>`; }
  };
}

async function vAnalytics(){
  const s=await api('/api/analytics?hours=168');
  const prov=Object.entries(s.by_provider||{}).map(([k,v])=>`<div class="kv"><span>${esc(k||'(none)')}</span><b>${v}</b></div>`).join('')||'<p class="muted">No data.</p>';
  const mods=Object.entries(s.by_model||{}).map(([k,v])=>`<div class="kv"><span class="mono">${esc(k)}</span><b>${v}</b></div>`).join('');
  return `<h2>Analytics</h2><div class="grid">
    <div class="card"><h3>Total</h3><div class="big">${s.total}</div></div>
    <div class="card"><h3>Success rate</h3><div class="big">${(s.success_rate||0).toFixed(1)}%</div></div>
    <div class="card"><h3>Avg latency</h3><div class="big">${((s.avg_latency_ms||0)/1000).toFixed(2)}s</div></div>
    <div class="card"><h3>Tokens</h3><div class="big">${s.input_tokens+s.output_tokens}</div><div class="muted small">in ${s.input_tokens} · out ${s.output_tokens}</div></div></div>
    <div class="card"><h3>By provider</h3>${prov}</div><div class="card" style="margin-top:12px"><h3>By model</h3>${mods}</div>`;
}

async function vSettings(){
  const b=await api('/api/settings'); const s=b.settings||{};
  return `<h2>Settings</h2><div class="card">
    <div class="kv"><span>Host</span><b class="mono">${esc(s.host||'')}</b></div>
    <div class="kv"><span>Port</span><b>${esc(s.port||'')}</b></div>
    <div class="kv"><span>Low-resource mode</span><b>${esc(s.low_resource||'false')}</b></div>
    <p class="muted small">Server options are set via environment (<span class="mono">OPENBRIDGE_HOST/PORT/DATA_DIR/MASTER_KEY/LOG_LEVEL/LOW_RESOURCE</span>). LAN mode (0.0.0.0) shows a warning: others on your network could use your quota.</p>
    <div class="row"><button class="btn ghost" id="expBtn">Export config</button><button class="btn ghost" id="logoutBtn">Sign out</button></div></div>`;
}
function bindSettings(){
  if(!$('#logoutBtn')) return;
  $('#logoutBtn').onclick=()=>{ localStorage.removeItem('obg_session'); state.token=''; showLogin(); };
  $('#expBtn').onclick=()=>{ window.open('/api/export','_blank'); };
}
window.toast=toast;
render();
