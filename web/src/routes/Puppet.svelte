<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface RunSummary {
    resources: { changed:number; failed:number; skipped:number; total:number; out_of_sync:number };
    events:    { failure:number; success:number; total:number };
    changes:   { total:number };
    time:      Record<string,number>;
  }
  interface AgentStatus {
    installed: boolean; version: string; cert_name: string;
    server: string; environment: string;
    enabled: boolean; disabled_msg: string;
    last_run_at: string; last_run_ago: string;
    run_summary: RunSummary;
    state_dir: string; conf_dir: string;
  }
  interface CatalogResource {
    type: string; title: string; tags: string[];
    parameters: Record<string,any>; file: string; line: number;
  }
  interface RunEvent {
    resource: string; status: string; message: string;
    property: string; old_value: string; new_value: string;
  }

  let status: AgentStatus | null = $state(null);
  let loading = $state(true);
  let error = $state('');
  let tab = $state<'overview'|'catalog'|'report'|'facts'>('overview');

  let catalog: CatalogResource[] = $state([]);
  let catalogLoading = $state(false);
  let catalogFilter = $state('');

  let reportEvents: RunEvent[] = $state([]);
  let reportLoading = $state(false);
  let reportFilter = $state('');

  let facts: Record<string,any> = $state({});
  let factsLoading = $state(false);
  let factsFilter = $state('');

  let running = $state(false);
  let runOutput: string[] = $state([]);
  let runNoop = $state(false);
  let disableMsg = $state('');
  let showDisable = $state(false);

  async function loadStatus() {
    loading = true; error = '';
    try {
      status = await api.get<AgentStatus>('/api/puppet/status');
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function loadCatalog() {
    catalogLoading = true;
    try {
      const res = await api.get<any>('/api/puppet/catalog');
      catalog = res.resources ?? [];
    } catch(e: any) { error = e.message; }
    finally { catalogLoading = false; }
  }

  async function loadReport() {
    reportLoading = true;
    try {
      const res = await api.get<any>('/api/puppet/report');
      reportEvents = res.events ?? [];
    } catch(e: any) { error = e.message; }
    finally { reportLoading = false; }
  }

  async function loadFacts() {
    factsLoading = true;
    try {
      facts = await api.get<Record<string,any>>('/api/puppet/facts');
    } catch(e: any) { error = e.message; }
    finally { factsLoading = false; }
  }

  async function runAgent() {
    running = true; runOutput = [];
    const es = new EventSource(`/api/puppet/run`);
    // Can't POST via EventSource — use fetch + reader
    es.close();

    try {
      const resp = await fetch('/api/puppet/run', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ noop: runNoop }),
      });
      const reader = resp.body!.getReader();
      const dec = new TextDecoder();
      let buf = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const lines = buf.split('\n');
        for (const line of lines.slice(0, -1)) {
          if (line.startsWith('data: ')) runOutput = [...runOutput, line.slice(6)];
        }
        buf = lines[lines.length - 1];
      }
    } catch(e: any) { runOutput = [...runOutput, '[error] ' + e.message]; }
    finally { running = false; await loadStatus(); }
  }

  async function toggleAgent() {
    if (!status) return;
    if (status.enabled) {
      showDisable = true;
    } else {
      await api.post('/api/puppet/enable', {});
      await loadStatus();
    }
  }

  async function confirmDisable() {
    await api.post('/api/puppet/disable', { message: disableMsg });
    showDisable = false; disableMsg = '';
    await loadStatus();
  }

  $effect(() => {
    if (tab === 'catalog' && catalog.length === 0) loadCatalog();
    if (tab === 'report' && reportEvents.length === 0) loadReport();
    if (tab === 'facts' && Object.keys(facts).length === 0) loadFacts();
  });

  function statusClass(s: string) {
    if (s === 'changed') return 'badge-blue';
    if (s === 'failed') return 'badge-red';
    if (s === 'skipped') return 'badge-yellow';
    return 'badge-green';
  }

  let filteredCatalog = $derived(catalog.filter(r =>
    !catalogFilter ||
    r.type.toLowerCase().includes(catalogFilter.toLowerCase()) ||
    r.title.toLowerCase().includes(catalogFilter.toLowerCase())
  ));

  let filteredEvents = $derived(reportEvents.filter(r =>
    !reportFilter ||
    r.resource.toLowerCase().includes(reportFilter.toLowerCase()) ||
    r.status.toLowerCase().includes(reportFilter.toLowerCase())
  ));

  let filteredFacts = $derived(Object.entries(facts).filter(([k, v]) =>
    !factsFilter ||
    k.toLowerCase().includes(factsFilter.toLowerCase()) ||
    String(v).toLowerCase().includes(factsFilter.toLowerCase())
  ));

  onMount(loadStatus);
</script>

<div class="puppet-page">
  <div class="page-header">
    <div>
      <h1>Puppet</h1>
      {#if status?.installed}
        <p class="subtitle">v{status.version} · {status.cert_name}</p>
      {/if}
    </div>
    <div class="actions">
      <button class="btn" onclick={loadStatus} disabled={loading}>⟳ Refresh</button>
    </div>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  {#if loading}
    <div class="card skeleton" style="height:120px"></div>
  {:else if !status?.installed}
    <div class="card" style="text-align:center;padding:3rem;color:var(--text-tertiary)">
      <div style="font-size:2rem;margin-bottom:1rem">◆</div>
      <h2>Puppet agent not detected</h2>
      <p style="margin-top:0.5rem;font-size:0.85rem">Install the Puppet agent to manage this node.</p>
      <code class="mono" style="display:inline-block;margin-top:1rem;padding:0.4rem 0.75rem;background:var(--bg-raised);border-radius:var(--r-md);font-size:0.8rem">
        curl -L https://puppet.com/misc/puppet-enterprise-installer | bash
      </code>
    </div>
  {:else}
    <!-- Status banner -->
    <div class="card status-banner" class:disabled={!status.enabled}>
      <div class="status-left">
        <span class="dot {status.enabled ? 'dot-green' : 'dot-red'}"></span>
        <span class="status-label">{status.enabled ? 'Enabled' : 'Disabled'}</span>
        {#if status.disabled_msg}
          <span style="font-size:0.78rem;color:var(--text-tertiary)">— {status.disabled_msg}</span>
        {/if}
      </div>
      <div class="status-meta">
        {#if status.last_run_ago}
          <span>Last run: <strong>{status.last_run_ago}</strong></span>
        {/if}
        <span class="badge badge-gray">{status.environment}</span>
        <span style="font-size:0.75rem;color:var(--text-tertiary)">{status.server}</span>
      </div>
      <div class="status-actions">
        <button class="btn {status.enabled ? '' : 'btn-primary'}" onclick={toggleAgent}>
          {status.enabled ? '⬛ Disable' : '▶ Enable'}
        </button>
        <label class="toggle-label">
          <input type="checkbox" bind:checked={runNoop} />
          <span>Noop</span>
        </label>
        <button class="btn btn-primary" disabled={running} onclick={runAgent}>
          {running ? '⟳ Running…' : '▶ Run now'}
        </button>
      </div>
    </div>

    <!-- Run output -->
    {#if runOutput.length > 0}
      <div class="card run-output">
        <div class="run-header">
          <span style="font-size:0.72rem;color:var(--text-tertiary);text-transform:uppercase;letter-spacing:0.06em">
            Run output {runNoop ? '(noop)' : ''}
          </span>
        </div>
        {#each runOutput as line}
          <div class="run-line mono"
            class:line-changed={line.includes('Notice:') || line.includes('changed')}
            class:line-error={line.includes('Error:') || line.includes('failed')}
            class:line-warn={line.includes('Warning:')}
          >{line}</div>
        {/each}
      </div>
    {/if}

    <!-- Summary cards -->
    {#if status.run_summary}
      {@const rs = status.run_summary.resources}
      <div class="summary-grid">
        <div class="summary-card">
          <div class="summary-num">{rs.total}</div>
          <div class="summary-lbl">Resources</div>
        </div>
        <div class="summary-card" class:highlight-green={rs.changed > 0}>
          <div class="summary-num" style="color:{rs.changed>0?'var(--accent)':'inherit'}">{rs.changed}</div>
          <div class="summary-lbl">Changed</div>
        </div>
        <div class="summary-card" class:highlight-red={rs.failed > 0}>
          <div class="summary-num" style="color:{rs.failed>0?'var(--red)':'inherit'}">{rs.failed}</div>
          <div class="summary-lbl">Failed</div>
        </div>
        <div class="summary-card">
          <div class="summary-num">{rs.skipped}</div>
          <div class="summary-lbl">Skipped</div>
        </div>
        <div class="summary-card">
          <div class="summary-num">{rs.out_of_sync}</div>
          <div class="summary-lbl">Out of sync</div>
        </div>
      </div>
    {/if}

    <!-- Tabs -->
    <div class="tab-bar">
      {#each ['overview','catalog','report','facts'] as t}
        <button class="tab-btn" class:active={tab===t} onclick={() => tab = t as any}>
          {t.charAt(0).toUpperCase() + t.slice(1)}
          {#if t === 'catalog' && catalog.length}{' '}({catalog.length}){/if}
          {#if t === 'report' && reportEvents.length}{' '}({reportEvents.length}){/if}
        </button>
      {/each}
    </div>

    {#if tab === 'overview'}
      <div class="card">
        <table class="detail-table">
          <tbody>
            <tr><td>Certificate name</td><td class="mono">{status.cert_name}</td></tr>
            <tr><td>Server</td><td class="mono">{status.server}</td></tr>
            <tr><td>Environment</td><td class="mono">{status.environment}</td></tr>
            <tr><td>Version</td><td class="mono">{status.version}</td></tr>
            <tr><td>State dir</td><td class="mono">{status.state_dir}</td></tr>
            <tr><td>Config dir</td><td class="mono">{status.conf_dir}</td></tr>
            {#if status.last_run_at}
              <tr><td>Last run</td><td class="mono">{new Date(status.last_run_at).toLocaleString()} ({status.last_run_ago})</td></tr>
            {/if}
          </tbody>
        </table>
      </div>

    {:else if tab === 'catalog'}
      <div style="margin-bottom:0.75rem">
        <input class="search-input" style="max-width:320px" type="search"
          placeholder="Filter by type or title…" bind:value={catalogFilter} />
      </div>
      <div class="card" style="padding:0">
        <table class="data-table">
          <thead><tr><th>Type</th><th>Title</th><th>File</th></tr></thead>
          <tbody>
            {#if catalogLoading}
              {#each [1,2,3] as _}<tr>{#each [1,2,3] as _}<td><div class="skeleton" style="height:13px;width:70%"></div></td>{/each}</tr>{/each}
            {:else if filteredCatalog.length === 0}
              <tr><td colspan="3" style="text-align:center;padding:2rem;color:var(--text-tertiary)">No resources found</td></tr>
            {:else}
              {#each filteredCatalog as res}
                <tr>
                  <td><span class="badge badge-purple">{res.type}</span></td>
                  <td class="mono" style="font-size:0.82rem">{res.title}</td>
                  <td class="mono" style="font-size:0.72rem;color:var(--text-tertiary)">
                    {res.file ? res.file + ':' + res.line : '—'}
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

    {:else if tab === 'report'}
      <div style="margin-bottom:0.75rem">
        <input class="search-input" style="max-width:320px" type="search"
          placeholder="Filter by resource or status…" bind:value={reportFilter} />
      </div>
      <div class="card" style="padding:0">
        <table class="data-table">
          <thead><tr><th>Resource</th><th>Status</th><th>Property</th><th>Message</th></tr></thead>
          <tbody>
            {#if reportLoading}
              {#each [1,2,3] as _}<tr>{#each [1,2,3,4] as _}<td><div class="skeleton" style="height:13px;width:70%"></div></td>{/each}</tr>{/each}
            {:else if filteredEvents.length === 0}
              <tr><td colspan="4" style="text-align:center;padding:2rem;color:var(--text-tertiary)">No events — run the agent first</td></tr>
            {:else}
              {#each filteredEvents as evt}
                <tr>
                  <td class="mono" style="font-size:0.78rem">{evt.resource}</td>
                  <td><span class="badge {statusClass(evt.status)}">{evt.status}</span></td>
                  <td class="mono" style="font-size:0.75rem;color:var(--text-secondary)">{evt.property || '—'}</td>
                  <td style="font-size:0.78rem;color:var(--text-secondary)">{evt.message || '—'}</td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

    {:else if tab === 'facts'}
      <div style="margin-bottom:0.75rem">
        <input class="search-input" style="max-width:320px" type="search"
          placeholder="Filter facts…" bind:value={factsFilter} />
      </div>
      <div class="card" style="padding:0">
        <table class="data-table">
          <thead><tr><th>Fact</th><th>Value</th></tr></thead>
          <tbody>
            {#if factsLoading}
              {#each [1,2,3,4,5] as _}<tr><td><div class="skeleton" style="height:13px;width:40%"></div></td><td><div class="skeleton" style="height:13px;width:70%"></div></td></tr>{/each}
            {:else if filteredFacts.length === 0}
              <tr><td colspan="2" style="text-align:center;padding:2rem;color:var(--text-tertiary)">No facts — is facter installed?</td></tr>
            {:else}
              {#each filteredFacts as [key, val]}
                <tr>
                  <td class="mono" style="color:var(--accent);font-size:0.8rem">{key}</td>
                  <td class="mono" style="font-size:0.8rem;word-break:break-all">
                    {typeof val === 'object' ? JSON.stringify(val) : String(val)}
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}

  <!-- Disable modal -->
  {#if showDisable}
    <div class="modal-overlay" role="button" tabindex="0"
      onkeydown={(e) => e.key === 'Escape' && (showDisable = false)}
      onclick={() => showDisable = false}>
      <div class="modal" role="dialog" tabindex="-1" onclick={(e) => e.stopPropagation()}>
        <div class="modal-header">
          <h2>Disable Puppet agent</h2>
          <button class="btn btn-ghost" onclick={() => showDisable = false}>✕</button>
        </div>
        <div class="modal-body">
          <label for="disable-msg" style="font-size:0.75rem;color:var(--text-secondary)">Reason (optional)</label>
          <input id="disable-msg" class="search-input" bind:value={disableMsg}
            placeholder="Maintenance window, debugging…" />
        </div>
        <div class="modal-footer">
          <button class="btn" onclick={() => showDisable = false}>Cancel</button>
          <button class="btn btn-danger" onclick={confirmDisable}>Disable</button>
        </div>
      </div>
    </div>
  {/if}

  <CLIEchoPane context="puppet" />
</div>

<style>
.puppet-page { max-width:1100px; padding-bottom:220px; }

.status-banner { display:flex; align-items:center; flex-wrap:wrap; gap:1rem; padding:0.875rem 1rem; margin-bottom:0.75rem; }
.status-banner.disabled { border-color:var(--red); }
.status-left { display:flex; align-items:center; gap:0.5rem; }
.status-label { font-weight:600; font-size:0.9rem; }
.status-meta { display:flex; align-items:center; gap:0.75rem; font-size:0.82rem; color:var(--text-secondary); flex:1; }
.status-actions { display:flex; align-items:center; gap:0.5rem; margin-left:auto; }
.toggle-label { display:flex; align-items:center; gap:0.3rem; font-size:0.78rem; color:var(--text-secondary); cursor:pointer; }

.summary-grid { display:grid; grid-template-columns:repeat(5,1fr); gap:0.5rem; margin-bottom:0.75rem; }
.summary-card { background:var(--bg-panel); border:1px solid var(--border-subtle); border-radius:var(--r-md); padding:0.75rem; text-align:center; }
.summary-num { font-size:1.5rem; font-weight:700; font-family:var(--font-mono); }
.summary-lbl { font-size:0.68rem; color:var(--text-tertiary); text-transform:uppercase; letter-spacing:0.06em; margin-top:0.2rem; }

.tab-bar { display:flex; border-bottom:1px solid var(--border-subtle); margin-bottom:0.75rem; }
.tab-btn { padding:0.5rem 1rem; background:none; border:none; border-bottom:2px solid transparent; cursor:pointer; font-size:0.85rem; color:var(--text-secondary); margin-bottom:-1px; }
.tab-btn.active { color:var(--accent); border-bottom-color:var(--accent); font-weight:500; }

.run-output { padding:0; overflow:hidden; margin-bottom:0.75rem; }
.run-header { padding:0.4rem 0.75rem; background:var(--bg-raised); border-bottom:1px solid var(--border-subtle); }
.run-line { font-size:0.72rem; padding:0.1rem 0.75rem; line-height:1.5; color:var(--text-secondary); white-space:pre-wrap; word-break:break-all; }
.run-line.line-changed { color:var(--accent); }
.run-line.line-error { color:var(--red); }
.run-line.line-warn { color:var(--yellow); }

.detail-table { width:100%; font-size:0.85rem; border-collapse:collapse; }
.detail-table td { padding:0.45rem 0.75rem; border-bottom:1px solid var(--border-subtle); }
.detail-table tbody tr:last-child td { border-bottom:none; }
.detail-table td:first-child { color:var(--text-tertiary); width:160px; }

.btn-danger { background:var(--red-dim); border-color:var(--red); color:var(--red); }
.modal-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.6); display:flex; align-items:center; justify-content:center; z-index:500; }
.modal { background:var(--bg-panel); border:1px solid var(--border-default); border-radius:var(--r-lg); width:380px; max-width:95vw; }
.modal-header { display:flex; justify-content:space-between; align-items:center; padding:1rem; border-bottom:1px solid var(--border-subtle); }
.modal-header h2 { font-size:1rem; margin:0; }
.modal-body { padding:1rem; display:flex; flex-direction:column; gap:0.375rem; }
.modal-footer { display:flex; justify-content:flex-end; gap:0.5rem; padding:1rem; border-top:1px solid var(--border-subtle); }
</style>
