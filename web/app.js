// ===========================
// InfraGraph — vis.js Frontend
// v3 — indexed queries + progress bar + hardened error handling
// ===========================

const API_BASE = window.location.origin;

let network       = null;
let currentQuery  = 'full';
let viewMode      = 'graph'; // 'graph' or 'list'
let edgeLabels    = false;   // off by default — big render win
let clusterOn     = false;
let nodeLimit     = 200;
let allNodes      = new vis.DataSet();
let allEdges      = new vis.DataSet();

// ── Progress state ────────────────────────────────────────────────────────────
let progressTimer = null;
let progressPct   = 0;
const STAGES = {
  start:   { pct: 5,  label: 'Connecting to server…' },
  query:   { pct: 35, label: 'Querying Neo4j (indexed lookup)…' },
  parse:   { pct: 70, label: 'Processing graph data…' },
  render:  { pct: 88, label: 'Building layout…' },
  physics: { pct: 95, label: 'Loading graph layout…' },
  done:    { pct: 100, label: 'Graph loaded ✓' },
};

function setStage(stageName) {
  const s = STAGES[stageName];
  if (!s) return;
  animateProgress(s.pct, s.label);
}

function animateProgress(targetPct, label) {
  clearInterval(progressTimer);
  const bar  = document.getElementById('progress-bar');
  const pct  = document.getElementById('progress-pct');
  const lbl  = document.getElementById('progress-label');
  if (lbl) lbl.textContent = label;

  progressTimer = setInterval(() => {
    if (progressPct < targetPct) {
      progressPct = Math.min(progressPct + 1.5, targetPct);
    } else {
      progressPct = targetPct;
      clearInterval(progressTimer);
    }
    if (bar) bar.style.width = progressPct.toFixed(1) + '%';
    if (pct) pct.textContent = Math.round(progressPct) + '%';
  }, 40);
}

// ── Node styling ──────────────────────────────────────────────────────────────
const GROUP_STYLES = {
  Domain: {
    color: { background: '#0a1a2e', border: '#00d4ff', highlight: { background: '#0d2240', border: '#00d4ff' } },
    font:  { color: '#00d4ff', size: 12, face: 'JetBrains Mono' },
    shape: 'dot', size: 14,
  },
  IP: {
    color: { background: '#1a1200', border: '#f59e0b', highlight: { background: '#261b00', border: '#f59e0b' } },
    font:  { color: '#f59e0b', size: 11, face: 'JetBrains Mono' },
    shape: 'diamond', size: 12,
  },
  CNAME: {
    color: { background: '#1a0a0a', border: '#ef4444', highlight: { background: '#261010', border: '#ef4444' } },
    font:  { color: '#ef4444', size: 11, face: 'JetBrains Mono' },
    shape: 'triangle', size: 12,
  },
  Technology: {
    color: { background: '#0a1a12', border: '#10b981', highlight: { background: '#0d261a', border: '#10b981' } },
    font:  { color: '#10b981', size: 10, face: 'Inter' },
    shape: 'square', size: 10,
  },
  HTTPTitle: {
    color: { background: '#110a1a', border: '#7c3aed', highlight: { background: '#180d26', border: '#7c3aed' } },
    font:  { color: '#a78bfa', size: 10, face: 'Inter' },
    shape: 'ellipse', size: 10,
  },
  Unknown: {
    color: { background: '#111', border: '#444', highlight: { background: '#1a1a1a', border: '#666' } },
    font:  { color: '#94a3b8', size: 10 },
    shape: 'dot', size: 8,
  },
};

const EDGE_COLORS = {
  RESOLVES_TO:    '#00d4ff',
  HAS_CNAME:      '#ef4444',
  RUNS_ON:        '#10b981',
  HAS_TITLE:      '#7c3aed',
  DANGLING_CNAME: '#ef4444',
  default:        '#4b5563',
};

// ── Init ──────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  const saved = parseInt(localStorage.getItem('ig-node-limit') || '200', 10);
  nodeLimit = isNaN(saved) ? 200 : saved;
  const slider  = document.getElementById('node-limit-slider');
  const display = document.getElementById('node-limit-val');
  if (slider)  slider.value      = nodeLimit;
  if (display) display.textContent = nodeLimit;

  initGraph();
  fetchStats();
  loadGraph('full');

  if (slider) {
    slider.addEventListener('input', () => {
      nodeLimit = parseInt(slider.value, 10);
      if (display) display.textContent = nodeLimit;
      localStorage.setItem('ig-node-limit', nodeLimit);
    });
    slider.addEventListener('change', () => loadGraph(currentQuery));
  }
});

// ── Graph init ────────────────────────────────────────────────────────────────
function initGraph() {
  const container = document.getElementById('graph-canvas');
  if (!container) return;

  const options = {
    nodes: {
      borderWidth: 2,
      borderWidthSelected: 4,
      shadow: { enabled: true, color: 'rgba(0, 212, 255, 0.4)', size: 15, x: 0, y: 0 },
    },
    edges: {
      width: 1.5,
      smooth: { enabled: true, type: 'continuous' }, // aesthetic curves
      arrows: { to: { enabled: true, scaleFactor: 0.5 } },
      color: { inherit: false },
      font: { color: '#94a3b8', size: 10, face: 'Inter', strokeWidth: 2, strokeColor: '#0a0c14', align: 'middle' },
      selectionWidth: 3,
    },
    physics: {
      enabled: true,
      barnesHut: {
        gravitationalConstant: -8000,
        centralGravity: 0.3,
        springLength: 120,
        springConstant: 0.04,
        damping: 0.09,
        avoidOverlap: 0.2,
      },
      solver: 'barnesHut',
      stabilization: {
        enabled: true,
        iterations: 80,        // was 150 — reduced for speed
        updateInterval: 50,
        fit: true,
      },
      maxVelocity: 50,
      minVelocity: 1.0,        // stop physics sooner
      timestep: 0.5,
    },
    interaction: {
      hover: true,
      tooltipDelay: 150,
      navigationButtons: false,
      keyboard: { enabled: true, bindToWindow: false },
      multiselect: false,
    },
    layout: {
      improvedLayout: false,   // skip Kamada-Kawai pre-layout
    },
    groups: GROUP_STYLES,
  };

  network = new vis.Network(container, { nodes: allNodes, edges: allEdges }, options);

  network.on('click', (params) => {
    if (params.nodes.length > 0) {
      openInspector(allNodes.get(params.nodes[0]));
    } else {
      closeInspector();
    }
  });

  // Physics done → disable to free CPU, complete progress
  const finishPhysics = () => {
    network.setOptions({ physics: { enabled: false } });
    setStage('done');
    setTimeout(hideLoading, 400);
  };
  network.on('stabilizationIterationsDone', finishPhysics);
  network.on('stabilized', finishPhysics);

  network.on('doubleClick', (params) => {
    if (params.nodes.length > 0 && network.isCluster(params.nodes[0])) {
      network.openCluster(params.nodes[0]);
    }
  });
}

// ── Data loading ──────────────────────────────────────────────────────────────
async function loadGraph(query) {
  if (!network) return;
  currentQuery = query;

  updateActiveButton(query);
  showLoading();
  setStage('start');

  // Re-enable physics for new layout
  network.setOptions({ physics: { enabled: true, stabilization: { iterations: 80 } } });

  const endpointMap = {
    'full':          `/api/graph?limit=${nodeLimit}`,
    'shared-infra':  '/api/query/shared-infra',
    'tech-exposure': '/api/query/tech-exposure',
  };

  const titleMap = {
    'full':          'Full Attack Surface',
    'shared-infra':  '🔗 Shared Infrastructure',
    'tech-exposure': '⚙️ Technology Exposure',
  };

  const endpoint = endpointMap[query];
  if (!endpoint) {
    hideLoading();
    showToast('Unknown query: ' + query, 'error');
    return;
  }

  setStage('query');

  let data;
  try {
    const resp = await fetch(API_BASE + endpoint);

    // Always read body — even on error it contains useful detail
    const bodyText = await resp.text();

    if (!resp.ok) {
      // Try to parse structured JSON error from our new apiError() format
      let detail = bodyText;
      try {
        const parsed = JSON.parse(bodyText);
        detail = parsed.detail || parsed.error || bodyText;
      } catch (_) { /* body was plain text */ }
      throw new Error(`[HTTP ${resp.status}] ${detail}`);
    }

    // Validate content type before JSON parse
    const ct = resp.headers.get('content-type') || '';
    if (!ct.includes('application/json')) {
      throw new Error(`Expected JSON but got: ${ct}\n${bodyText.slice(0, 200)}`);
    }

    setStage('parse');
    data = JSON.parse(bodyText);

  } catch (err) {
    hideLoading();
    const msg = err.message || String(err);
    showToast('Load error — see console for details', 'error');
    console.error('[InfraGraph] loadGraph error:', msg);
    document.getElementById('graph-title').textContent = '⚠ Error: ' + msg.slice(0, 80);
    return;
  }

  // Validate response shape
  if (!data || typeof data !== 'object') {
    hideLoading();
    showToast('Invalid API response (not an object)', 'error');
    return;
  }

  setStage('render');
  renderGraph(data, query, titleMap[query] || query);
  if (viewMode === 'list') {
    renderListView(data, query);
  }
}

function renderGraph(data, query, titleLabel) {
  const rawNodes = Array.isArray(data.nodes) ? data.nodes : [];
  const rawEdges = Array.isArray(data.edges) ? data.edges : [];

  const nodes = rawNodes.map(n => ({
    id:    n.id,
    label: truncateLabel(n.label || '', 28),
    title: n.title || n.label || '',
    group: n.group || 'Unknown',
    _raw:  n,
  }));

  const edges = rawEdges.map((e, i) => ({
    id:    'e' + i,
    from:  e.from,
    to:    e.to,
    label: edgeLabels ? (e.label || '') : undefined,
    color: { color: EDGE_COLORS[e.label] || EDGE_COLORS.default, opacity: 0.65 },
    title: e.label || '',
  }));

  setStage('physics');
  
  // Batch update — single DOM flush
  // Use a small timeout to let the UI paint the loading state before we lock the thread
  setTimeout(() => {
    allNodes.clear();
    allEdges.clear();
    allNodes.add(nodes);
    allEdges.add(edges);

    // Performance vs Aesthetics: Turn off smooth curves for huge graphs
    if (nodes.length > 300) {
      network.setOptions({ edges: { smooth: { enabled: false } } });
    } else {
      network.setOptions({ edges: { smooth: { enabled: true, type: 'continuous' } } });
    }

    document.getElementById('graph-title').textContent   = titleLabel;
    document.getElementById('node-count').textContent =
      `${nodes.length} nodes · ${edges.length} edges`;

    if (clusterOn) applyClustersByGroup();

    // Tiny graphs: skip physics
    if (nodes.length <= 6) {
      network.setOptions({ physics: { enabled: false } });
      setStage('done');
      setTimeout(() => {
        hideLoading();
        network.fit({ animation: { duration: 400, easingFunction: 'easeInOutQuad' } });
      }, 100);
      return;
    }

    // Empty result
    if (nodes.length === 0) {
      network.setOptions({ physics: { enabled: false } });
      setStage('done');
      setTimeout(hideLoading, 200);
      showToast('No data found. Run a scan first (-target example.com).', 'warn');
      return;
    }

    showToast(`Loaded ${nodes.length} nodes · ${edges.length} edges`, 'success');
    
    // Fallback: if physics takes too long or stabilization fires instantly, forcefully hide loading after 4s
    setTimeout(() => {
      const overlay = document.getElementById('loading-overlay');
      if (overlay && !overlay.classList.contains('hidden')) {
        network.setOptions({ physics: { enabled: false } });
        setStage('done');
        hideLoading();
      }
    }, 4000);
  }, 50);
}

// ── Clustering ────────────────────────────────────────────────────────────────
function applyClustersByGroup() {
  if (!network) return;
  ['Technology', 'HTTPTitle', 'CNAME', 'IP', 'Domain'].forEach(group => {
    // Get items of this group from the DataSet using get with filter
    const items = allNodes.get({
      filter: function(n) { return n.group === group; }
    });
    if (!items || items.length <= 3) return;

    try {
      network.cluster({
        joinCondition: function(n) { return n.group === group; },
        clusterNodeProperties: {
          label:       group + ' (' + items.length + ')',
          borderWidth: 3,
          shape:       'database',
          size:        22 + Math.min(items.length * 2, 30),
          color: {
            border:     GROUP_STYLES[group] && GROUP_STYLES[group].color ? GROUP_STYLES[group].color.border : '#888',
            background: '#0d1117',
          },
          font: {
            color: GROUP_STYLES[group] && GROUP_STYLES[group].color ? GROUP_STYLES[group].color.border : '#888',
            size: 13,
          },
          title: 'Cluster: ' + items.length + ' ' + group + ' nodes — double-click to expand',
        },
      });
    } catch (e) {
      console.warn('[cluster] skipped group', group, e.message);
    }
  });
}

function toggleClustering() {
  clusterOn = !clusterOn;
  const btn = document.getElementById('btn-cluster');
  if (clusterOn) {
    applyClustersByGroup();
    if (btn) { btn.textContent = '🗂 Uncluster'; btn.classList.add('active'); }
    showToast('Double-click a cluster to expand', 'info');
  } else {
    loadGraph(currentQuery);
    if (btn) { btn.textContent = '🗂 Cluster'; btn.classList.remove('active'); }
  }
}

// ── Stats ─────────────────────────────────────────────────────────────────────
async function fetchStats() {
  try {
    const resp = await fetch(API_BASE + '/api/stats');
    if (!resp.ok) throw new Error('stats ' + resp.status);
    const s = await resp.json();
    document.getElementById('val-domains').textContent  = s.domains  ?? '—';
    document.getElementById('val-ips').textContent      = s.ips      ?? '—';
    document.getElementById('val-cnames').textContent   = s.cnames   ?? '—';
    document.getElementById('val-tech').textContent     = s.technologies ?? '—';
    document.getElementById('val-edges').textContent    = s.edges    ?? '—';
    document.getElementById('status-dot').classList.add('connected');
    document.getElementById('status-label').textContent = 'Connected';
  } catch (e) {
    document.getElementById('status-dot').classList.add('error');
    document.getElementById('status-label').textContent = 'Offline';
  }
}

// ── Inspector ─────────────────────────────────────────────────────────────────
function openInspector(node) {
  if (!node) return;
  const panel = document.getElementById('inspector');
  const body  = document.getElementById('inspector-body');
  const raw   = node._raw || {};
  
  // Aggregate connected nodes by group
  let connectedHTML = '';
  if (network) {
    const connectedNodeIds = network.getConnectedNodes(node.id);
    const groups = {};
    connectedNodeIds.forEach(cId => {
      const cNode = allNodes.get(cId);
      if (cNode) {
        if (!groups[cNode.group]) groups[cNode.group] = [];
        groups[cNode.group].push(cNode.title || cNode.label);
      }
    });

    for (const [groupName, items] of Object.entries(groups)) {
      connectedHTML += `
        <div class="prop-group" style="margin-top: 6px;">
          <div class="prop-label">Connected ${esc(groupName)}</div>
          <div class="prop-value" style="display:flex; flex-wrap:wrap; gap:4px; background:transparent; border:none; padding:0;">
            ${items.map(item => `<span class="list-badge ${groupName.toLowerCase()}">${esc(item)}</span>`).join('')}
          </div>
        </div>
      `;
    }
  }

  panel.classList.add('open');
  body.innerHTML = `
    <span class="node-type-badge badge-${node.group}">${esc(node.group)}</span>
    <div class="prop-group">
      <div class="prop-label">Label</div>
      <div class="prop-value">${esc(node.label)}</div>
    </div>
    ${raw.title ? `<div class="prop-group"><div class="prop-label">Full</div><div class="prop-value">${esc(raw.title)}</div></div>` : ''}
    <div class="prop-group">
      <div class="prop-label">ID</div>
      <div class="prop-value" style="font-size:10px;word-break:break-all;">${esc(String(node.id))}</div>
    </div>
    <div class="prop-group" style="margin-bottom: 8px;">
      <div class="prop-label">Total Edges</div>
      <div class="prop-value">${network ? network.getConnectedEdges(node.id).length : '—'}</div>
    </div>
    <hr style="border:0; border-top:1px solid var(--border); margin: 4px 0;" />
    ${connectedHTML}
  `;
}

function closeInspector() {
  const p = document.getElementById('inspector');
  if (p) p.classList.remove('open');
}

// ── View Mode Toggle ──────────────────────────────────────────────────────────
function toggleViewMode(mode) {
  if (viewMode === mode) return;
  viewMode = mode;
  
  const btnGraph = document.getElementById('btn-view-graph');
  const btnList  = document.getElementById('btn-view-list');
  const canvas   = document.getElementById('graph-canvas');
  const listView = document.getElementById('list-view');
  
  if (mode === 'graph') {
    btnGraph.classList.add('active');
    btnList.classList.remove('active');
    listView.classList.add('hidden');
    canvas.style.display = 'block';
    if (network) network.fit();
  } else {
    btnList.classList.add('active');
    btnGraph.classList.remove('active');
    listView.classList.remove('hidden');
    canvas.style.display = 'none';
    
    // Re-render the list based on current data without fetching again
    loadGraph(currentQuery); 
  }
}

function renderListView(data, query) {
  const container = document.getElementById('list-view');
  if (!data || !data.nodes || data.nodes.length === 0) {
    container.innerHTML = '<div style="padding:40px;text-align:center;color:#94a3b8;">No data found for this query.</div>';
    return;
  }

  let html = '<table class="data-table"><thead><tr><th class="num-col">#</th>';

  if (query === 'shared-infra') {
    html += '<th>Shared IP Address</th><th>Hosted Domains</th></tr></thead><tbody>';
    // Group domains by IP
    const ipMap = {};
    const ipNodes = data.nodes.filter(n => n.group === 'IP');
    ipNodes.forEach(ip => { ipMap[ip.id] = { ip: ip.title, domains: [] }; });
    
    data.edges.filter(e => e.label === 'RESOLVES_TO').forEach(e => {
      if (ipMap[e.to]) {
        const d = data.nodes.find(n => n.id === e.from);
        if (d) ipMap[e.to].domains.push(d.title);
      }
    });
    
    let count = 1;
    Object.values(ipMap).sort((a,b) => b.domains.length - a.domains.length).forEach(entry => {
      if (entry.domains.length > 0) {
        html += `<tr><td class="num-col">${count++}</td>
                 <td><span class="list-badge ip">${esc(entry.ip)}</span></td>
                 <td>${entry.domains.map(d => `<div>${esc(d)}</div>`).join('')}</td></tr>`;
      }
    });
  }
  else if (query === 'tech-exposure') {
    html += '<th>Technology Stack</th><th>Domains Running It</th></tr></thead><tbody>';
    const techMap = {};
    const techNodes = data.nodes.filter(n => n.group === 'Technology');
    techNodes.forEach(t => { techMap[t.id] = { tech: t.title, domains: [] }; });
    
    data.edges.filter(e => e.label === 'RUNS_ON').forEach(e => {
      if (techMap[e.to]) {
        const d = data.nodes.find(n => n.id === e.from);
        if (d) techMap[e.to].domains.push(d.title);
      }
    });

    let count = 1;
    Object.values(techMap).sort((a,b) => b.domains.length - a.domains.length).forEach(entry => {
      if (entry.domains.length > 0) {
        html += `<tr><td class="num-col">${count++}</td>
                 <td><span class="list-badge tech">${esc(entry.tech)}</span></td>
                 <td>${entry.domains.map(d => `<div style="margin-bottom:2px;">${esc(d)}</div>`).join('')}</td></tr>`;
      }
    });
  }
  else {
    // Full graph — simple list of all entities
    html += '<th>Node Label</th><th>Type / Group</th></tr></thead><tbody>';
    let count = 1;
    // Sort Domain first, then IP, etc.
    const sorted = [...data.nodes].sort((a,b) => a.group.localeCompare(b.group) || (a.title || '').localeCompare(b.title || ''));
    sorted.forEach(n => {
      const cls = n.group ? n.group.toLowerCase() : '';
      html += `<tr><td class="num-col">${count++}</td>
               <td>${esc(n.title || n.label)}</td>
               <td><span class="list-badge ${cls}">${esc(n.group)}</span></td></tr>`;
    });
  }
  
  html += '</tbody></table>';
  container.innerHTML = html;
}

// ── Controls ──────────────────────────────────────────────────────────────────
function togglePhysics(enabled) {
  if (network) network.setOptions({ physics: { enabled } });
}

function toggleEdgeLabels(visible) {
  edgeLabels = visible;
  const updates = allEdges.map(function(e) {
    return { id: e.id, label: visible ? (e.title || '') : undefined };
  });
  if (updates && updates.length) allEdges.update(updates);
}

function fitGraph() {
  if (network) network.fit({ animation: { duration: 500, easingFunction: 'easeInOutQuad' } });
}

function exportPNG() {
  if (!network) return;
  const canvas = document.querySelector('#graph-canvas canvas');
  if (!canvas) return showToast('No canvas found', 'error');
  const a = document.createElement('a');
  a.download = 'infragraph-' + currentQuery + '-' + Date.now() + '.png';
  a.href = canvas.toDataURL('image/png');
  a.click();
  showToast('PNG exported', 'success');
}

// ── Loading overlay ───────────────────────────────────────────────────────────
function showLoading() {
  progressPct = 0;
  clearInterval(progressTimer);
  const bar  = document.getElementById('progress-bar');
  const pct  = document.getElementById('progress-pct');
  const lbl  = document.getElementById('progress-label');
  if (bar) bar.style.width = '0%';
  if (pct) pct.textContent = '0%';
  if (lbl) lbl.textContent = 'Starting…';
  const overlay = document.getElementById('loading-overlay');
  if (overlay) overlay.classList.remove('hidden');
}

function hideLoading() {
  clearInterval(progressTimer);
  progressPct = 100;
  const bar = document.getElementById('progress-bar');
  const pct = document.getElementById('progress-pct');
  if (bar) bar.style.width = '100%';
  if (pct) pct.textContent = '100%';
  setTimeout(() => {
    const overlay = document.getElementById('loading-overlay');
    if (overlay) overlay.classList.add('hidden');
    progressPct = 0;
  }, 350);
}

// ── UI helpers ────────────────────────────────────────────────────────────────
function updateActiveButton(query) {
  document.querySelectorAll('.query-btn').forEach(b => b.classList.remove('active'));
  const map = {
    'full':          'btn-full-graph',
    'shared-infra':  'btn-shared',
    'tech-exposure': 'btn-tech',
  };
  const id = map[query];
  if (id) {
    const el = document.getElementById(id);
    if (el) el.classList.add('active');
  }
}

function showToast(message, type) {
  const toast = document.getElementById('toast');
  if (!toast) return;
  toast.textContent = message;
  toast.className   = 'toast show ' + (type || 'info');
  setTimeout(() => toast.classList.remove('show'), 3500);
}

function truncateLabel(str, max) {
  if (!str) return '';
  return str.length > max ? str.slice(0, max) + '…' : str;
}

function esc(str) {
  if (str === null || str === undefined) return '';
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

// Keep stats fresh
setInterval(fetchStats, 30000);
