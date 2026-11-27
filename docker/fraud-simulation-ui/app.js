const $ = id => document.getElementById(id);
const format = new Intl.NumberFormat();
const escape = value => String(value ?? '').replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]);
const short = value => String(value || '').length > 24 ? `…${String(value).slice(-23)}` : String(value || '—');
const money = row => row.amount == null ? '—' : `${row.currency || 'GHS'} ${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(row.amount)}`;
const duration = ms => ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(1)} s`;
const names = { approve: 'Approved', review: 'Review', block_and_review: 'Block & review', decline: 'Declined',
  no_trigger: 'No trigger', rejected: 'Rejected', unknown: 'Outcome unknown', ingesting: 'Ingesting',
  ready: 'Ready to evaluate', evaluating: 'Evaluating', async_queued: 'Async queued', async_running: 'Async running', error: 'Error', accepted: 'Accepted' };
const knownStatuses = new Set(Object.keys(names));
let latest, displayed, frozen = false, filter = 'all', selectedId, pinned = false, detailKey = '', selectedSnapshot;
let ledgerMarkup = '';
let queueMarkup = '';
let lastReceived = 0, connected = false;

function badge(status, active = false) {
  const safe = knownStatuses.has(status) ? status : 'unknown';
  return `<span class="status ${safe}${active ? ' active' : ''}">${escape(names[safe])}</span>`;
}

function updateConnection() {
  const stale = connected && Date.now() - lastReceived > 6000;
  $('connection').className = `connection ${connected && !stale ? 'connected' : 'disconnected'}`;
  $('connection').innerHTML = `<i></i>${stale ? 'Updates delayed' : connected ? 'Live connection' : 'Reconnecting'}`;
}

function render(snapshot) {
  displayed = snapshot;
  const { counts, stages } = snapshot;
  const percent = snapshot.target ? Math.min(100, counts.evaluated / snapshot.target * 100) : 0;
  $('demo-banner').hidden = !snapshot.preview && snapshot.mode !== 'demo';
  $('mode-description').textContent = snapshot.mode === 'deterministic' ? 'Deterministic simulation · choose the outcome, inspect the data, run the case.' : 'Continuous simulation · a front-row seat to every transaction.';
  $('deterministic-lab').hidden = snapshot.mode !== 'deterministic';
  $('run-id').textContent = snapshot.runId;
  $('run-id').title = snapshot.runId;
  $('run-state').textContent = snapshot.mode === 'demo' ? 'Demo' : snapshot.status.replaceAll('_', ' ');
  $('run-message').textContent = snapshot.message;
  $('evaluated').textContent = format.format(counts.evaluated);
  $('target').textContent = `of ${format.format(snapshot.target)} planned`;
  $('progress-percent').textContent = `${percent.toFixed(1)}%`;
  $('progress-bar').style.width = `${percent}%`;
  $('in-flight').textContent = format.format(snapshot.activeCount);
  $('ingested').textContent = `${format.format(counts.ingested)} ${snapshot.mode === 'deterministic' ? 'records ' : ''}ingested`;
  $('rule-hits').textContent = format.format(counts.hits);
  $('skipped').textContent = `${format.format(counts.skipped)} skipped`;
  $('flagged').textContent = format.format(counts.flagged);
  $('error-count').textContent = `${format.format(counts.errors)} errors`;
  document.querySelector('.mini-stages').classList.toggle('busy', snapshot.activeCount > 0);
  const peak = Math.max(1, ...snapshot.throughput);
  $('sparkline').setAttribute('d', snapshot.throughput.map((value, i) => `${i ? 'L' : 'M'}${i / 59 * 250} ${24 - value / peak * 22}`).join(' '));
  $('approved-strip').style.flex = snapshot.outcomes.approve || 0;
  $('flagged-strip').style.flex = counts.flagged || 0;
  $('error-strip').style.flex = counts.errors || 0;
  $('queued-stage').textContent = `${snapshot.upcomingCount} queued`;
  $('ingest-stage').textContent = `${stages.ingesting || 0} submitting`;
  const evaluating = (stages.evaluating || 0) + (stages.async_queued || 0) + (stages.async_running || 0) + (stages.ready || 0);
  $('engine-stage').textContent = `${evaluating} evaluating`;
  $('response-stage').textContent = `${format.format(counts.evaluated)} returned`;
  $('flow-state').textContent = snapshot.activeCount ? 'Traffic in motion' : snapshot.status === 'running' ? 'Between batches' : snapshot.status === 'completed' ? 'Run complete' : snapshot.status === 'failed' ? 'Run stopped' : 'Awaiting traffic';
  renderParticles(snapshot);
  const queued = snapshot.upcoming.length ? snapshot.upcoming.map((row, index) => `<div class="upcoming-row" data-hover-id="${escape(row.id)}" data-hover-kind="queued" tabindex="0"><span class="queue-index">${String(index + 1).padStart(2, '0')}</span><div><strong class="customer-name">${escape(row.customerName || 'Customer')}</strong><span class="upcoming-id">${escape(short(row.id))}</span><span class="upcoming-meta">${escape(row.leg || 'record')}${row.state ? ` · ${escape(row.state)}` : ''}</span></div><span class="upcoming-amount">${escape(money(row))}</span></div>`).join('') : `<div class="empty">${['completed', 'stopped', 'failed'].includes(snapshot.status) ? 'No more transactions scheduled.' : 'Preparing the next batch…'}</div>`;
  if (queued !== queueMarkup) { queueMarkup = queued; $('upcoming').innerHTML = queued; }
  $('queue-footnote').textContent = snapshot.upcomingCount ? `${snapshot.upcomingCount} queued · hover or focus to inspect request data` : 'The queue shows the next generated batch';
  renderLedger();
  $('events').innerHTML = snapshot.events.slice(0, 6).map(event => `<div class="event ${event.kind === 'error' ? 'error' : ''}"><time>${escape(new Date(event.at).toLocaleTimeString([], { hour12: false }))}</time><span title="${escape(event.message)}">${escape(event.message)}</span></div>`).join('') || '<p class="muted">Waiting for the first update.</p>';
  $('event-count').textContent = `${snapshot.events.length} recent events`;
  renderClock();
  if (snapshot.lab) renderLab(snapshot.lab);
}

function renderParticles(snapshot) {
  const particles = [];
  for (const row of snapshot.active.slice(0, 10)) {
    const ingest = row.stage === 'ingesting';
    particles.push({ id: row.id, cls: ingest ? 'ingest' : 'queue', from: ingest ? '14%' : '39%', to: ingest ? '38%' : '64%' });
  }
  for (const row of snapshot.recent.filter(row => snapshot.now - row.completedAt < 1800).slice(0, 5)) {
    particles.push({ id: `${row.id}-response`, cls: '', from: '65%', to: '88%' });
  }
  const container = $('flight-particles');
  const keys = new Set(particles.map(item => `${item.id}:${item.from}`));
  for (const element of [...container.children]) if (!keys.has(element.dataset.key)) element.remove();
  for (const particle of particles) {
    const key = `${particle.id}:${particle.from}`;
    if ([...container.children].some(element => element.dataset.key === key)) continue;
    const element = document.createElement('i');
    element.className = `flight-particle ${particle.cls}`;
    element.dataset.key = key;
    element.style.setProperty('--from', particle.from);
    element.style.setProperty('--to', particle.to);
    container.append(element);
  }
}

function renderClock() {
  if (!displayed || frozen) return;
  const elapsed = Math.max(0, Math.floor(((displayed.endedAt ?? displayed.now) - displayed.startedAt) / 1000));
  const hours = Math.floor(elapsed / 3600);
  $('elapsed').textContent = `${hours ? `${hours}:` : ''}${String(Math.floor(elapsed / 60) % 60).padStart(2, '0')}:${String(elapsed % 60).padStart(2, '0')} elapsed`;
  $('dispatch-countdown').textContent = displayed.nextDispatchAt ? `In ${Math.max(0, Math.ceil((displayed.nextDispatchAt - Date.now()) / 1000))}s` : displayed.upcomingCount ? 'Next batch' : 'Queue clear';
}

function renderLedger() {
  if (!displayed) return;
  const rows = [...displayed.active, ...displayed.recent];
  const query = $('search').value.trim().toLowerCase();
  const matches = rows.filter(row => (!query || row.id.toLowerCase().includes(query) || row.customerName?.toLowerCase().includes(query)) &&
    (filter === 'all' || filter === 'attention' && ['review', 'block_and_review', 'decline'].includes(row.outcome) || filter === 'problems' && row.stage === 'error'));
  $('visible-count').textContent = matches.length;
  if (!pinned) selectedId = displayed.recent[0]?.id || displayed.active[0]?.id;
  const selected = rows.find(row => row.id === selectedId);
  if (selected) selectedSnapshot = selected;
  const markup = matches.length ? matches.map(row => {
    const active = row.stage !== 'completed' && row.stage !== 'error';
    const hits = row.ruleHits || 0;
    return `<tr class="transaction-row${row.id === selectedId ? ' chosen' : ''}" data-id="${escape(row.id)}" data-hover-id="${escape(row.id)}" data-hover-kind="received"><td><button class="tx-select customer-name" type="button">${escape(row.customerName || 'Customer')}</button><span class="tx-subtitle">${escape(short(row.id))}</span><span class="tx-subtitle">${escape(row.leg || row.role || 'record')}${row.state ? ` · ${escape(row.state)}` : ''}</span></td><td>${escape(money(row))}</td><td>${badge(active ? row.stage : row.outcome, active)}</td><td class="${hits ? 'hit-count' : 'muted'}">${active ? '—' : hits}</td><td class="latency">${row.latencyMs == null ? '—' : duration(row.latencyMs)}${row.httpStatus === 202 ? '<span class="tx-subtitle">async</span>' : ''}</td></tr>`;
  }).join('') : '<tr><td colspan="5" class="empty">No transactions match this view.</td></tr>';
  if (markup !== ledgerMarkup) {
    ledgerMarkup = markup;
    $('transaction-rows').innerHTML = markup;
  }
  $('ledger-caption').textContent = pinned ? 'Inspecting a selected transaction. Click it again to follow the latest.' : `${displayed.activeCount} active · ${displayed.recent.length} recent · following latest response`;
  renderDetail(selectedSnapshot);
}

function renderDetail(row) {
  if (!row) return;
  const key = JSON.stringify(row);
  if (key === detailKey) return;
  detailKey = key;
  const open = document.querySelector('.response-json')?.open;
  const active = row.stage !== 'completed' && row.stage !== 'error';
  const responses = (row.responses || []).map(result => {
    if (!result.triggered) return `<div class="scenario-result"><h3 class="scenario-name">${escape(result.scenario)}</h3><span class="no-hits">Scenario trigger did not match.</span></div>`;
    const hits = result.rules.filter(rule => ['hit', 'skipped', 'snoozed'].includes(rule.outcome));
    return `<div class="scenario-result"><h3 class="scenario-name">${escape(result.scenario)}</h3><div class="scenario-result-head">${badge(result.outcome)}<span class="score">Score <strong>${escape(result.score)}</strong></span></div>${hits.length ? `<ul class="rule-list">${hits.map(rule => `<li title="${escape(rule.reason || rule.outcome)}"><span class="rule-result ${rule.outcome}">${rule.outcome === 'hit' ? '+' : '∼'}</span><span>${escape(rule.name)}${rule.outcome !== 'hit' ? ` <span class="muted">(${escape(rule.outcome)})</span>` : ''}</span>${rule.outcome === 'hit' ? `<span class="rule-modifier">${rule.modifier >= 0 ? '+' : ''}${escape(rule.modifier)}</span>` : ''}</li>`).join('')}</ul>` : '<p class="no-hits">No rules matched.</p>'}</div>`;
  }).join('');
  $('response-content').innerHTML = `<div class="inspector-detail"><strong class="customer-name">${escape(row.customerName || "Customer")}</strong><div class="detail-id">${escape(row.id)}</div>${badge(active ? row.stage : row.outcome, active)}<dl class="detail-meta"><div><dt>Amount</dt><dd>${row.amount == null ? 'Not applicable' : escape(money(row))}</dd></div><div><dt>Evaluation time</dt><dd>${row.latencyMs == null ? 'In progress' : duration(row.latencyMs)}</dd></div><div><dt>HTTP admission</dt><dd>${row.httpStatus || 'Pending'}${row.httpStatus === 202 ? ' · async' : ''}</dd></div><div><dt>Transaction state</dt><dd>${escape(row.state || '—')}</dd></div></dl>${row.executionId ? `<p class="detail-id">Async execution<br>${escape(row.executionId)}</p>` : ''}${row.assertion ? `<p class="no-hits">Fixture assertion: ${escape(row.assertion)}</p>` : ''}${row.error ? `<div class="error-detail">${escape(row.error)}${row.outcome === 'unknown' ? '<br>Some effects may already be saved. Reconcile before retrying.' : ''}</div>` : ''}${active ? '<p class="no-hits">The request is in flight. Outcomes appear only after evaluation completes.</p>' : responses}${row.responseTruncated ? '<p class="no-hits">Showing up to 12 scenarios and 40 rule results. Totals include the full response.</p>' : ''}<details class="response-json"${open ? ' open' : ''}><summary>Inspect response summary</summary><pre>${escape(JSON.stringify({ transaction: row.id, http_status: row.httpStatus ?? null, async_execution_id: row.executionId ?? null, latency_ms: row.latencyMs ?? null, scenarios: row.responses ?? [], truncated: row.responseTruncated ?? false, error: row.error ?? null }, null, 2))}</pre></details></div>`;
}

$('transaction-rows').addEventListener('click', event => {
  const row = event.target.closest('[data-id]');
  if (!row) return;
  pinned = !(pinned && selectedId === row.dataset.id);
  selectedId = row.dataset.id;
  renderLedger();
});
document.querySelectorAll('[data-filter]').forEach(button => button.addEventListener('click', () => {
  filter = button.dataset.filter;
  document.querySelectorAll('[data-filter]').forEach(tab => { tab.classList.toggle('active', tab === button); tab.setAttribute('aria-pressed', String(tab === button)); });
  renderLedger();
}));
$('search').addEventListener('input', renderLedger);
$('freeze').addEventListener('click', () => {
  frozen = !frozen;
  document.body.classList.toggle('frozen', frozen);
  $('freeze').setAttribute('aria-pressed', String(frozen));
  $('freeze').querySelector('span').textContent = frozen ? 'Resume view' : 'Freeze view';
  $('freeze').title = frozen ? 'The simulation continues while the view is frozen.' : 'Freeze the display without stopping the simulation.';
  if (frozen) $('run-message').textContent = 'View frozen. The simulation continues in the background.';
  else if (latest) render(latest);
});

async function api(endpoint, body) {
  const response = await fetch(endpoint, body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  const result = await response.json();
  if (!response.ok) throw new Error(result.error || `HTTP ${response.status}`);
  return result;
}

let stream;
async function connect() {
  try {
    const token = location.hash.slice(1);
    if (/^[a-f0-9]{64}$/.test(token)) {
      await api('/api/session', { token });
      history.replaceState(null, '', location.pathname + location.search);
    }
    await api('/api/snapshot');
    stream = new EventSource('/api/events');
    stream.onmessage = event => {
      try { latest = JSON.parse(event.data); } catch { return; }
      lastReceived = Date.now();
      connected = true;
      updateConnection();
      if (!frozen) render(latest);
    };
    stream.onerror = () => { connected = false; updateConnection(); };
  } catch (error) { $('run-message').textContent = error.message; $('connection').textContent = 'Connection required'; }
}
setInterval(() => { updateConnection(); renderClock(); }, 1000);
window.addEventListener('pagehide', () => stream?.close());
connect();

let hoverTimer, hideTimer, hoverPinned = false, hoverRequest = 0;
const detailCache = new Map();
function hideHover() {
  if (hoverPinned) return;
  clearTimeout(hoverTimer);
  $('request-hover').hidden = true;
  hoverRequest++;
}
async function showHover(element, pin = false) {
  if (hoverPinned && !pin) return;
  clearTimeout(hideTimer);
  hoverPinned = pin;
  const request = ++hoverRequest;
  const id = element.dataset.hoverId;
  const kind = element.dataset.hoverKind;
  const row = [...(displayed?.active || []), ...(displayed?.recent || [])].find(item => item.id === id);
  const key = `${id}:${row?.stage || 'queued'}:${row?.completedAt || ''}`;
  const popover = $('request-hover');
  const rect = element.getBoundingClientRect();
  const width = Math.min(600, innerWidth - 24);
  popover.style.width = `${width}px`;
  popover.style.left = `${Math.max(12, Math.min(innerWidth - width - 12, rect.right + 10))}px`;
  popover.style.top = `${Math.max(12, Math.min(innerHeight - Math.min(innerHeight * .78, 750) - 12, rect.top))}px`;
  popover.hidden = false;
  popover.innerHTML = '<p class="field-hint">Loading request details…</p>';
  try {
    let detail = detailCache.get(key);
    if (!detail) {
      detail = await api(`/api/transactions/${encodeURIComponent(id)}`);
      detailCache.set(key, detail);
      if (detailCache.size > 30) detailCache.delete(detailCache.keys().next().value);
    }
    if (request !== hoverRequest) return;
    const sections = kind === 'queued' ? [['Queued request', detail.queuedRequest]] : [
      ['Ingestion request · record in batch', detail.ingestionRequest], ['Ingestion response', detail.ingestionResponse],
      ['Evaluation request', detail.evaluationRequest], ['Evaluation response', detail.evaluationResponse],
    ];
    popover.innerHTML = `<div class="hover-heading"><div><span class="eyebrow">${kind === 'queued' ? 'ON THE WAY' : 'RECEIVED TRAFFIC'}</span><h3>${escape(id)}</h3></div><button id="close-hover" type="button" aria-label="Close request details">×</button></div>${sections.filter(([, value]) => value).map(([label, value]) => `<details open><summary>${escape(label)}${value.truncated ? ' · truncated at 64 KiB' : ''}</summary><pre>${escape(value.json)}</pre></details>`).join('') || '<p class="field-hint">The request has not been submitted yet.</p>'}<p class="field-hint">${pin ? 'Pinned for inspection.' : 'Click the request to pin these details.'}</p>`;
  } catch (error) {
    if (request === hoverRequest) popover.innerHTML = `<p class="form-error">${escape(error.message)}</p><button id="close-hover" type="button">Close</button>`;
  }
}
document.addEventListener('pointerover', event => {
  const row = event.target.closest('[data-hover-id]');
  if (!row || row.contains(event.relatedTarget)) return;
  clearTimeout(hoverTimer);
  hoverTimer = setTimeout(() => showHover(row), 180);
});
document.addEventListener('pointerout', event => {
  const row = event.target.closest('[data-hover-id]');
  if (!row || row.contains(event.relatedTarget)) return;
  clearTimeout(hoverTimer);
  hideTimer = setTimeout(hideHover, 220);
});
document.addEventListener('focusin', event => { const row = event.target.closest('[data-hover-id]'); if (row) showHover(row); });
document.addEventListener('click', event => {
  if (event.target.closest('#close-hover')) { hoverPinned = false; hideHover(); return; }
  const row = event.target.closest('[data-hover-id]');
  if (row) showHover(row, true);
});
$('request-hover').addEventListener('pointerenter', () => clearTimeout(hideTimer));
$('request-hover').addEventListener('pointerleave', () => { hideTimer = setTimeout(hideHover, 220); });
document.addEventListener('keydown', event => { if (event.key === 'Escape') { hoverPinned = false; hideHover(); } });

let labCatalog, labPlan, labDirty = true, labLoading = false, labRecordMarkup = '', labResultMarkup = '';
const expectations = {};
function labError(message) { $('lab-error').textContent = message; $('lab-error').hidden = !message; }
function renderRuleChoices() {
  const scenario = labCatalog?.find(item => item.name === $('lab-scenario').value);
  if (!scenario) return;
  for (const key of Object.keys(expectations)) delete expectations[key];
  for (const rule of scenario.rules) expectations[rule.id] = 'ignore';
  $('lab-rules').innerHTML = scenario.rules.map(rule => `<div class="lab-rule"><div><strong>${escape(rule.name)}</strong><span class="rule-weight">+${rule.score}</span></div><p>${escape(rule.description)}</p><div class="rule-options" role="group" aria-label="${escape(rule.name)}">${[['hit', 'Match'], ['no_hit', 'Don’t match'], ['ignore', 'Any']].map(([value, label]) => `<button type="button" data-rule="${escape(rule.id)}" data-expectation="${value}" aria-pressed="${value === 'ignore'}" class="${value === 'ignore' ? 'chosen' : ''}">${label}</button>`).join('')}</div></div>`).join('');
  labDirty = true;
  $('lab-submit').disabled = true;
}

function renderLab(state) {
  if (!labCatalog) {
    labCatalog = state.scenarios;
    $('lab-scenario').innerHTML = labCatalog.map(scenario => `<option>${escape(scenario.name)}</option>`).join('');
    if (state.plan) $('lab-scenario').value = state.plan.scenario;
    renderRuleChoices();
  }
  const busy = ['waiting', 'submitting', 'evaluating'].includes(state.plan?.status);
  $('lab-generate').disabled = !state.ready || busy || labLoading;
  $('lab-scenario').disabled = busy;
  for (const button of document.querySelectorAll('[data-expectation]')) button.disabled = busy;
  if (state.plan && state.plan.id !== labPlan?.id && !labLoading) {
    labLoading = true;
    api('/api/lab').then(full => {
      labPlan = full.plan;
      $('lab-scenario').value = labPlan.scenario;
      renderRuleChoices();
      for (const rule of labPlan.rules) {
        expectations[rule.id] = rule.expected;
        for (const button of document.querySelectorAll('[data-rule]')) if (button.dataset.rule === rule.id) {
          const selected = button.dataset.expectation === rule.expected;
          button.classList.toggle('chosen', selected);
          button.setAttribute('aria-pressed', String(selected));
        }
      }
      labDirty = false;
      renderLab(full);
    }).catch(error => labError(error.message)).finally(() => { labLoading = false; });
    return;
  }
  if (!state.plan || !labPlan) return;
  const records = state.plan.records.map((record, i) => ({ ...labPlan.records[i], ...record }));
  labPlan = { ...labPlan, ...state.plan, records };
  const people = new Map(labPlan.records.filter(record => record.table === 'customers').map(record => [record.data.object_id, record.data.display_name]));
  const recordName = record => record.data.display_name || record.data.account_holder_name || people.get(record.data.user_id) || '';
  $('lab-record-count').textContent = labPlan.records.length;
  $('lab-plan-status').textContent = labDirty && labPlan.status === 'ready' ? 'Regenerate for new choices' : labPlan.status;
  $('lab-prediction').hidden = false;
  $('lab-prediction').innerHTML = `<div><span class="field-hint">Expected from generated data</span><strong>${escape(names[labPlan.predictedOutcome] || labPlan.predictedOutcome)}</strong></div><div><span class="field-hint">Predicted score</span><strong>${labPlan.predictedScore}</strong></div><div><span class="field-hint">Aggregation history</span><strong>${labPlan.records.filter(record => record.role === 'history').length} records</strong></div>`;
  const markup = labPlan.records.map((record, i) => `<details class="lab-record ${record.status}" data-record="${escape(record.data.object_id)}"><summary><span class="record-position">${i + 1}</span><span><strong>${escape(record.table)} <span class="record-role ${record.role}">${escape(record.role)}</span></strong><span class="record-person">${escape(recordName(record))}</span><code>${escape(record.data.object_id)}</code></span><span class="record-status">${escape(record.status)}</span></summary><pre>${escape(JSON.stringify(record.data, null, 2))}</pre>${record.response ? `<div class="record-response">Response: ${escape(JSON.stringify(record.response))}</div>` : ''}${record.error ? `<p class="form-error">${escape(record.error)}</p>` : ''}</details>`).join('');
  if (markup !== labRecordMarkup) {
    const opened = new Set([...document.querySelectorAll('.lab-record[open]')].map(element => element.dataset.record));
    labRecordMarkup = markup;
    $('lab-records').innerHTML = markup;
    for (const element of document.querySelectorAll('.lab-record')) element.open = opened.has(element.dataset.record) || (!opened.size && element.querySelector('.record-role.target'));
  }
  $('lab-submit').disabled = busy || labDirty || labPlan.status !== 'ready';
  const remaining = labPlan.nextSubmitAt ? Math.max(0, Math.ceil((labPlan.nextSubmitAt - Date.now()) / 1000)) : 0;
  $('lab-progress').textContent = labPlan.error || (busy ? `${labPlan.submittedRecords}/${labPlan.records.length} accepted · ${remaining ? `next submission in ${remaining}s` : labPlan.status}` : labDirty ? 'Rule choices changed. Generate a fresh dataset before submitting.' : labPlan.status === 'ready' ? 'Supporting records and history are submitted first. The target request is submitted and evaluated last.' : `${labPlan.submittedRecords}/${labPlan.records.length} records accepted · ${labPlan.status}`);
  if (labPlan.checks) {
    $('lab-results-panel').hidden = false;
    $('lab-result-status').textContent = labPlan.status === 'completed' ? 'Expectations matched' : 'Expectation mismatch';
    const results = `<table><thead><tr><th>Rule</th><th>Requested</th><th>Engine returned</th><th>Check</th></tr></thead><tbody>${labPlan.checks.map(check => `<tr><td>${escape(check.name)}</td><td>${escape(check.expected === 'ignore' ? 'Any' : check.expected)}</td><td>${escape(check.actual)}</td><td class="${check.passed === false ? 'form-error' : 'mint'}">${check.passed == null ? 'Not constrained' : check.passed ? 'Passed' : 'Mismatch'}</td></tr>`).join('')}</tbody></table>`;
    if (results !== labResultMarkup) { labResultMarkup = results; $('lab-results').innerHTML = results; }
  } else $('lab-results-panel').hidden = true;
}
$('lab-scenario').addEventListener('change', () => { renderRuleChoices(); labError(''); });
$('lab-rules').addEventListener('click', event => {
  const button = event.target.closest('[data-expectation]');
  if (!button) return;
  expectations[button.dataset.rule] = button.dataset.expectation;
  for (const choice of button.parentElement.querySelectorAll('button')) { choice.classList.toggle('chosen', choice === button); choice.setAttribute('aria-pressed', String(choice === button)); }
  labDirty = true;
  $('lab-submit').disabled = true;
});
$('lab-generate').addEventListener('click', async () => {
  labError('');
  labLoading = true;
  $('lab-generate').disabled = true;
  try {
    const state = await api('/api/lab/generate', { scenario: $('lab-scenario').value, expectations });
    labPlan = state.plan;
    labDirty = false;
    renderLab(state);
  } catch (error) { labError(error.message); }
  finally { labLoading = false; $('lab-generate').disabled = false; }
});
$('lab-submit').addEventListener('click', async () => {
  if (!labPlan || labDirty || !$('lab-initial-delay').reportValidity() || !$('lab-interval').reportValidity()) return;
  $('lab-submit').disabled = true;
  labError('');
  try {
    const state = await api('/api/lab/submit', { planId: labPlan.id, initialDelayMs: Number($('lab-initial-delay').value) * 1000, intervalMs: Number($('lab-interval').value) * 1000 });
    renderLab(state);
  } catch (error) { labError(error.message); }
});
