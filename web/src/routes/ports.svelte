<script lang="ts">
  import { onMount } from 'svelte';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface PortInfo {
    proto: string;
    local_ip: string;
    local_port: number;
    state: string;
    pid: number;
    process_name: string;
    cmdline: string;
    systemd_socket?: string;
    inode?: number;
  }

  interface SystemdSocket {
    name: string;
    description: string;
    listen_ports: number[];
    listen_addrs: string[];
  }

  let ports: PortInfo[] = $state([]);
  let sockets: SystemdSocket[] = $state([]);
  let scannedAt = $state('');
  let loading = $state(false);
  let error = $state('');
  let filterProto = $state('all');
  let filterText = $state('');
  let sortField = $state('local_port');
  let sortAsc = $state(true);

  async function load() {
    loading = true;
    error = '';
    ports = [];
    sockets = [];
    try {
      const res = await fetch('/api/ports');
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const data = await res.json();
      ports = Array.isArray(data.ports) ? data.ports : [];
      sockets = Array.isArray(data.systemd_sockets) ? data.systemd_sockets : [];
      scannedAt = data.scanned_at ?? '';
    } catch (e: any) {
      error = String(e?.message ?? e);
    } finally {
      loading = false;
    }
  }

  function sortBy(field: string) {
    if (sortField === field) sortAsc = !sortAsc;
    else { sortField = field; sortAsc = true; }
  }

  // Intentionally NOT using $derived here — compute inline in template
  // to avoid any chance of a $derived crash freezing the component
  function getFiltered(): PortInfo[] {
    let out = ports.filter(p => {
      if (!p || typeof p !== 'object') return false;
      if (filterProto !== 'all' && !String(p.proto ?? '').startsWith(filterProto)) return false;
      if (filterText) {
        const t = filterText.toLowerCase();
        return String(p.process_name ?? '').toLowerCase().includes(t)
          || String(p.local_port ?? '').includes(t)
          || String(p.state ?? '').toLowerCase().includes(t)
          || String(p.systemd_socket ?? '').toLowerCase().includes(t);
      }
      return true;
    });

    out = out.slice().sort((a, b) => {
      try {
        if (sortField === 'local_port' || sortField === 'pid') {
          const an = Number(a[sortField as keyof PortInfo] ?? 0);
          const bn = Number(b[sortField as keyof PortInfo] ?? 0);
          return sortAsc ? an - bn : bn - an;
        }
        const as_ = String(a[sortField as keyof PortInfo] ?? '');
        const bs_ = String(b[sortField as keyof PortInfo] ?? '');
        return sortAsc ? as_.localeCompare(bs_) : bs_.localeCompare(as_);
      } catch { return 0; }
    });

    return out;
  }

  function stateClass(state: string) {
    if (state === 'LISTEN') return 'badge-green';
    if (state === 'ESTABLISHED') return 'badge-blue';
    if (state === 'UNCONN') return 'badge-gray';
    return 'badge-yellow';
  }

  function sortIcon(field: string) {
    if (sortField !== field) return '';
    return sortAsc ? ' ↑' : ' ↓';
  }

  onMount(load);
</script>

<div class="ports-page">
  <div class="page-header">
    <div>
      <h1>Ports &amp; Sockets</h1>
      {#if scannedAt}
        <p class="subtitle">Scanned at {new Date(scannedAt).toLocaleTimeString()} · {ports.length} sockets found</p>
      {:else if !loading}
        <p class="subtitle">Read directly from /proc/net — no external tools required</p>
      {/if}
    </div>
    <div class="actions">
      <button class="btn" onclick={load} disabled={loading}>
        {loading ? 'Scanning…' : '⟳ Rescan'}
      </button>
      <a class="btn btn-primary" href="#/migration">Build Migration Template</a>
    </div>
  </div>

  {#if error}
    <div class="alert alert-error" style="margin-bottom:1rem">{error}</div>
  {/if}

  {#if loading}
    <div class="card" style="padding:2rem;text-align:center;color:var(--text-tertiary)">
      Scanning /proc/net…
    </div>
  {:else}
    <!-- Filter bar -->
    <div class="filter-bar">
      <input
        class="search-input"
        style="max-width:320px"
        type="search"
        placeholder="Filter by process, port, state…"
        bind:value={filterText}
      />
      <div class="proto-tabs">
        {#each ['all', 'tcp', 'udp'] as proto}
          <button
            class="proto-tab"
            class:active={filterProto === proto}
            onclick={() => filterProto = proto}
          >{proto.toUpperCase()}</button>
        {/each}
      </div>
      <span class="result-count">{getFiltered().length} / {ports.length}</span>
    </div>

    <!-- Ports table -->
    <div class="table-wrap card" style="padding:0">
      <table class="data-table">
        <thead>
          <tr>
            <th class="sortable" onclick={() => sortBy('local_port')}>Port{sortIcon('local_port')}</th>
            <th class="sortable" onclick={() => sortBy('proto')}>Proto{sortIcon('proto')}</th>
            <th>Bind</th>
            <th class="sortable" onclick={() => sortBy('state')}>State{sortIcon('state')}</th>
            <th class="sortable" onclick={() => sortBy('process_name')}>Process{sortIcon('process_name')}</th>
            <th>PID</th>
            <th>systemd socket</th>
          </tr>
        </thead>
        <tbody>
          {#if getFiltered().length === 0}
            <tr><td colspan="7" class="cell-center">
              {ports.length === 0 ? 'No ports found' : 'No ports match filter'}
            </td></tr>
          {:else}
            {#each getFiltered() as p, i}
              <tr>
                <td class="mono" style="font-weight:600;color:var(--accent)">{p.local_port}</td>
                <td>
                  <span class="badge {String(p.proto).startsWith('tcp') ? 'badge-blue' : 'badge-yellow'}">{p.proto}</span>
                </td>
                <td class="mono" style="color:var(--text-tertiary);font-size:0.72rem">{p.local_ip}</td>
                <td><span class="badge {stateClass(p.state)}">{p.state}</span></td>
                <td>
                  <div class="process-cell">
                    <span style="font-weight:500">{p.process_name || '—'}</span>
                    {#if p.cmdline && p.cmdline !== p.process_name}
                      <span class="cmdline" title={p.cmdline}>
                        {p.cmdline.slice(0, 60)}{p.cmdline.length > 60 ? '…' : ''}
                      </span>
                    {/if}
                  </div>
                </td>
                <td class="mono" style="color:var(--text-tertiary)">{p.pid || '—'}</td>
                <td>
                  {#if p.systemd_socket}
                    <span class="badge badge-purple">{p.systemd_socket}</span>
                  {:else}
                    <span style="color:var(--text-tertiary)">—</span>
                  {/if}
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>

    <!-- systemd sockets -->
    {#if sockets.length > 0}
      <section style="margin-top:1.5rem">
        <h2 style="margin-bottom:0.75rem">systemd Socket Units</h2>
        <div class="socket-grid">
          {#each sockets as s}
            <div class="card socket-card">
              <div class="mono" style="font-weight:600;font-size:0.82rem;margin-bottom:0.25rem">{s.name}</div>
              {#if s.description}
                <div style="font-size:0.75rem;color:var(--text-secondary);margin-bottom:0.5rem">{s.description}</div>
              {/if}
              <div class="port-pills">
                {#each (s.listen_ports ?? []) as port}
                  <span class="badge badge-blue">{port}</span>
                {/each}
                {#each (s.listen_addrs ?? []) as addr}
                  <span class="badge badge-gray">{addr}</span>
                {/each}
              </div>
            </div>
          {/each}
        </div>
      </section>
    {/if}
  {/if}

  <CLIEchoPane context="ports" />
</div>

<style>
.ports-page { max-width: 1200px; padding-bottom: 220px; }

.filter-bar { display: flex; gap: 0.75rem; align-items: center; margin-bottom: 0.75rem; }
.proto-tabs { display: flex; border: 1px solid var(--border-default); border-radius: var(--r-md); overflow: hidden; }
.proto-tab { padding: 0.35rem 0.65rem; background: var(--bg-raised); border: none; border-right: 1px solid var(--border-default); cursor: pointer; font-size: 0.72rem; font-family: var(--font-mono); color: var(--text-secondary); }
.proto-tab:last-child { border-right: none; }
.proto-tab.active { background: var(--accent); color: var(--text-inverse); font-weight: 600; }
.result-count { font-size: 0.72rem; color: var(--text-tertiary); font-family: var(--font-mono); }

.sortable { cursor: pointer; user-select: none; }
.sortable:hover { color: var(--text-primary) !important; }
.table-wrap { overflow-x: auto; }

.process-cell { display: flex; flex-direction: column; gap: 1px; }
.cmdline { font-family: var(--font-mono); font-size: 0.68rem; color: var(--text-tertiary); }
.cell-center { text-align: center; padding: 2rem; color: var(--text-tertiary); }

.socket-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 0.5rem; }
.socket-card { padding: 0.75rem; }
.port-pills { display: flex; flex-wrap: wrap; gap: 0.25rem; }
</style>
