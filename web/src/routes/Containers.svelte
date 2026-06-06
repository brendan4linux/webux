<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface Container {
    id: string; short_id: string; name: string; image: string;
    state: string; status: string; created: string;
    ports: {host_port:number, container_port:number, protocol:string}[];
    mounts: {source:string, destination:string, rw:boolean}[];
    networks: string[];
    size_rw: number; size_rootfs: number;
  }
  interface Image {
    id: string; short_id: string; tags: string[];
    created: string; size_bytes: number;
  }

  let containers: Container[] = $state([]);
  let images: Image[] = $state([]);
  let runtimes: string[] = $state([]);
  let tab = $state<'containers'|'images'>('containers');
  let selectedRuntime = $state('');
  let showAll = $state(true);
  let loading = $state(true);
  let error = $state('');
  let actionPending = $state<string|null>(null);
  let expandedId = $state<string|null>(null);
  let logs = $state<string[]>([]);
  let logsLoading = $state(false);
  let pullRef = $state('');
  let pulling = $state(false);

  async function load() {
    loading = true; error = '';
    try {
      const statusRes = await api.get<any>('/api/containers/status');
      runtimes = statusRes.runtimes ?? [];
      if (!selectedRuntime && runtimes.length) selectedRuntime = runtimes[0];

      const [cRes, iRes] = await Promise.all([
        api.get<any>(`/api/containers?runtime=${selectedRuntime}&all=${showAll}`),
        api.get<any>(`/api/containers/images?runtime=${selectedRuntime}`),
      ]);
      containers = cRes.containers ?? [];
      images = iRes.images ?? [];
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function doAction(id: string, action: string) {
    actionPending = id + ':' + action;
    try {
      await api.post(`/api/containers/${id}/action?runtime=${selectedRuntime}`,
        { action, force: action === 'remove' });
      await load();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  async function showLogs(id: string) {
    expandedId = id; logs = []; logsLoading = true;
    try {
      const res = await fetch(`/api/containers/${id}/logs?runtime=${selectedRuntime}&tail=200`);
      const text = await res.text();
      logs = text.split('\n')
        .filter(l => l.startsWith('data: '))
        .map(l => l.slice(6));
    } catch(e: any) { error = String(e); }
    finally { logsLoading = false; }
  }

  function stateClass(state: string) {
    if (state === 'running') return 'badge-green';
    if (state === 'exited' || state === 'stopped') return 'badge-gray';
    if (state === 'paused') return 'badge-yellow';
    return 'badge-red';
  }

  function fmtSize(b: number) {
    if (b > 1e9) return (b/1e9).toFixed(1) + ' GB';
    if (b > 1e6) return (b/1e6).toFixed(1) + ' MB';
    if (b > 1e3) return (b/1e3).toFixed(1) + ' KB';
    return b + ' B';
  }

  onMount(load);
</script>

<div class="ctr-page">
  <div class="page-header">
    <div>
      <h1>Containers</h1>
      <p class="subtitle">{containers.length} containers · {images.length} images</p>
    </div>
    <div class="actions">
      {#if runtimes.length > 1}
        <div class="seg-control">
          {#each runtimes as rt}
            <button class="seg-btn" class:active={selectedRuntime === rt}
              onclick={() => { selectedRuntime = rt; load(); }}>{rt}</button>
          {/each}
        </div>
      {/if}
      <label class="sys-toggle">
        <input type="checkbox" bind:checked={showAll} onchange={load} />
        <span>Show stopped</span>
      </label>
      <button class="btn" onclick={load} disabled={loading}>⟳ Refresh</button>
    </div>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  {#if runtimes.length === 0 && !loading}
    <div class="card no-runtime">
      <div style="font-size:2rem;margin-bottom:1rem">▣</div>
      <h2>No container runtime detected</h2>
      <p>Install Docker or Podman and ensure the socket is accessible.</p>
      <code class="mono">sudo systemctl enable --now docker</code>
    </div>
  {:else}
    <div class="tab-bar">
      <button class="tab-btn" class:active={tab==='containers'} onclick={() => tab='containers'}>
        Containers ({containers.length})
      </button>
      <button class="tab-btn" class:active={tab==='images'} onclick={() => tab='images'}>
        Images ({images.length})
      </button>
    </div>

    {#if tab === 'containers'}
      <div class="card" style="padding:0">
        <table class="data-table">
          <thead>
            <tr>
              <th>Name</th><th>Image</th><th>State</th>
              <th>Ports</th><th>Size</th>
              <th style="text-align:right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {#if loading}
              {#each [1,2,3] as _}
                <tr>{#each [1,2,3,4,5,6] as _}<td><div class="skeleton" style="height:13px;width:80%"></div></td>{/each}</tr>
              {/each}
            {:else if containers.length === 0}
              <tr><td colspan="6" style="text-align:center;padding:2rem;color:var(--text-tertiary)">
                No containers found
              </td></tr>
            {:else}
              {#each containers as c (c.id)}
                <tr class="ctr-row" class:expanded={expandedId === c.id}>
                  <td>
                    <div class="mono" style="font-weight:600">{c.name}</div>
                    <div style="font-size:0.68rem;color:var(--text-tertiary)">{c.short_id}</div>
                  </td>
                  <td style="font-size:0.78rem;max-width:180px;overflow:hidden;text-overflow:ellipsis" title={c.image}>
                    {c.image}
                  </td>
                  <td><span class="badge {stateClass(c.state)}">{c.state}</span></td>
                  <td>
                    {#each (c.ports ?? []).filter(p => p.host_port) as p}
                      <span class="badge badge-blue" style="margin:1px">{p.host_port}→{p.container_port}</span>
                    {/each}
                  </td>
                  <td class="mono" style="font-size:0.72rem;color:var(--text-tertiary)">{fmtSize(c.size_rw)}</td>
                  <td>
                    <div style="display:flex;gap:0.25rem;justify-content:flex-end">
                      {#if c.state === 'running'}
                        <button class="btn btn-ghost" style="font-size:0.72rem"
                          disabled={actionPending !== null}
                          onclick={() => doAction(c.id, 'stop')}>■ Stop</button>
                        <button class="btn btn-ghost" style="font-size:0.72rem"
                          disabled={actionPending !== null}
                          onclick={() => doAction(c.id, 'restart')}>↺</button>
                      {:else}
                        <button class="btn btn-primary" style="font-size:0.72rem"
                          disabled={actionPending !== null}
                          onclick={() => doAction(c.id, 'start')}>▶ Start</button>
                      {/if}
                      <button class="btn btn-ghost" style="font-size:0.72rem"
                        onclick={() => showLogs(c.id)}>≡ Logs</button>
                      <button class="btn btn-ghost" style="font-size:0.72rem;color:var(--red)"
                        disabled={actionPending !== null}
                        onclick={() => doAction(c.id, 'remove')}>✕</button>
                    </div>
                  </td>
                </tr>
                {#if expandedId === c.id}
                  <tr>
                    <td colspan="6" style="padding:0">
                      <div class="log-panel">
                        <div class="log-panel-header">
                          <span class="mono">{c.name}</span>
                          <span style="color:var(--text-tertiary)"> — logs</span>
                          <button class="btn btn-ghost" style="margin-left:auto"
                            onclick={() => { expandedId = null; logs = []; }}>✕ Close</button>
                        </div>
                        <div class="log-body">
                          {#if logsLoading}
                            <div style="padding:1rem;color:var(--text-tertiary)">Loading…</div>
                          {:else}
                            {#each logs as line}
                              <div class="log-line mono">{line}</div>
                            {/each}
                          {/if}
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

    {:else}
      <!-- Images tab -->
      <div style="margin-bottom:0.75rem;display:flex;gap:0.5rem">
        <input class="search-input" style="max-width:280px" bind:value={pullRef}
          placeholder="nginx:latest, ubuntu:22.04…" />
        <button class="btn btn-primary" onclick={() => {}} disabled={pulling || !pullRef}>
          {pulling ? 'Pulling…' : '⬇ Pull image'}
        </button>
      </div>
      <div class="card" style="padding:0">
        <table class="data-table">
          <thead>
            <tr><th>Tags</th><th>ID</th><th>Created</th><th>Size</th><th style="text-align:right">Remove</th></tr>
          </thead>
          <tbody>
            {#if loading}
              {#each [1,2] as _}<tr>{#each [1,2,3,4,5] as _}<td><div class="skeleton" style="height:13px;width:80%"></div></td>{/each}</tr>{/each}
            {:else if images.length === 0}
              <tr><td colspan="5" style="text-align:center;padding:2rem;color:var(--text-tertiary)">No images</td></tr>
            {:else}
              {#each images as img (img.id)}
                <tr>
                  <td>
                    {#each (img.tags ?? []) as tag}
                      <span class="badge badge-gray" style="margin:1px">{tag}</span>
                    {/each}
                    {#if !img.tags?.length}<span style="color:var(--text-tertiary)">&lt;none&gt;</span>{/if}
                  </td>
                  <td class="mono" style="font-size:0.72rem;color:var(--text-tertiary)">{img.short_id}</td>
                  <td style="font-size:0.78rem;color:var(--text-secondary)">{new Date(img.created).toLocaleDateString()}</td>
                  <td class="mono" style="font-size:0.75rem">{fmtSize(img.size_bytes)}</td>
                  <td style="text-align:right">
                    <button class="btn btn-ghost" style="color:var(--red);font-size:0.72rem"
                      onclick={() => doAction(img.id, 'remove')}>✕</button>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}

  <CLIEchoPane context="containers" />
</div>

<style>
.ctr-page { max-width:1200px; padding-bottom:220px; }
.tab-bar { display:flex; border-bottom:1px solid var(--border-subtle); margin-bottom:0.75rem; }
.tab-btn { padding:0.5rem 1rem; background:none; border:none; border-bottom:2px solid transparent; cursor:pointer; font-size:0.85rem; color:var(--text-secondary); margin-bottom:-1px; }
.tab-btn.active { color:var(--accent); border-bottom-color:var(--accent); font-weight:500; }
.seg-control { display:flex; border:1px solid var(--border-default); border-radius:var(--r-md); overflow:hidden; }
.seg-btn { padding:0.3rem 0.6rem; background:var(--bg-raised); border:none; border-right:1px solid var(--border-default); cursor:pointer; font-size:0.72rem; font-family:var(--font-mono); color:var(--text-secondary); }
.seg-btn:last-child { border-right:none; }
.seg-btn.active { background:var(--accent); color:var(--text-inverse); }
.sys-toggle { display:flex; align-items:center; gap:0.375rem; font-size:0.78rem; color:var(--text-secondary); cursor:pointer; }
.ctr-row:hover { background:var(--bg-hover); }
.ctr-row.expanded { background:var(--accent-dim); }
.no-runtime { text-align:center; padding:3rem; }
.no-runtime h2 { margin-bottom:0.5rem; }
.no-runtime p { color:var(--text-secondary); font-size:0.875rem; margin-bottom:1rem; }
.log-panel { background:var(--bg-base); border-top:1px solid var(--border-subtle); }
.log-panel-header { display:flex; align-items:center; gap:0.5rem; padding:0.5rem 1rem; background:var(--bg-raised); border-bottom:1px solid var(--border-subtle); font-size:0.82rem; }
.log-body { max-height:280px; overflow-y:auto; padding:0.5rem 1rem; }
.log-line { font-size:0.72rem; color:var(--text-secondary); line-height:1.5; white-space:pre-wrap; word-break:break-all; }
</style>
