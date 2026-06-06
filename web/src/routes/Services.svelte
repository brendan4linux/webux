<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface Unit {
    name: string;
    description: string;
    load_state: string;
    active_state: string;
    sub_state: string;
    unit_type: string;
    enabled: string;
    following: string;
  }

  let units: Unit[] = $state([]);
  let initSystem = $state('');
  let loading = $state(true);
  let error = $state('');
  let filterText = $state('');
  let filterType = $state('service');
  let filterState = $state('all');
  let actionPending = $state<string | null>(null);
  let logUnit = $state<string | null>(null);
  let logs = $state<{timestamp:string, message:string}[]>([]);
  let logsLoading = $state(false);

  async function load() {
    loading = true; error = '';
    try {
      const res = await api.get<any>(`/api/services?type=${filterType}`);
      units = res.units ?? [];
      initSystem = res.init_system ?? '';
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function doAction(name: string, action: string) {
    actionPending = name + ':' + action;
    try {
      await api.post(`/api/services/${encodeURIComponent(name)}/action`, { action });
      await load();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  async function showLogs(name: string) {
    logUnit = name; logs = []; logsLoading = true;
    try {
      const res = await api.get<any>(`/api/services/${encodeURIComponent(name)}/logs?lines=150`);
      logs = res.entries ?? [];
    } catch(e: any) { error = e.message; }
    finally { logsLoading = false; }
  }

  let filtered = $derived(units.filter(u => {
    if (filterState !== 'all' && u.active_state !== filterState) return false;
    if (filterText) {
      const t = filterText.toLowerCase();
      return u.name.toLowerCase().includes(t) || u.description?.toLowerCase().includes(t);
    }
    return true;
  }));

  function activeClass(state: string) {
    if (state === 'active') return 'badge-green';
    if (state === 'failed') return 'badge-red';
    if (state === 'activating') return 'badge-yellow';
    return 'badge-gray';
  }

  function enabledClass(enabled: string) {
    if (enabled === 'enabled') return 'badge-blue';
    if (enabled === 'disabled') return 'badge-gray';
    if (enabled === 'masked') return 'badge-red';
    return 'badge-gray';
  }

  onMount(load);
</script>

<div class="services-page">
  <div class="page-header">
    <div>
      <h1>Services</h1>
      <p class="subtitle">
        {initSystem ? initSystem + ' — ' : ''}
        {filtered.length} of {units.length} units
      </p>
    </div>
    <div class="actions">
      <button class="btn" onclick={load} disabled={loading}>
        {loading ? 'Loading…' : '⟳ Refresh'}
      </button>
    </div>
  </div>

  {#if error}
    <div class="alert alert-error" style="margin-bottom:1rem">{error}</div>
  {/if}

  <!-- Filters -->
  <div class="filter-bar">
    <input class="search-input" style="max-width:280px" type="search"
      placeholder="Filter by name or description…" bind:value={filterText} />

    <div class="seg-control">
      {#each ['service','socket','timer','all'] as t}
        <button class="seg-btn" class:active={filterType === (t === 'all' ? '' : t)}
          onclick={() => { filterType = t === 'all' ? '' : t; load(); }}>
          {t}
        </button>
      {/each}
    </div>

    <div class="seg-control">
      {#each ['all','active','inactive','failed'] as s}
        <button class="seg-btn" class:active={filterState === s}
          onclick={() => filterState = s}>
          {s}
        </button>
      {/each}
    </div>
  </div>

  <!-- Table -->
  <div class="card" style="padding:0;overflow-x:auto">
    <table class="data-table">
      <thead>
        <tr>
          <th>Unit</th>
          <th>State</th>
          <th>Sub-state</th>
          <th>Enabled</th>
          <th>Description</th>
          <th style="text-align:right">Actions</th>
        </tr>
      </thead>
      <tbody>
        {#if loading}
          {#each [1,2,3,4,5] as _}
            <tr>
              {#each [1,2,3,4,5,6] as _}
                <td><div class="skeleton" style="height:14px;width:80%"></div></td>
              {/each}
            </tr>
          {/each}
        {:else if filtered.length === 0}
          <tr><td colspan="6" style="text-align:center;padding:2rem;color:var(--text-tertiary)">
            No units match filter
          </td></tr>
        {:else}
          {#each filtered as u, i (u.name + ':' + i)}
            <tr class="unit-row" class:failed={u.active_state === 'failed'}>
              <td>
                <div class="unit-name mono">{u.name}</div>
              </td>
              <td>
                <span class="badge {activeClass(u.active_state)}">
                  {u.active_state}
                </span>
              </td>
              <td class="mono" style="font-size:0.72rem;color:var(--text-tertiary)">
                {u.sub_state}
              </td>
              <td>
                <span class="badge {enabledClass(u.enabled)}">{u.enabled || '—'}</span>
              </td>
              <td style="color:var(--text-secondary);font-size:0.8rem;max-width:280px">
                <span class="desc-text">{u.description || ''}</span>
              </td>
              <td>
                <div class="unit-actions">
                  {#if u.active_state !== 'active'}
                    <button class="btn btn-ghost action-btn"
                      disabled={actionPending !== null}
                      onclick={() => doAction(u.name, 'start')}
                      title="Start">▶</button>
                  {:else}
                    <button class="btn btn-ghost action-btn"
                      disabled={actionPending !== null}
                      onclick={() => doAction(u.name, 'stop')}
                      title="Stop">■</button>
                  {/if}
                  <button class="btn btn-ghost action-btn"
                    disabled={actionPending !== null}
                    onclick={() => doAction(u.name, 'restart')}
                    title="Restart">↺</button>
                  {#if u.enabled !== 'enabled'}
                    <button class="btn btn-ghost action-btn"
                      disabled={actionPending !== null}
                      onclick={() => doAction(u.name, 'enable')}
                      title="Enable at boot">☆</button>
                  {:else}
                    <button class="btn btn-ghost action-btn"
                      disabled={actionPending !== null}
                      onclick={() => doAction(u.name, 'disable')}
                      title="Disable at boot">★</button>
                  {/if}
                  <button class="btn btn-ghost action-btn"
                    onclick={() => showLogs(u.name)}
                    title="View logs">≡</button>
                </div>
              </td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>

  <!-- Log drawer -->
  {#if logUnit}
    <div class="log-drawer">
      <div class="log-drawer-header">
        <span class="mono">{logUnit}</span>
        <span style="color:var(--text-tertiary);font-size:0.75rem">— journal logs</span>
        <button class="btn btn-ghost" style="margin-left:auto"
          onclick={() => { logUnit = null; logs = []; }}>✕ Close</button>
      </div>
      <div class="log-body">
        {#if logsLoading}
          <div style="padding:1rem;color:var(--text-tertiary)">Loading logs…</div>
        {:else if logs.length === 0}
          <div style="padding:1rem;color:var(--text-tertiary)">No log entries found.</div>
        {:else}
          {#each logs as entry}
            <div class="log-line">
              {#if entry.timestamp}
                <span class="log-ts">{entry.timestamp}</span>
              {/if}
              <span class="log-msg">{entry.message}</span>
            </div>
          {/each}
        {/if}
      </div>
    </div>
  {/if}

  <CLIEchoPane context="services" />
</div>

<style>
.services-page { max-width: 1300px; padding-bottom: 220px; }

.filter-bar { display:flex; gap:0.75rem; align-items:center; margin-bottom:0.75rem; flex-wrap:wrap; }

.seg-control { display:flex; border:1px solid var(--border-default); border-radius:var(--r-md); overflow:hidden; }
.seg-btn { padding:0.3rem 0.6rem; background:var(--bg-raised); border:none; border-right:1px solid var(--border-default); cursor:pointer; font-size:0.72rem; font-family:var(--font-mono); color:var(--text-secondary); }
.seg-btn:last-child { border-right:none; }
.seg-btn.active { background:var(--accent); color:var(--text-inverse); font-weight:600; }

.unit-row:hover { background:var(--bg-hover); }
.unit-row.failed td { background:rgba(224,82,82,0.04); }
.unit-name { font-size:0.8rem; font-weight:500; }

.desc-text { display:block; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; max-width:280px; }

.unit-actions { display:flex; gap:0.2rem; justify-content:flex-end; }
.action-btn { padding:0.25rem 0.4rem; font-size:0.8rem; min-width:28px; }

.log-drawer {
  margin-top:1rem;
  border:1px solid var(--border-default);
  border-radius:var(--r-lg);
  overflow:hidden;
}
.log-drawer-header {
  display:flex;
  align-items:center;
  gap:0.5rem;
  padding:0.5rem 0.875rem;
  background:var(--bg-raised);
  border-bottom:1px solid var(--border-subtle);
  font-size:0.82rem;
}
.log-body {
  max-height:300px;
  overflow-y:auto;
  padding:0.5rem;
  background:var(--bg-base);
}
.log-line {
  display:flex;
  gap:0.75rem;
  padding:0.1rem 0.25rem;
  font-family:var(--font-mono);
  font-size:0.72rem;
  line-height:1.5;
}
.log-line:hover { background:var(--bg-raised); }
.log-ts { color:var(--text-tertiary); flex-shrink:0; }
.log-msg { color:var(--text-secondary); word-break:break-all; }
</style>
