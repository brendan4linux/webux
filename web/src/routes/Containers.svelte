<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface Port { host_port:number; container_port:number; protocol:string; }
  interface Mount { source:string; destination:string; rw:boolean; }
  interface Container {
    id:string; short_id:string; name:string; image:string;
    state:string; status:string; created:string;
    ports:Port[]; mounts:Mount[]; networks:string[];
    size_rw:number; size_rootfs:number;
  }
  interface Image {
    id:string; short_id:string; tags:string[];
    created:string; size_bytes:number;
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

  // ── Delete confirmation ────────────────────────────────────────────────
  let confirmDelete = $state<{id:string; name:string} | null>(null);

  // ── Deploy modal ───────────────────────────────────────────────────────
  interface DeployPort  { host:string; container:string; }
  interface DeployMount { host:string; container:string; }
  interface DeployEnv   { key:string; value:string; }
  let deployImage = $state<Image|null>(null);
  let deployName  = $state('');
  let deployPorts:  DeployPort[]  = $state([{ host:'', container:'' }]);
  let deployMounts: DeployMount[] = $state([{ host:'', container:'' }]);
  let deployEnvs:   DeployEnv[]   = $state([{ key:'', value:'' }]);
  let deploying = $state(false);
  let deployError = $state('');

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

  async function doAction(id: string, action: string, name?: string) {
    // Confirm before removing
    if (action === 'remove') {
      confirmDelete = { id, name: name ?? id.slice(0,12) };
      return;
    }
    await runAction(id, action);
  }

  async function runAction(id: string, action: string) {
    actionPending = id + ':' + action;
    try {
      await api.post(`/api/containers/${id}/action?runtime=${selectedRuntime}`,
        { action, force: action === 'remove' });
      await load();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  async function confirmAndRemove() {
    if (!confirmDelete) return;
    const { id } = confirmDelete;
    confirmDelete = null;
    await runAction(id, 'remove');
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

  // ── Deploy from image ──────────────────────────────────────────────────
  function openDeploy(img: Image) {
    deployImage = img;
    const tag = img.tags?.[0] ?? '';
    deployName = tag.replace(/[^a-zA-Z0-9_-]/g, '-').replace(/^-+|-+$/g, '').slice(0, 32) || '';
    deployPorts  = [{ host:'', container:'' }];
    deployMounts = [{ host:'', container:'' }];
    deployEnvs   = [{ key:'', value:'' }];
    deployError  = '';
    deploying    = false;
  }

  async function runDeploy() {
    if (!deployImage) return;
    deploying = true; deployError = '';
    const tag = deployImage.tags?.[0] ?? deployImage.short_id;

    // Build docker run equivalent args
    const ports  = deployPorts.filter(p => p.host && p.container);
    const mounts = deployMounts.filter(m => m.host && m.container);
    const envs   = deployEnvs.filter(e => e.key);

    try {
      await api.post(`/api/containers/run?runtime=${selectedRuntime}`, {
        image:  tag,
        name:   deployName || undefined,
        ports:  ports,
        mounts: mounts,
        env:    envs,
      });
      deployImage = null;
      tab = 'containers';
      await load();
    } catch(e: any) { deployError = e.message; }
    finally { deploying = false; }
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

    <!-- ── Containers tab ─────────────────────────────────────────── -->
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
                          onclick={() => doAction(c.id, 'stop', c.name)}>■ Stop</button>
                        <button class="btn btn-ghost" style="font-size:0.72rem"
                          disabled={actionPending !== null}
                          onclick={() => doAction(c.id, 'restart', c.name)}>↺</button>
                      {:else}
                        <button class="btn btn-primary" style="font-size:0.72rem"
                          disabled={actionPending !== null}
                          onclick={() => doAction(c.id, 'start', c.name)}>▶ Start</button>
                      {/if}
                      <button class="btn btn-ghost" style="font-size:0.72rem"
                        onclick={() => showLogs(c.id)}>≡ Logs</button>
                      <button class="btn btn-ghost" style="font-size:0.72rem;color:var(--red)"
                        disabled={actionPending !== null}
                        onclick={() => doAction(c.id, 'remove', c.name)}>✕ Remove</button>
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

    <!-- ── Images tab ─────────────────────────────────────────────── -->
    {:else}
      <div style="margin-bottom:0.75rem;display:flex;gap:0.5rem">
        <input class="search-input" style="max-width:280px" bind:value={pullRef}
          placeholder="nginx:latest, ubuntu:22.04…" />
        <button class="btn btn-primary" disabled={pulling || !pullRef}>
          {pulling ? 'Pulling…' : '⬇ Pull image'}
        </button>
      </div>
      <div class="card" style="padding:0">
        <table class="data-table">
          <thead>
            <tr><th>Tags</th><th>ID</th><th>Created</th><th>Size</th><th style="text-align:right">Actions</th></tr>
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
                    <div style="display:flex;gap:0.25rem;justify-content:flex-end">
                      <button class="btn btn-primary" style="font-size:0.72rem"
                        onclick={() => openDeploy(img)}>▶ Deploy</button>
                      <button class="btn btn-ghost" style="color:var(--red);font-size:0.72rem"
                        onclick={() => doAction(img.id, 'remove')}>✕</button>
                    </div>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>
    {/if}
  {/if}

  <!-- ── Delete confirmation modal ──────────────────────────────── -->
  {#if confirmDelete}
    <div class="modal-overlay" role="button" tabindex="0"
      onkeydown={(e) => e.key === 'Escape' && (confirmDelete = null)}
      onclick={() => confirmDelete = null}>
      <div class="modal confirm-modal" role="dialog" tabindex="-1"
        onclick={(e) => e.stopPropagation()}>
        <div class="modal-header">
          <h2>Remove container?</h2>
        </div>
        <div class="modal-body">
          <p>Are you sure you want to remove <strong class="mono">{confirmDelete.name}</strong>?</p>
          <p style="font-size:0.8rem;color:var(--text-tertiary);margin-top:0.375rem">
            This will delete the container. Volumes will not be removed.
          </p>
        </div>
        <div class="modal-footer">
          <button class="btn" onclick={() => confirmDelete = null}>Cancel</button>
          <button class="btn btn-danger" onclick={confirmAndRemove}>Remove</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ── Deploy modal ───────────────────────────────────────────── -->
  {#if deployImage}
    <div class="modal-overlay" role="button" tabindex="0"
      onkeydown={(e) => e.key === 'Escape' && !deploying && (deployImage = null)}
      onclick={() => !deploying && (deployImage = null)}>
      <div class="modal deploy-modal" role="dialog" tabindex="-1"
        onclick={(e) => e.stopPropagation()}>
        <div class="modal-header">
          <h2>Deploy image</h2>
          {#if !deploying}
            <button class="btn btn-ghost" onclick={() => deployImage = null}>✕</button>
          {/if}
        </div>
        <div class="modal-body">
          <!-- Image name -->
          <div class="deploy-image-name mono">
            {deployImage.tags?.[0] ?? deployImage.short_id}
          </div>

          <!-- Container name -->
          <div class="field">
            <label>Container name <span class="optional">(optional)</span></label>
            <input class="search-input" bind:value={deployName} placeholder="my-container"
              disabled={deploying} />
          </div>

          <!-- Ports -->
          <div class="field">
            <label>Port mappings <span class="optional">(-p host:container)</span></label>
            {#each deployPorts as p, i}
              <div class="mapping-row">
                <input class="search-input mono" style="width:100px" bind:value={p.host}
                  placeholder="8080" disabled={deploying} />
                <span class="mapping-sep">→</span>
                <input class="search-input mono" style="width:100px" bind:value={p.container}
                  placeholder="80" disabled={deploying} />
                {#if deployPorts.length > 1}
                  <button class="btn btn-ghost" style="font-size:0.72rem"
                    onclick={() => deployPorts = deployPorts.filter((_, j) => j !== i)}>✕</button>
                {/if}
              </div>
            {/each}
            <button class="btn btn-ghost" style="font-size:0.72rem;margin-top:0.25rem"
              onclick={() => deployPorts = [...deployPorts, { host:'', container:'' }]}
              disabled={deploying}>+ Add port</button>
          </div>

          <!-- Volumes -->
          <div class="field">
            <label>Volume mounts <span class="optional">(-v host:container)</span></label>
            {#each deployMounts as m, i}
              <div class="mapping-row">
                <input class="search-input mono" style="flex:1" bind:value={m.host}
                  placeholder="/host/path" disabled={deploying} />
                <span class="mapping-sep">:</span>
                <input class="search-input mono" style="flex:1" bind:value={m.container}
                  placeholder="/container/path" disabled={deploying} />
                {#if deployMounts.length > 1}
                  <button class="btn btn-ghost" style="font-size:0.72rem"
                    onclick={() => deployMounts = deployMounts.filter((_, j) => j !== i)}>✕</button>
                {/if}
              </div>
            {/each}
            <button class="btn btn-ghost" style="font-size:0.72rem;margin-top:0.25rem"
              onclick={() => deployMounts = [...deployMounts, { host:'', container:'' }]}
              disabled={deploying}>+ Add mount</button>
          </div>

          <!-- Environment variables -->
          <div class="field">
            <label>Environment variables <span class="optional">(-e KEY=value)</span></label>
            {#each deployEnvs as e, i}
              <div class="mapping-row">
                <input class="search-input mono" style="width:130px" bind:value={e.key}
                  placeholder="ENV_VAR" disabled={deploying} />
                <span class="mapping-sep">=</span>
                <input class="search-input mono" style="flex:1" bind:value={e.value}
                  placeholder="value" disabled={deploying} />
                {#if deployEnvs.length > 1}
                  <button class="btn btn-ghost" style="font-size:0.72rem"
                    onclick={() => deployEnvs = deployEnvs.filter((_, j) => j !== i)}>✕</button>
                {/if}
              </div>
            {/each}
            <button class="btn btn-ghost" style="font-size:0.72rem;margin-top:0.25rem"
              onclick={() => deployEnvs = [...deployEnvs, { key:'', value:'' }]}
              disabled={deploying}>+ Add env var</button>
          </div>

          <!-- Preview command -->
          <div class="preview-cmd mono">
            {selectedRuntime} run -d
            {deployName ? `--name ${deployName}` : ''}
            {deployPorts.filter(p=>p.host&&p.container).map(p=>`-p ${p.host}:${p.container}`).join(' ')}
            {deployMounts.filter(m=>m.host&&m.container).map(m=>`-v ${m.host}:${m.container}`).join(' ')}
            {deployEnvs.filter(e=>e.key).map(e=>`-e ${e.key}=${e.value}`).join(' ')}
            {deployImage.tags?.[0] ?? deployImage.short_id}
          </div>

          {#if deployError}
            <div class="alert alert-error" style="margin-top:0.5rem">{deployError}</div>
          {/if}
        </div>
        <div class="modal-footer">
          <button class="btn" onclick={() => deployImage = null} disabled={deploying}>Cancel</button>
          <button class="btn btn-primary" onclick={runDeploy} disabled={deploying}>
            {deploying ? '⟳ Deploying…' : '▶ Deploy'}
          </button>
        </div>
      </div>
    </div>
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
/* Modals */
.modal-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.6); display:flex; align-items:center; justify-content:center; z-index:500; }
.modal { background:var(--bg-panel); border:1px solid var(--border-default); border-radius:var(--r-lg); }
.modal-header { display:flex; justify-content:space-between; align-items:center; padding:1rem; border-bottom:1px solid var(--border-subtle); }
.modal-header h2 { font-size:1rem; margin:0; }
.modal-body { padding:1rem; display:flex; flex-direction:column; gap:0.875rem; max-height:70vh; overflow-y:auto; }
.modal-footer { display:flex; justify-content:flex-end; gap:0.5rem; padding:1rem; border-top:1px solid var(--border-subtle); }
/* Confirm modal */
.confirm-modal { width:380px; max-width:95vw; }
.confirm-modal p { font-size:0.85rem; margin:0; }
/* Deploy modal */
.deploy-modal { width:540px; max-width:95vw; }
.deploy-image-name { font-size:0.82rem; color:var(--accent); background:var(--bg-raised); padding:0.375rem 0.625rem; border-radius:var(--r-sm); border:1px solid var(--border-subtle); }
.field { display:flex; flex-direction:column; gap:0.375rem; }
.field label { font-size:0.72rem; font-weight:500; color:var(--text-secondary); }
.optional { color:var(--text-tertiary); font-weight:400; }
.mapping-row { display:flex; align-items:center; gap:0.375rem; margin-bottom:0.25rem; }
.mapping-sep { color:var(--text-tertiary); font-family:var(--font-mono); font-size:0.82rem; flex-shrink:0; }
.preview-cmd { font-size:0.68rem; color:var(--text-tertiary); background:var(--bg-base); border-radius:var(--r-sm); padding:0.5rem 0.625rem; border-left:2px solid var(--border-default); white-space:pre-wrap; word-break:break-all; line-height:1.7; }
.btn-danger { background:var(--red); color:#fff; border-color:var(--red); }
.btn-danger:hover { opacity:0.85; }
</style>
