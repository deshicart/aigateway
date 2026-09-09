/* OpenBridge dashboard — vanilla JS, tiny bundle, mobile-first. */
(function(){
'use strict';
var $ = function(s, el){ return (el||document).querySelector(s); };
var $$ = function(s, el){ return Array.prototype.slice.call((el||document).querySelectorAll(s)); };

function storeGet(k){
  try{ return window.localStorage.getItem(k); }catch(e){ return null; }
}
function storeSet(k, v){
  try{ window.localStorage.setItem(k, v); }catch(e){}
}
function storeDel(k){
  try{ window.localStorage.removeItem(k); }catch(e){}
}

var state = {
  page: 'overview',
  token: storeGet('obg_session') || '',
  status: null,
  providers: [],
  models: [],
  keys: [],
  lastError: ''
};

/* Route table: clean paths, served via Go SPA fallback. */
var PAGE_TO_PATH = {
  overview: '/',
  providers: '/providers',
  models: '/models',
  api: '/api',
  routing: '/routing',
  keys: '/keys',
  playground: '/playground',
  analytics: '/analytics',
  settings: '/settings'
};
var PATH_TO_PAGE = {
  '/': 'overview',
  '/overview': 'overview',
  '/providers': 'providers',
  '/models': 'models',
  '/api': 'api',
  '/routing': 'routing',
  '/keys': 'keys',
  '/playground': 'playground',
  '/analytics': 'analytics',
  '/settings': 'settings'
};
function pageFromPath(){
  var p = '/';
  try{ p = window.location.pathname || '/'; }catch(e){}
  if(p.length > 1) p = p.replace(/\/+$/, '');
  if(!p) p = '/';
  return PATH_TO_PAGE[p] || 'overview';
}
function pathFor(page){ return PAGE_TO_PATH[page] || '/'; }

function toast(msg){
  var box = $('#toast');
  if(!box) return;
  var t = document.createElement('div');
  t.textContent = msg;
  box.appendChild(t);
  setTimeout(function(){ if(t.parentNode) t.parentNode.removeChild(t); }, 2600);
}

function api(path, opts){
  opts = opts || {};
  opts.headers = { 'Content-Type': 'application/json' };
  if(state.token) opts.headers['Authorization'] = 'Bearer ' + state.token;
  return window.fetch(path, opts).then(function(r){
    if(r.status === 401 && path.indexOf('/login') < 0){
      showLogin();
      var e = new Error('auth');
      e.code = 'auth';
      throw e;
    }
    var ct = r.headers.get('content-type') || '';
    if(ct.indexOf('json') >= 0){
      return r.json().then(function(body){
        if(!r.ok) throw new Error((body && body.error && body.error.message) || ('HTTP ' + r.status));
        return body;
      });
    }
    return r.text().then(function(body){
      if(!r.ok) throw new Error('HTTP ' + r.status);
      return body;
    });
  }).catch(function(err){
    if(err && err.code === 'auth') throw err;
    if(err instanceof TypeError){
      var ne = new Error('Network error: cannot reach gateway at ' + window.location.origin);
      ne.code = 'network';
      throw ne;
    }
    throw err;
  });
}

function showLogin(){ var l = $('#login'); if(l) l.classList.remove('hidden'); }
function hideLogin(){ var l = $('#login'); if(l) l.classList.add('hidden'); }

function showFatal(msg){
  state.lastError = msg || 'unknown error';
  try{ window.__obg_err = state.lastError; }catch(e){}
  var m = $('#fatalMsg');
  if(m) m.textContent = 'The dashboard hit an error: ' + state.lastError;
  var f = $('#fatal');
  if(f) f.classList.remove('hidden');
}
function openDiag(){
  try{
    $('#dgUrl').textContent = window.location.href;
    $('#dgBoot').textContent = window.__obg_boot ? 'yes' : 'no';
    $('#dgErr').textContent = state.lastError || window.__obg_err || '(none)';
    $('#dgUa').textContent = window.navigator.userAgent;
    $('#dgHealth').textContent = 'checking…';
    window.fetch('/health').then(function(r){ return r.text(); }).then(function(t){
      $('#dgHealth').textContent = 'OK ' + t.slice(0, 80);
    }).catch(function(e){ $('#dgHealth').textContent = 'FAIL ' + e.message; });
  }catch(e){}
  $('#diag').classList.remove('hidden');
}

window.addEventListener('error', function(e){
  if(window.__obg_boot && e && e.message){
    state.lastError = e.message;
  }
});
window.addEventListener('unhandledrejection', function(e){
  if(window.__obg_boot && e && e.reason && e.reason.message && e.reason.code !== 'auth'){
    state.lastError = e.reason.message;
  }
});

function copyText(text, okMsg){
  function done(){ toast(okMsg || 'Copied'); }
  if(navigator.clipboard && navigator.clipboard.writeText){
    navigator.clipboard.writeText(text).then(done, function(){ fallback(); });
  } else { fallback(); }
  function fallback(){
    try{
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.style.position = 'fixed';
      ta.style.opacity = '0';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
      done();
    }catch(e){ toast('Copy failed — long-press to copy'); }
  }
}

function esc(s){
  return String(s == null ? '' : s).replace(/[&<>"]/g, function(c){
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c];
  });
}
function pill(h){
  var m = { healthy: 'ok', unknown: '', disabled: '', rate_limited: 'warn', invalid: 'bad', timeout: 'warn', error: 'bad' };
  return '<span class="pill ' + (m[h] || '') + '">' + esc(h || 'unknown') + '</span>';
}
function signinCard(){
  return '<div class="card"><h3>Sign in required</h3>' +
    '<p class="muted">Your admin session expired or is missing. Sign in to manage the gateway.</p>' +
    '<div class="row"><button class="btn" data-act="login">Sign in</button></div></div>';
}

/* ---------- navigation ---------- */

function setActiveNav(){
  $$('nav button').forEach(function(x){
    x.classList.toggle('active', x.getAttribute('data-page') === state.page);
  });
}
function closeSidebar(){
  var sb = $('#sidebar');
  if(sb) sb.classList.remove('open');
  var bd = $('#backdrop');
  if(bd) bd.classList.add('hidden');
  var mb = $('#menuBtn');
  if(mb) mb.setAttribute('aria-expanded', 'false');
}
function openSidebar(){
  var sb = $('#sidebar');
  if(sb) sb.classList.add('open');
  var bd = $('#backdrop');
  if(bd) bd.classList.remove('hidden');
  var mb = $('#menuBtn');
  if(mb) mb.setAttribute('aria-expanded', 'true');
}
function isSidebarOpen(){
  var sb = $('#sidebar');
  return !!(sb && sb.classList.contains('open'));
}

function navigate(page, push){
  if(!PAGE_TO_PATH[page]) page = 'overview';
  state.page = page;
  if(push !== false){
    try{ window.history.pushState({ page: page }, '', pathFor(page)); }catch(e){}
  }
  setActiveNav();
  closeSidebar();
  render();
  try{
    var pg = $('#page');
    if(pg) pg.focus({ preventScroll: true });
  }catch(e){}
}

function bindChrome(){
  $$('nav button').forEach(function(b){
    b.addEventListener('click', function(){ navigate(b.getAttribute('data-page')); });
  });
  var mb = $('#menuBtn');
  if(mb) mb.addEventListener('click', function(){
    if(isSidebarOpen()) closeSidebar(); else openSidebar();
  });
  var bd = $('#backdrop');
  if(bd) bd.addEventListener('click', closeSidebar);
  document.addEventListener('keydown', function(e){
    if(e.key === 'Escape'){
      closeSidebar();
      var d = $('#diag');
      if(d && !d.classList.contains('hidden')) d.classList.add('hidden');
    }
  });
  window.addEventListener('popstate', function(){
    state.page = pageFromPath();
    setActiveNav();
    closeSidebar();
    render();
  });
  /* delegated actions inside rendered pages (replaces inline <script>) */
  var pg = $('#page');
  if(pg) pg.addEventListener('click', function(e){
    var t = e.target;
    while(t && t !== pg && !(t.getAttribute && t.getAttribute('data-goto'))) t = t.parentNode;
    if(t && t !== pg){
      var dest = t.getAttribute('data-goto');
      if(dest) navigate(dest);
      return;
    }
    var l = e.target;
    while(l && l !== pg && !(l.getAttribute && l.getAttribute('data-act') === 'login')) l = l.parentNode;
    if(l && l !== pg){ showLogin(); }
  });

  var lf = $('#loginForm');
  if(lf) lf.addEventListener('submit', function(e){
    e.preventDefault();
    var le = $('#loginErr');
    if(le) le.textContent = '';
    api('/api/admin/login', { method: 'POST', body: JSON.stringify({
      username: $('#loginUser').value, password: $('#loginPass').value
    })}).then(function(b){
      state.token = b.token;
      storeSet('obg_session', b.token);
      hideLogin();
      toast('Signed in');
      render();
    }).catch(function(err){ if(le) le.textContent = err.message; });
  });
  var tb = $('#themeBtn');
  if(tb) tb.addEventListener('click', function(){
    var h = document.documentElement;
    h.dataset.theme = h.dataset.theme === 'dark' ? 'light' : 'dark';
    storeSet('obg_theme', h.dataset.theme);
  });
  try{
    var t = storeGet('obg_theme');
    if(t) document.documentElement.dataset.theme = t;
  }catch(e){}
  var fr = $('#fatalReload');
  if(fr) fr.addEventListener('click', function(){ window.location.reload(); });
  var fd = $('#fatalDiag');
  if(fd) fd.addEventListener('click', function(){
    $('#fatal').classList.add('hidden');
    openDiag();
  });
}

/* ---------- views ---------- */

function render(){
  var p = $('#page');
  if(!p) return Promise.resolve();
  p.dataset.rendered = '1';
  var done;
  try{
    if(state.page === 'overview') done = vOverview().then(function(h){ p.innerHTML = h; bindOverview(); });
    else if(state.page === 'providers') done = vProviders().then(function(h){ p.innerHTML = h; bindProviders(); });
    else if(state.page === 'models') done = vModels().then(function(h){ p.innerHTML = h; bindModels(); });
    else if(state.page === 'api') done = Promise.resolve(vApi()).then(function(h){ p.innerHTML = h; bindApi(); });
    else if(state.page === 'routing') done = vRouting().then(function(h){ p.innerHTML = h; bindRouting(); });
    else if(state.page === 'keys') done = vKeys().then(function(h){ p.innerHTML = h; bindKeys(); });
    else if(state.page === 'playground') done = Promise.resolve(vPlayground()).then(function(h){ p.innerHTML = h; bindPlayground(); });
    else if(state.page === 'analytics') done = vAnalytics().then(function(h){ p.innerHTML = h; });
    else if(state.page === 'settings') done = vSettings().then(function(h){ p.innerHTML = h; bindSettings(); });
    else done = vOverview().then(function(h){ p.innerHTML = h; bindOverview(); });
  }catch(err){
    p.innerHTML = '<div class="card"><h3>Something went wrong.</h3><p class="muted">' +
      esc((err && err.message) || 'render failed') +
      '</p><div class="row"><button class="btn" onclick="location.reload()">Reload</button></div></div>';
    return Promise.resolve();
  }
  return done.catch(function(err){
    if(err && err.code === 'auth'){
      p.innerHTML = signinCard();
      return;
    }
    state.lastError = (err && err.message) || 'load failed';
    p.innerHTML = '<div class="card"><h3>Something went wrong.</h3><p class="muted">' +
      esc(state.lastError) +
      '</p><div class="row"><button class="btn" onclick="location.reload()">Reload</button></div></div>';
  });
}

function vOverview(){
  return api('/api/admin/status').then(function(s){
    state.status = s;
    var ver = $('#ver');
    if(ver) ver.textContent = 'v' + (s.version || '');
    var prov = s.providers || {};
    var endpoint = s.endpoint || (window.location.origin + '/v1');
    return '<h2>Overview</h2>' +
    '<div class="grid">' +
      '<div class="card"><h3>Gateway</h3><div class="big">● Running</div><div class="muted small">v' + esc(s.version || '') + '</div></div>' +
      '<div class="card"><h3>Endpoint</h3><div class="mono small break">' + esc(endpoint) + '</div>' +
        '<div class="row" style="margin-top:8px"><button class="btn ghost" id="ovCopy">Copy</button></div></div>' +
      '<div class="card"><h3>API Key</h3><div class="mono">' + esc(s.api_key_masked || '—') + '</div><div class="muted small">Manage in API Keys</div></div>' +
      '<div class="card"><h3>Providers</h3><div class="big">' + (prov.connected != null ? prov.connected : 0) + ' Connected</div>' +
        '<div class="muted small">' + (prov.unhealthy || 0) + ' unhealthy · ' + (prov.keys || 0) + ' keys</div></div>' +
      '<div class="card"><h3>Requests</h3><div class="big">' + (s.requests != null ? s.requests : 0) + '</div>' +
        '<div class="muted small">success ' + Number(s.success_rate || 0).toFixed(1) + '%</div></div>' +
      '<div class="card"><h3>Latency</h3><div class="big">' + (Number(s.avg_latency_ms || 0) / 1000).toFixed(2) + 's</div>' +
        '<div class="muted small">avg · fallbacks ' + (s.fallbacks || 0) + '</div></div>' +
    '</div>' +
    '<div class="card"><h3>First-run wizard</h3>' +
      '<p class="muted">Add your first provider to get started. Beginner flow: start → dashboard → add key → Test → copy unified key.</p>' +
      '<div class="row"><button class="btn" data-goto="providers">Connect provider</button>' +
      '<button class="btn ghost" data-goto="playground">Open playground</button></div>' +
    '</div>';
  }).catch(function(e){
    if(e && e.code === 'auth') throw e;
    return '<h2>Overview</h2><div class="card"><h3>First-run wizard</h3>' +
      '<p class="muted">Add your first provider to get started. Beginner flow: start → dashboard → add key → Test → copy unified key.</p>' +
      '<div class="row"><button class="btn" data-goto="providers">Connect provider</button>' +
      '<button class="btn ghost" data-goto="playground">Open playground</button></div></div>' +
      '<div class="card" style="margin-top:12px"><h3>Status unavailable</h3><p class="muted small">' +
      esc(e.message) + ' — sign in to see live stats.</p><div class="row"><button class="btn ghost" data-act="login">Sign in</button></div></div>';
  });
}
function bindOverview(){
  var b = $('#ovCopy');
  if(b && state.status) b.onclick = function(){ copyText(state.status.endpoint || (window.location.origin + '/v1')); };
}

var PTYPES = [['google','Google Gemini'],['groq','Groq'],['openrouter','OpenRouter'],['cerebras','Cerebras'],['mistral','Mistral'],['nvidia','NVIDIA'],['github','GitHub Models'],['cloudflare','Cloudflare Workers AI'],['huggingface','Hugging Face'],['ollama','Ollama (local)'],['custom','Custom OpenAI-compatible']];

function vProviders(){
  return api('/api/providers').then(function(b){
    state.providers = b.providers || [];
    var cards = state.providers.map(function(p){
      return '<div class="card"><div class="row" style="justify-content:space-between"><strong>' +
        esc(p.display_name || p.type) + '</strong>' + pill(p.health) + '</div>' +
        '<div class="muted small">' + esc(p.type) + ' · ' + p.keys + ' keys · ' + (p.enabled ? 'enabled' : 'disabled') + '</div>' +
        '<div class="row" style="margin-top:8px"><button class="btn ghost" data-edit="' + esc(p.id) + '">Edit</button>' +
        '<button class="btn ghost" data-test="' + esc(p.id) + '">Test</button>' +
        '<button class="btn danger" data-del="' + esc(p.id) + '">Delete</button></div></div>';
    }).join('') || '<p class="muted">No providers yet.</p>';
    var opts = PTYPES.map(function(o){ return '<option value="' + o[0] + '">' + o[1] + '</option>'; }).join('');
    return '<h2>Providers</h2><div class="card"><h3>Add provider</h3>' +
      '<div class="row"><select id="npType">' + opts + '</select></div>' +
      '<label>Display name<input id="npName" placeholder="e.g. My Google key"></label>' +
      '<label>Base URL (optional for presets; required for custom)<input id="npBase" placeholder="https://... or http://localhost:11434/v1"></label>' +
      '<label>API key(s) — one per line for rotation<input id="npKey" type="password" placeholder="sk-..."></label>' +
      '<label>Model IDs (custom only, comma separated)<input id="npModels" placeholder="llama3.3, qwen2.5"></label>' +
      '<div class="row"><button class="btn" id="npAdd">Add provider</button><button class="btn ghost" id="npTest">Test connection</button></div>' +
      '<p class="muted small" id="npMsg"></p></div>' +
      '<h3>Configured</h3><div class="grid">' + cards + '</div>';
  });
}
function bindProviders(){
  function g(id){ return $(id); }
  if(!g('#npAdd')) return;
  g('#npAdd').onclick = function(){
    var payload = {
      type: g('#npType').value,
      display_name: g('#npName').value,
      base_url: g('#npBase').value,
      api_key: g('#npKey').value,
      model_ids: g('#npModels').value.split(',').map(function(s){ return s.trim(); }).filter(Boolean)
    };
    api('/api/providers', { method: 'POST', body: JSON.stringify(payload) }).then(function(){
      toast('Provider added'); render();
    }).catch(function(e){ g('#npMsg').textContent = e.message; });
  };
  g('#npTest').onclick = function(){
    api('/api/providers/test', { method: 'POST', body: JSON.stringify({
      type: g('#npType').value, base_url: g('#npBase').value, api_key: g('#npKey').value
    })}).then(function(r){
      g('#npMsg').textContent = (r.ok ? '✓ Success: ' : '✕ ' + (r.state + ': ')) + (r.message || '');
    }).catch(function(e){ g('#npMsg').textContent = e.message; });
  };
  $$('[data-del]').forEach(function(b){
    b.onclick = function(){
      if(!window.confirm('Delete provider?')) return;
      api('/api/providers/' + b.getAttribute('data-del'), { method: 'DELETE' }).then(function(){
        toast('Deleted'); render();
      }).catch(function(e){ toast(e.message); });
    };
  });
  $$('[data-test]').forEach(function(b){
    b.onclick = function(){
      var p = null;
      for(var i = 0; i < state.providers.length; i++){
        if(state.providers[i].id === b.getAttribute('data-test')) p = state.providers[i];
      }
      if(!p) return;
      api('/api/providers/test', { method: 'POST', body: JSON.stringify({
        type: p.type, base_url: p.base_url || '', api_key: ''
      })}).then(function(r){ toast('Health: ' + r.state); })
      .catch(function(e){ toast(e.message); });
    };
  });
  $$('[data-edit]').forEach(function(b){
    b.onclick = function(){
      var en = window.confirm('OK = enable, Cancel = disable');
      api('/api/providers/' + b.getAttribute('data-edit'), { method: 'PUT', body: JSON.stringify({ enabled: en }) })
        .then(function(){ render(); })
        .catch(function(e){ toast(e.message); });
    };
  });
}

function vModels(){
  return api('/api/models').then(function(b){
    state.models = b.models || [];
    var rows = state.models.map(function(m){
      var caps = [];
      if(m.supports_vision) caps.push('Vision');
      if(m.supports_tools) caps.push('Tools');
      if(m.supports_streaming) caps.push('Streaming');
      if(m.supports_embeddings) caps.push('Embed');
      return '<tr><td><strong>' + esc(m.display_name || m.id) + '</strong><br>' +
        '<span class="muted small mono break">' + esc(m.id) + '</span></td>' +
        '<td>' + esc(m.provider) + '</td>' +
        '<td class="small">' + esc(caps.join(' · ')) + '</td>' +
        '<td>' + (m.enabled ? '● Enabled' : '○ Disabled') + '</td>' +
        '<td><button class="btn ghost" data-tgl="' + esc(m.id) + '">' + (m.enabled ? 'Disable' : 'Enable') + '</button></td></tr>';
    }).join('');
    return '<h2>Models</h2><div class="card"><input id="mq" placeholder="Search models..." aria-label="Search models"></div>' +
      '<div class="card" style="margin-top:12px;overflow:auto"><table><thead><tr><th>Model</th><th>Provider</th><th>Caps</th><th>Status</th><th></th></tr></thead>' +
      '<tbody id="mrows">' + rows + '</tbody></table></div>';
  });
}
function bindModels(){
  var q = $('#mq');
  if(!q) return;
  q.oninput = function(){
    var s = q.value.toLowerCase();
    $$('#mrows tr').forEach(function(tr){
      tr.style.display = tr.textContent.toLowerCase().indexOf(s) >= 0 ? '' : 'none';
    });
  };
  $$('[data-tgl]').forEach(function(b){
    b.onclick = function(){
      var id = b.getAttribute('data-tgl');
      var m = null;
      for(var i = 0; i < state.models.length; i++){
        if(state.models[i].id === id) m = state.models[i];
      }
      api('/api/models/' + encodeURIComponent(id), { method: 'PUT', body: JSON.stringify({ enabled: !(m && m.enabled) }) })
        .then(function(){ render(); })
        .catch(function(e){ toast(e.message); });
    };
  });
}

function vApi(){
  var origin = '';
  try{ origin = window.location.origin; }catch(e){}
  var endpoint = (state.status && state.status.endpoint) || (origin + '/v1');
  var keyMasked = (state.status && state.status.api_key_masked) || 'obg_… (see API Keys)';
  return '<h2>API</h2>' +
    '<div class="card"><h3>Unified endpoint</h3>' +
      '<div class="mono small break">' + esc(endpoint) + '</div>' +
      '<div class="row" style="margin-top:8px"><button class="btn ghost" id="apiCopyEp">Copy endpoint</button>' +
      '<button class="btn ghost" data-goto="keys">Manage keys</button></div></div>' +
    '<div class="card" style="margin-top:12px"><h3>Authentication</h3>' +
      '<p class="muted small">Send your unified key as <span class="mono">Authorization: Bearer ' + esc(keyMasked) + '</span>.</p>' +
      '<pre class="mono small">curl ' + esc(endpoint) + '/chat/completions \\\n' +
      '  -H "Authorization: Bearer obg_your_key" \\\n' +
      '  -H "Content-Type: application/json" \\\n' +
      '  -d ' + esc('\'{"model":"auto","messages":[{"role":"user","content":"Hello"}]}\'') + '</pre>' +
      '<div class="row"><button class="btn ghost" id="apiCopyCurl">Copy curl</button></div></div>' +
    '<div class="card" style="margin-top:12px"><h3>Reference</h3>' +
      '<div class="kv"><span>Health</span><b class="mono">GET /health</b></div>' +
      '<div class="kv"><span>Models</span><b class="mono">GET /v1/models</b></div>' +
      '<div class="kv"><span>Chat</span><b class="mono">POST /v1/chat/completions</b></div>' +
      '<div class="kv"><span>Docs</span><b><a href="/v1/docs">/v1/docs</a> · <a href="/v1/openapi.json">openapi.json</a></b></div></div>';
}
function bindApi(){
  var ep = (state.status && state.status.endpoint) || (window.location.origin + '/v1');
  var c1 = $('#apiCopyEp');
  if(c1) c1.onclick = function(){ copyText(ep); };
  var c2 = $('#apiCopyCurl');
  if(c2) c2.onclick = function(){
    copyText('curl ' + ep + '/chat/completions -H "Authorization: Bearer obg_your_key" -H "Content-Type: application/json" -d \'{"model":"auto","messages":[{"role":"user","content":"Hello"}]}\'');
  };
  if(!state.status){
    api('/api/admin/status').then(function(s){
      state.status = s;
      var ver = $('#ver');
      if(ver) ver.textContent = 'v' + (s.version || '');
    }).catch(function(){});
  }
}

function vRouting(){
  return api('/api/routing').then(function(r){
    var strats = ['auto', 'priority', 'fastest', 'cheapest', 'balanced'];
    var opts = strats.map(function(s){ return '<option' + (s === r.strategy ? ' selected' : '') + '>' + s + '</option>'; }).join('');
    return '<h2>Routing</h2><div class="card">' +
      '<label>Strategy<select id="rtStr">' + opts + '</select></label>' +
      '<label>Max attempts (1–20)<input id="rtMax" type="number" min="1" max="20" value="' + esc(r.max_attempts) + '"></label>' +
      '<label class="row" style="flex-direction:row"><input type="checkbox" id="rtSticky"' + (r.sticky_sessions ? ' checked' : '') + ' style="width:auto"> Sticky sessions (30 min default)</label>' +
      '<div class="row"><button class="btn" id="rtSave">Save</button></div></div>' +
      '<div class="card" style="margin-top:12px"><h3>Virtual models</h3>' +
      '<p class="muted small"><span class="mono">auto</span>, <span class="mono">auto:fast</span>, <span class="mono">auto:balanced</span>, ' +
      '<span class="mono">auto:priority</span> — plus <span class="mono">provider/model</span> or bare IDs.</p></div>';
  });
}
function bindRouting(){
  if(!$('#rtSave')) return;
  $('#rtSave').onclick = function(){
    api('/api/routing', { method: 'PUT', body: JSON.stringify({
      strategy: $('#rtStr').value,
      max_attempts: +$('#rtMax').value,
      sticky_sessions: $('#rtSticky').checked
    })}).then(function(){ toast('Routing saved'); })
    .catch(function(e){ toast(e.message); });
  };
}

function vKeys(){
  return api('/api/keys').then(function(b){
    state.keys = b.keys || [];
    var rows = state.keys.map(function(k){
      return '<div class="card"><strong>' + esc(k.name) + '</strong>' +
        '<div class="mono small">' + esc(k.prefix) + '••••••••</div>' +
        '<div class="muted small">Created ' + esc(k.created_at || '') + ' · Last used ' + esc(k.last_used_at || 'never') + '</div>' +
        '<div class="row" style="margin-top:8px"><button class="btn danger" data-kdel="' + esc(k.id) + '">Revoke</button></div></div>';
    }).join('') || '<p class="muted">No keys.</p>';
    return '<h2>API Keys</h2><div class="card"><h3>New key</h3>' +
      '<div class="row"><input id="nkName" placeholder="My Application" style="flex:1"><button class="btn" id="nkAdd">Create</button></div>' +
      '<p class="mono small break" id="nkOut"></p>' +
      '<p class="muted small">Clients use <span class="mono">Authorization: Bearer obg_...</span> against <span class="mono">/v1</span>.</p></div>' +
      '<div class="grid" style="margin-top:12px">' + rows + '</div>';
  });
}
function bindKeys(){
  if(!$('#nkAdd')) return;
  $('#nkAdd').onclick = function(){
    api('/api/keys', { method: 'POST', body: JSON.stringify({ name: $('#nkName').value }) }).then(function(b){
      $('#nkOut').textContent = b.key;
      copyText(b.key, 'Key created & copied');
      render();
    }).catch(function(e){ toast(e.message); });
  };
  $$('[data-kdel]').forEach(function(x){
    x.onclick = function(){
      if(!window.confirm('Revoke key?')) return;
      api('/api/keys/' + x.getAttribute('data-kdel'), { method: 'DELETE' })
        .then(function(){ render(); })
        .catch(function(e){ toast(e.message); });
    };
  });
}

function vPlayground(){
  var opts = '<option>auto</option><option>auto:fast</option><option>auto:balanced</option>' +
    (state.models || []).map(function(m){ return '<option>' + esc(m.id) + '</option>'; }).join('');
  return '<h2>Playground</h2><div class="card">' +
    '<div class="row"><select id="pgModel">' + opts + '</select>' +
    '<input id="pgTemp" type="number" step="0.1" min="0" max="2" value="0.7" style="max-width:100px" aria-label="Temperature"></div>' +
    '<label>System prompt<textarea id="pgSys" rows="2"></textarea></label>' +
    '<label>User<textarea id="pgUser" rows="4"></textarea></label>' +
    '<div class="row"><button class="btn" id="pgSend">Send</button></div>' +
    '<div id="pgOut" style="margin-top:12px"></div></div>';
}
function bindPlayground(){
  if(!$('#pgSend')) return;
  function ready(){ $('#pgSend').onclick = send; }
  if(!state.models.length){
    api('/api/models').then(function(b){
      state.models = b.models || [];
      var sel = $('#pgModel');
      if(sel) sel.innerHTML = '<option>auto</option><option>auto:fast</option><option>auto:balanced</option>' +
        state.models.map(function(m){ return '<option>' + esc(m.id) + '</option>'; }).join('');
      ready();
    }).catch(function(){ ready(); });
  } else { ready(); }
  function send(){
    $('#pgOut').innerHTML = '<p class="muted">…</p>';
    api('/api/playground', { method: 'POST', body: JSON.stringify({
      model: $('#pgModel').value, system: $('#pgSys').value,
      user: $('#pgUser').value, temperature: +$('#pgTemp').value
    })}).then(function(r){
      $('#pgOut').innerHTML = '<pre>' + esc(r.response) + '</pre>' +
        '<div class="kv"><span>Provider</span><b>' + esc(r.provider) + '</b></div>' +
        '<div class="kv"><span>Model</span><b class="mono">' + esc(r.model) + '</b></div>' +
        '<div class="kv"><span>Latency</span><b>' + esc(r.latency) + '</b></div>' +
        '<div class="kv"><span>Tokens</span><b>' + r.tokens.total + ' (in ' + r.tokens.prompt + ' / out ' + r.tokens.completion + ')</b></div>' +
        '<div class="kv"><span>Fallbacks</span><b>' + r.fallbacks + '</b></div>';
    }).catch(function(e){
      $('#pgOut').innerHTML = '<p class="err">✕ Failed: ' + esc(e.message) + '</p>';
    });
  }
}

function vAnalytics(){
  return api('/api/analytics?hours=168').then(function(s){
    var prov = Object.keys(s.by_provider || {}).map(function(k){
      return '<div class="kv"><span>' + esc(k || '(none)') + '</span><b>' + s.by_provider[k] + '</b></div>';
    }).join('') || '<p class="muted">No data.</p>';
    var mods = Object.keys(s.by_model || {}).map(function(k){
      return '<div class="kv"><span class="mono">' + esc(k) + '</span><b>' + s.by_model[k] + '</b></div>';
    }).join('');
    var tot = s.input_tokens + s.output_tokens;
    return '<h2>Analytics</h2><div class="grid">' +
      '<div class="card"><h3>Total</h3><div class="big">' + s.total + '</div></div>' +
      '<div class="card"><h3>Success rate</h3><div class="big">' + Number(s.success_rate || 0).toFixed(1) + '%</div></div>' +
      '<div class="card"><h3>Avg latency</h3><div class="big">' + (Number(s.avg_latency_ms || 0) / 1000).toFixed(2) + 's</div></div>' +
      '<div class="card"><h3>Tokens</h3><div class="big">' + tot + '</div><div class="muted small">in ' + s.input_tokens + ' · out ' + s.output_tokens + '</div></div></div>' +
      '<div class="card"><h3>By provider</h3>' + prov + '</div>' +
      '<div class="card" style="margin-top:12px"><h3>By model</h3>' + (mods || '<p class="muted">No data.</p>') + '</div>';
  });
}

function vSettings(){
  return api('/api/settings').then(function(b){
    var s = b.settings || {};
    return '<h2>Settings</h2><div class="card">' +
      '<div class="kv"><span>Host</span><b class="mono">' + esc(s.host || '') + '</b></div>' +
      '<div class="kv"><span>Port</span><b>' + esc(s.port || '') + '</b></div>' +
      '<div class="kv"><span>Low-resource mode</span><b>' + esc(s.low_resource || 'false') + '</b></div>' +
      '<p class="muted small">Server options are set via environment (<span class="mono">OPENBRIDGE_HOST/PORT/DATA_DIR/MASTER_KEY/LOG_LEVEL/LOW_RESOURCE</span>). ' +
      'LAN mode (0.0.0.0) shows a warning: others on your network could use your quota.</p>' +
      '<div class="row"><button class="btn ghost" id="expBtn">Export config</button>' +
      '<button class="btn ghost" id="diagBtn">Diagnostics</button>' +
      '<button class="btn ghost" id="logoutBtn">Sign out</button></div></div>';
  });
}
function bindSettings(){
  if(!$('#logoutBtn')) return;
  $('#logoutBtn').onclick = function(){
    storeDel('obg_session');
    state.token = '';
    try{
      api('/api/admin/logout', { method: 'POST' }).catch(function(){});
    }catch(e){}
    showLogin();
  };
  $('#expBtn').onclick = function(){ window.open('/api/export', '_blank'); };
  var dg = $('#diagBtn');
  if(dg) dg.onclick = function(){ openDiag(); };
}

/* ---------- boot ---------- */

function boot(){
  try{
    state.page = pageFromPath();
  }catch(e){ state.page = 'overview'; }
  try{
    bindChrome();
  }catch(err){
    showFatal((err && err.message) || 'init failed');
    return;
  }
  setActiveNav();
  try{
    window.__obg_boot = true;
  }catch(e){}
  window.toast = toast;
  window.obgNavigate = navigate;
  render();
}

if(document.readyState === 'loading'){
  document.addEventListener('DOMContentLoaded', boot);
} else {
  boot();
}
})();
