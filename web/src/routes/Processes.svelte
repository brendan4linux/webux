<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface Process {
    pid: number;
    ppid: number;
    name: string;
    cmdline: string;
    state: string;
    state_name: string;
    username: string;
    uid: number;
    cpu_percent: number;
    mem_rss_kb: number;
    mem_vsz_kb: number;
    threads: number;
    priority: number;
    nice: number;
    start_time: string;
    open_fds: number;
  }

  let procs: Process[] = $state([]);
  let loading = $state(true);
  let scanning = $state(false);
  let error = $state('');
  let filterText = $state('');
  let sortField = $state('cpu');
  let sortAsc = $state(false);
  let autoRefresh = $state(false);
  let refreshTimer: ReturnType<typeof setInterval> | null = null;
  let expandedPid = $state<number | null>(null);

  async function load() {
    if (scanning) return;
    scanning = true;
    if (procs.length === 0) loading = true;
    try {
      const res = await api.get<any>(`/api/processes?sort=${sortField}&order=${sortAsc ? 'asc' : 'desc'}`);
      procs = res.processes ?? [];
    } catch(e: any) { error = e.message; }
    finally { loading = false; scanning = false; }
  }

  function toggleSort(field: string) {
    if (sortField === field) sortAsc = !sortAsc;
    else { sortField = field; sortAsc = false; }
    load();
  }

  function sortIcon(field: string) {
    if (sortField !== field) return '';
    return sortAsc ? ' ↑' : ' ↓';
  }

  $effect(() => {
    if (autoRefresh) {
      refreshTimer = setInterval(load, 3000);
    } else {
      if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null; }
    }
  });

  let filtered = $derived(procs.filter(p => {
    if (!filterText) return true;
    const t = filterText.toLowerCase();
    return p.name.toLowerCase().includes(t)
      || String(p.pid).includes(t)
      || p.username.toLowerCase().includes(t)
      || p.cmdline.toLowerCase().includes(t);
  }));

  function fmtMem(kb: number): string {
    if (kb > 1024 * 1024) return (kb / 1024 / 1024).toFixed(1) + ' GB';
    if (kb > 1024) return (kb / 1024).toFixed(1) + ' MB';
    return kb + ' KB';
  }

  function stateClass(state: string): string {
    if (state === 'R') return 'badge-green';
    if (state === 'Z') return 'badge-red';
    if (state === 'D') return 'badge-yellow';
    if (state === 'T') return 'badge-yellow';
    return 'badge-gray';
  }

  function cpuColor(pct: number): string {
    if (pct > 50) return 'var(--red)';
    if (pct > 20) return 'var(--yellow)';
    if (pct > 2) return 'var(--accent)';
    return 'var(--text-secondary)';
  }

  onMount(load);
  onDestroy(() => { if (refreshTimer) clearInterval(refreshTimer); });
</script>

<div class="proc-page">
  <div class="page-header">
    <div>
      <h1>Processes</h1>
      <p class="subtitle">
        {filtered.length} of {procs.length} processes
        {scanning ? '— scanning /proc…' : ''}
      </p>
    </div>
    <div class="actions">
      <label class="auto-refresh">
        <input type="checkbox" bind:checked={autoRefresh} />
        <span>Auto-refresh (3s)</span>
      </label>
      <button class="btn" onclick={load} disabled={scanning}>
        {scanning ? 'Scanning…' : '⟳ Refresh'}
      </button>
    </div>
  </div>

  {#if error}
    <div class="alert alert-error" style="margin-bottom:1rem">{error}</div>
  {/if}

  <div class="filter-bar">
    <input class="search-input" style="max-width:320px" type="search"
      placeholder="Filter by name, PID, user, cmdline…" bind:value={filterText} />
    <span style="font-size:0.72rem;color:var(--text-tertiary);font-family:var(--font-mono)">
      {filtered.length} shown
    </span>
  </div>

  <div class="card" style="padding:0;overflow-x:auto">
    <table class="data-table proc-table">
      <thead>
        <tr>
          <th class="sortable" onclick={() => toggleSort('pid')}>PID{sortIcon('pid')}</th>
          <th class="sortable" onclick={() => toggleSort('name')}>Name{sortIcon('name')}</th>
          <th class="sortable" onclick={() => toggleSort('user')}>User{sortIcon('user')}</th>
          <th class="sortable" onclick={() => toggleSort('cpu')}>CPU%{sortIcon('cpu')}</th>
          <th class="sortable" onclick={() => toggleSort('mem')}>RSS{sortIcon('mem')}</th>
          <th>State</th>
          <th>Threads</th>
          <th>Nice</th>
          <th>Started</th>
        </tr>
      </thead>
      <tbody>
        {#if loading}
          {#each [1,2,3,4,5,6,7,8] as _}
            <tr>{#each [1,2,3,4,5,6,7,8,9] as _}
              <td><div class="skeleton" style="height:12px;width:75%"></div></td>
            {/each}</tr>
          {/each}
        {:else if filtered.length === 0}
          <tr><td colspan="9" style="text-align:center;padding:2rem;color:var(--text-tertiary)">
            No processes match filter
          </td></tr>
        {:else}
          {#each filtered as p (p.pid)}
            <tr class="proc-row" class:expanded={expandedPid === p.pid}
              onclick={() => expandedPid = expandedPid === p.pid ? null : p.pid}>
              <td class="mono" style="color:var(--text-tertiary)">{p.pid}</td>
              <td class="proc-name">
                <span class="mono" style="font-weight:500">{p.name}</span>
              </td>
              <td class="mono" style="font-size:0.72rem;color:var(--text-secondary)">{p.username}</td>
              <td>
                <span class="mono" style="font-weight:600;color:{cpuColor(p.cpu_percent)}">
                  {p.cpu_percent.toFixed(1)}%
                </span>
              </td>
              <td class="mono" style="font-size:0.75rem">{fmtMem(p.mem_rss_kb)}</td>
              <td><span class="badge {stateClass(p.state)}" title={p.state_name}>{p.state}</span></td>
              <td class="mono" style="font-size:0.72rem;color:var(--text-tertiary)">{p.threads}</td>
              <td class="mono" style="font-size:0.72rem;color:var(--text-tertiary)">{p.nice}</td>
              <td class="mono" style="font-size:0.72rem;color:var(--text-tertiary)">{p.start_time}</td>
            </tr>
            {#if expandedPid === p.pid}
              <tr class="proc-detail-row">
                <td colspan="9">
                  <div class="proc-detail">
                    <div class="detail-row">
                      <span class="detail-label">PPID</span>
                      <span class="mono">{p.ppid}</span>
                    </div>
                    <div class="detail-row">
                      <span class="detail-label">VSZ</span>
                      <span class="mono">{fmtMem(p.mem_vsz_kb)}</span>
                    </div>
                    <div class="detail-row">
                      <span class="detail-label">Open FDs</span>
                      <span class="mono">{p.open_fds}</span>
                    </div>
                    <div class="detail-row">
                      <span class="detail-label">Priority</span>
                      <span class="mono">{p.priority}</span>
                    </div>
                    <div class="detail-row" style="grid-column:1/-1">
                      <span class="detail-label">Cmdline</span>
                      <code class="cmdline-full">{p.cmdline || p.name}</code>
                    </div>
                  </div>
                </td>
              </tr>
            {/if}
          {/each}
        {/if}
      </tbody>
    </table>
  </div>

  <CLIEchoPane context="processes" />
</div>

<style>
.proc-page { max-width:1300px; padding-bottom:220px; }

.filter-bar { display:flex; gap:0.75rem; align-items:center; margin-bottom:0.75rem; }

.auto-refresh { display:flex; align-items:center; gap:0.375rem; font-size:0.78rem; color:var(--text-secondary); cursor:pointer; }

.proc-table { font-size:0.78rem; }
.sortable { cursor:pointer; user-select:none; }
.sortable:hover { color:var(--text-primary) !important; }

.proc-row { cursor:pointer; }
.proc-row:hover { background:var(--bg-hover); }
.proc-row.expanded { background:var(--accent-dim); }

.proc-name { max-width:180px; overflow:hidden; }

.proc-detail-row td { padding:0; }
.proc-detail {
  display:grid;
  grid-template-columns:repeat(4, 1fr);
  gap:0.5rem;
  padding:0.75rem 1rem;
  background:var(--bg-raised);
  border-top:1px solid var(--border-subtle);
}
.detail-row { display:flex; flex-direction:column; gap:2px; }
.detail-label { font-size:0.65rem; font-weight:500; color:var(--text-tertiary); text-transform:uppercase; letter-spacing:0.05em; }
.cmdline-full { font-size:0.72rem; color:var(--text-secondary); word-break:break-all; font-family:var(--font-mono); }
</style>
