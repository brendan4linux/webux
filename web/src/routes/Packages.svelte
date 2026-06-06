<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface Package { name:string; version:string; new_version:string; description:string; size:string; repo:string; installed:boolean; upgradable:boolean; }
  interface FlatpakApp { name:string; app_id:string; version:string; branch:string; origin:string; install_type:string; }
  interface Repo { id:string; name:string; url:string; enabled:boolean; file:string; line:number; keyring:string; section:string; extra:string; source:string; }
  interface FlatpakRemote { name:string; url:string; enabled:boolean; type:string; }

  let family      = $state('');
  let hasFlatpak  = $state(false);
  let infoLoading = $state(true);
  let tab = $state<'installed'|'upgradable'|'search'|'flatpak'|'repos'>('installed');

  let installed:       Package[]      = $state([]);
  let upgradable:      Package[]      = $state([]);
  let searchResults:   Package[]      = $state([]);
  let flatpaks:        FlatpakApp[]   = $state([]);
  let repos:           Repo[]         = $state([]);
  let flatpakRemotes:  FlatpakRemote[]= $state([]);

  let loadingTab = $state('');  // which tab is currently fetching
  let searching         = $state(false);
  let error             = $state('');
  let filterText        = $state('');
  let searchQuery       = $state('');

  let opOutput:string[] = $state([]);
  let opRunning = $state(false);
  let opTitle   = $state('');

  // Repo modals
  let showAddRepo  = $state(false);
  let newRepo      = $state({ name:'', url:'', key_url:'' });
  let addRepoError = $state('');
  let addingRepo   = $state(false);
  let showAddRemote = $state(false);
  let newRemote     = $state({ name:'', url:'', system:true });

  // Tab load guards
  let _loadedInstalled  = false;
  let _loadedUpgradable = false;
  let _loadedFlatpak    = false;
  let _loadedRepos      = false;

  async function loadInfo() {
    infoLoading = true;
    try {
      const res = await api.get<any>('/api/packages/info');
      family = res.family ?? ''; hasFlatpak = res.has_flatpak ?? false;
      // Pre-fetch flatpak count so the tab label is correct before clicking
      if (res.has_flatpak) {
        api.get<any>('/api/packages/flatpak').then(r => {
          if (r.apps) flatpaks = r.apps;
          _loadedFlatpak = true;
        }).catch(() => {});
      }
    } catch(e:any) { error = e.message; }
    finally { infoLoading = false; }
  }

  async function loadInstalled() {
    loadingTab = 'installed'; error = '';
    try { const r = await api.get<any>('/api/packages'); installed = r.packages ?? []; }
    catch(e:any) { error = e.message; }
    finally { loadingTab = ''; }
  }

  async function loadUpgradable() {
    loadingTab = 'upgradable'; error = '';
    try { const r = await api.get<any>('/api/packages/upgradable'); upgradable = r.packages ?? []; }
    catch(e:any) { error = e.message; }
    finally { loadingTab = ''; }
  }

  async function loadFlatpaks() {
    loadingTab = 'flatpak'; error = '';
    try { const r = await api.get<any>('/api/packages/flatpak'); flatpaks = r.apps ?? []; }
    catch(e:any) { error = e.message; }
    finally { loadingTab = ''; }
  }

  async function loadRepos() {
    loadingTab = 'repos'; error = '';
    try {
      const [rRes, frRes] = await Promise.all([
        api.get<any>('/api/packages/repos'),
        hasFlatpak ? api.get<any>('/api/packages/repos/flatpak') : Promise.resolve({ remotes:[] }),
      ]);
      repos = rRes.repos ?? []; flatpakRemotes = frRes.remotes ?? [];
    } catch(e:any) { error = e.message; }
    finally { loadingTab = ''; }
  }

  function switchTab(t: typeof tab) {
    tab = t;
    if (t === 'upgradable' && !_loadedUpgradable) { _loadedUpgradable = true; loadUpgradable(); }
    if (t === 'flatpak'    && !_loadedFlatpak)    { _loadedFlatpak    = true; loadFlatpaks(); }
    if (t === 'repos'      && !_loadedRepos)       { _loadedRepos      = true; loadRepos(); }
  }

  function refreshUpgradable() { _loadedUpgradable = false; switchTab('upgradable'); }

  async function doSearch() {
    if (!searchQuery) return;
    searching = true;
    try { const r = await api.get<any>('/api/packages/search?q=' + encodeURIComponent(searchQuery)); searchResults = r.packages ?? []; }
    catch(e:any) { error = e.message; }
    finally { searching = false; }
  }

  async function streamOp(url:string, body:any, title:string) {
    opRunning = true; opOutput = []; opTitle = title;
    try {
      const resp = await fetch(url, { method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body) });
      const reader = resp.body!.getReader(); const dec = new TextDecoder(); let buf = '';
      while(true) {
        const {done,value} = await reader.read(); if(done) break;
        buf += dec.decode(value,{stream:true});
        const lines = buf.split('\n');
        for(const line of lines.slice(0,-1)) { if(line.startsWith('data: ')) opOutput = [...opOutput, line.slice(6)]; }
        buf = lines[lines.length-1];
      }
    } catch(e:any) { opOutput = [...opOutput, '[error] '+String(e)]; }
    finally {
      opRunning = false;
      _loadedInstalled = false; loadInstalled();
      if(tab==='upgradable') { _loadedUpgradable=false; loadUpgradable(); }
      if(tab==='flatpak')    { _loadedFlatpak=false;    loadFlatpaks(); }
    }
  }

  const install        = (name:string) => streamOp('/api/packages/install',       {name},  'Installing '+name+'…');
  const remove         = (name:string) => { if(!confirm('Remove '+name+'?')) return; streamOp('/api/packages/remove',{name},'Removing '+name+'…'); };
  const upgrade        = (name='')     => streamOp('/api/packages/upgrade',        {name},  name?'Upgrading '+name+'…':'Upgrading all packages…');
  const updateCache    = ()            => streamOp('/api/packages/update-cache',  {},      'Refreshing package cache…');
  const removeFlatpak  = (id:string)   => { if(!confirm('Remove '+id+'?')) return; streamOp('/api/packages/flatpak/remove',{app_id:id},'Removing '+id+'…'); };
  const updateFlatpaks = ()            => streamOp('/api/packages/flatpak/update',{},'Updating all Flatpaks…');

  async function toggleRepo(repo:Repo) {
    try { await api.post(repo.enabled ? '/api/packages/repos/disable' : '/api/packages/repos/enable', repo); await loadRepos(); }
    catch(e:any) { error = e.message; }
  }
  async function removeRepo(repo:Repo) {
    if(!confirm('Remove repo "'+repo.name+'"? This edits '+repo.file+'.')) return;
    try { await api.delete('/api/packages/repos', repo as any); await loadRepos(); }
    catch(e:any) { error = e.message; }
  }
  async function addRepo() {
    if(!newRepo.name || !newRepo.url) { addRepoError='Name and URL required'; return; }
    addingRepo=true; addRepoError='';
    try { await api.post('/api/packages/repos', newRepo); showAddRepo=false; newRepo={name:'',url:'',key_url:''}; await loadRepos(); }
    catch(e:any) { addRepoError=e.message; }
    finally { addingRepo=false; }
  }
  async function addFlatpakRemote() {
    if(!newRemote.name||!newRemote.url) return;
    try { await api.post('/api/packages/repos/flatpak', newRemote); showAddRemote=false; newRemote={name:'',url:'',system:true}; await loadRepos(); }
    catch(e:any) { error=e.message; }
  }
  async function removeFlatpakRemote(name:string) {
    if(!confirm('Remove Flatpak remote "'+name+'"?')) return;
    try { await api.delete('/api/packages/repos/flatpak', {name} as any); await loadRepos(); }
    catch(e:any) { error=e.message; }
  }

  let filteredInstalled = $derived(installed.filter(p =>
    !filterText || p.name.toLowerCase().includes(filterText.toLowerCase()) ||
    p.description?.toLowerCase().includes(filterText.toLowerCase())
  ));

  onMount(async () => {
    await loadInfo();
    _loadedInstalled = true;
    await loadInstalled();
  });
</script>

<div class="pkg-page">
  <div class="page-header">
    <div>
      <h1>Packages</h1>
      <p class="subtitle">
        {#if family}
          <span class="badge badge-blue">{family}</span>
          {#if hasFlatpak}<span class="badge badge-purple" style="margin-left:0.25rem">flatpak</span>{/if}
          · {installed.length.toLocaleString()} installed
        {/if}
      </p>
    </div>
    <div class="actions">
      <button class="btn" onclick={updateCache} disabled={opRunning}>⟳ Update cache</button>
      <button class="btn btn-primary" onclick={() => upgrade()} disabled={opRunning}>↑ Upgrade all</button>
    </div>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  {#if infoLoading}
    <div class="card skeleton" style="height:48px;margin-bottom:0.75rem"></div>
  {:else if !family}
    <div class="card" style="text-align:center;padding:3rem;color:var(--text-tertiary)">
      No supported package manager detected (pacman, apt, dnf, yum)
    </div>
  {:else}
    {#if opOutput.length > 0}
      <div class="card op-panel">
        <div class="op-header">
          <span class="mono" style="font-size:0.78rem">{opTitle}</span>
          {#if opRunning}<span class="dot dot-green" style="margin-left:0.5rem"></span>{/if}
          {#if !opRunning}<button class="btn btn-ghost" style="margin-left:auto;font-size:0.72rem" onclick={() => opOutput=[]}>✕ Clear</button>{/if}
        </div>
        <div class="op-body">
          {#each opOutput as line}
            <div class="op-line mono" class:line-ok={line.includes('install')||line.includes('complet')||line.includes('done')} class:line-err={line.includes('error')||line.includes('Error')||line.includes('failed')}>{line}</div>
          {/each}
        </div>
      </div>
    {/if}

    <div class="tab-bar">
      <button class="tab-btn" class:active={tab==='installed'} onclick={() => switchTab('installed')}>
        Installed ({installed.length.toLocaleString()})
      </button>
      <button class="tab-btn" class:active={tab==='upgradable'} onclick={refreshUpgradable}>
        Upgradable {#if upgradable.length}<span class="badge badge-yellow" style="margin-left:0.25rem">{upgradable.length}</span>{/if}
      </button>
      <button class="tab-btn" class:active={tab==='search'} onclick={() => switchTab('search')}>Search</button>
      {#if hasFlatpak}
        <button class="tab-btn" class:active={tab==='flatpak'} onclick={() => switchTab('flatpak')}>Flatpak ({flatpaks.length})</button>
      {/if}
      <button class="tab-btn" class:active={tab==='repos'} onclick={() => switchTab('repos')}>
        Repos {#if repos.length}<span class="badge badge-gray" style="margin-left:0.25rem">{repos.length}</span>{/if}
      </button>
    </div>

    <!-- ── Installed ── -->
    {#if tab === 'installed'}
      <div style="margin-bottom:0.75rem">
        <input class="search-input" style="max-width:320px" type="search" placeholder="Filter packages…" bind:value={filterText} />
      </div>
      <div class="card" style="padding:0;overflow-x:auto">
        <table class="data-table pkg-table">
          <thead><tr><th>Package</th><th>Version</th><th>Size</th><th>Description</th><th style="text-align:right">Remove</th></tr></thead>
          <tbody>
            {#if loadingTab === 'installed'}
              {#each [1,2,3,4,5] as _}
                <tr>{#each [1,2,3,4,5] as _}<td><div class="skeleton" style="height:13px;width:80%"></div></td>{/each}</tr>
              {/each}
            {:else if filteredInstalled.length === 0}
              <tr><td colspan="5" style="text-align:center;padding:2rem;color:var(--text-tertiary)">No packages match</td></tr>
            {:else}
              {#each filteredInstalled as pkg, i (pkg.name + ':' + i)}
                <tr>
                  <td class="mono" style="font-weight:600">{pkg.name}</td>
                  <td class="mono" style="color:var(--text-secondary);font-size:0.8rem">{pkg.version}</td>
                  <td style="color:var(--text-tertiary);font-size:0.78rem">{pkg.size||'—'}</td>
                  <td style="font-size:0.78rem;color:var(--text-secondary);max-width:300px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title={pkg.description}>{pkg.description||'—'}</td>
                  <td style="text-align:right">
                    <button class="btn btn-ghost" style="font-size:0.72rem;color:var(--red)" disabled={opRunning} onclick={() => remove(pkg.name)}>✕</button>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

    <!-- ── Upgradable ── -->
    {:else if tab === 'upgradable'}
      <div style="margin-bottom:0.75rem;display:flex;align-items:center;gap:0.75rem">
        <span style="font-size:0.85rem;color:var(--text-secondary)">{upgradable.length} packages can be upgraded</span>
        <button class="btn btn-primary" disabled={opRunning||upgradable.length===0} onclick={() => upgrade()}>↑ Upgrade all</button>
      </div>
      <div class="card" style="padding:0">
        <table class="data-table">
          <thead><tr><th>Package</th><th>Installed</th><th>Available</th><th>Repo</th><th style="text-align:right">Upgrade</th></tr></thead>
          <tbody>
            {#if loadingTab === 'upgradable'}
              {#each [1,2,3] as _}<tr>{#each [1,2,3,4,5] as _}<td><div class="skeleton" style="height:13px;width:70%"></div></td>{/each}</tr>{/each}
            {:else if upgradable.length === 0}
              <tr><td colspan="5" style="text-align:center;padding:2rem;color:var(--text-tertiary)">All packages up to date ✓</td></tr>
            {:else}
              {#each upgradable as pkg, i (pkg.name + ':' + i)}
                <tr>
                  <td class="mono" style="font-weight:600">{pkg.name}</td>
                  <td class="mono" style="font-size:0.8rem;color:var(--text-tertiary)">{pkg.version||'—'}</td>
                  <td class="mono" style="font-size:0.8rem;color:var(--accent)">{pkg.new_version}</td>
                  <td style="font-size:0.78rem;color:var(--text-tertiary)">{pkg.repo||'—'}</td>
                  <td style="text-align:right">
                    <button class="btn btn-primary" style="font-size:0.72rem" disabled={opRunning} onclick={() => upgrade(pkg.name)}>↑</button>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

    <!-- ── Search ── -->
    {:else if tab === 'search'}
      <div style="margin-bottom:0.75rem;display:flex;gap:0.5rem">
        <input class="search-input" style="max-width:320px" type="search" placeholder="Search packages…" bind:value={searchQuery} onkeydown={(e) => e.key==='Enter' && doSearch()} />
        <button class="btn btn-primary" onclick={doSearch} disabled={searching||!searchQuery}>{searching?'Searching…':'Search'}</button>
      </div>
      {#if searchResults.length > 0}
        <div class="card" style="padding:0">
          <table class="data-table">
            <thead><tr><th>Package</th><th>Description</th><th style="text-align:right">Install</th></tr></thead>
            <tbody>
              {#each searchResults as pkg, i (pkg.name + ':' + i)}
                <tr>
                  <td class="mono" style="font-weight:600">{pkg.name}</td>
                  <td style="font-size:0.82rem;color:var(--text-secondary)">{pkg.description||'—'}</td>
                  <td style="text-align:right">
                    <button class="btn btn-primary" style="font-size:0.72rem" disabled={opRunning} onclick={() => install(pkg.name)}>+ Install</button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else if !searching}
        <div class="card" style="text-align:center;padding:2rem;color:var(--text-tertiary)">Enter a package name and press Search or Enter</div>
      {/if}

    <!-- ── Flatpak ── -->
    {:else if tab === 'flatpak'}
      <div style="margin-bottom:0.75rem;display:flex;align-items:center;gap:0.75rem">
        <span style="font-size:0.85rem;color:var(--text-secondary)">{flatpaks.length} Flatpaks installed</span>
        <button class="btn btn-primary" disabled={opRunning} onclick={updateFlatpaks}>↑ Update all Flatpaks</button>
      </div>
      <div class="card" style="padding:0">
        <table class="data-table">
          <thead><tr><th>Name</th><th>App ID</th><th>Version</th><th>Origin</th><th>Type</th><th style="text-align:right">Remove</th></tr></thead>
          <tbody>
            {#if loadingTab === 'flatpak'}
              {#each [1,2,3] as _}<tr>{#each [1,2,3,4,5,6] as _}<td><div class="skeleton" style="height:13px;width:70%"></div></td>{/each}</tr>{/each}
            {:else if flatpaks.length === 0}
              <tr><td colspan="6" style="text-align:center;padding:2rem;color:var(--text-tertiary)">No Flatpaks installed</td></tr>
            {:else}
              {#each flatpaks as app, i (app.app_id + ':' + i)}
                <tr>
                  <td style="font-weight:600">{app.name}</td>
                  <td class="mono" style="font-size:0.75rem;color:var(--text-secondary)">{app.app_id}</td>
                  <td class="mono" style="font-size:0.78rem">{app.version||'—'}</td>
                  <td style="font-size:0.78rem;color:var(--text-tertiary)">{app.origin}</td>
                  <td><span class="badge {app.install_type==='system'?'badge-blue':'badge-gray'}">{app.install_type}</span></td>
                  <td style="text-align:right">
                    <button class="btn btn-ghost" style="font-size:0.72rem;color:var(--red)" disabled={opRunning} onclick={() => removeFlatpak(app.app_id)}>✕</button>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

    <!-- ── Repos ── -->
    {:else if tab === 'repos'}
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.75rem">
        <span style="font-size:0.85rem;color:var(--text-secondary)">{repos.length} {family} repositories configured</span>
        <button class="btn btn-primary" onclick={() => { showAddRepo=true; addRepoError=''; }}>+ Add repo</button>
      </div>
      <div class="card" style="padding:0;margin-bottom:1rem">
        <table class="data-table">
          <thead><tr><th>Name</th><th>URL / Mirror</th><th>File</th><th>Enabled</th><th style="text-align:right">Remove</th></tr></thead>
          <tbody>
            {#if loadingTab === 'repos'}
              {#each [1,2,3] as _}<tr>{#each [1,2,3,4,5] as _}<td><div class="skeleton" style="height:13px;width:70%"></div></td>{/each}</tr>{/each}
            {:else if repos.length === 0}
              <tr><td colspan="5" style="text-align:center;padding:2rem;color:var(--text-tertiary)">No repositories found</td></tr>
            {:else}
              {#each repos as repo, i (repo.id + ':' + i)}
                <tr class:repo-disabled={!repo.enabled}>
                  <td>
                    <div class="mono" style="font-weight:600;font-size:0.85rem">{repo.name}</div>
                    {#if repo.section}<div style="font-size:0.7rem;color:var(--text-tertiary)">{repo.section}</div>{/if}
                  </td>
                  <td style="max-width:260px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:0.78rem;color:var(--text-secondary)" title={repo.url}>{repo.url||'—'}</td>
                  <td class="mono" style="font-size:0.7rem;color:var(--text-tertiary)">{repo.file}{repo.line?':'+repo.line:''}</td>
                  <td>
                    <button class="btn btn-ghost" style="font-size:0.75rem;min-width:72px" onclick={() => toggleRepo(repo)}>
                      {repo.enabled?'● Disable':'○ Enable'}
                    </button>
                  </td>
                  <td style="text-align:right">
                    <button class="btn btn-ghost" style="font-size:0.72rem;color:var(--red)" onclick={() => removeRepo(repo)}>✕</button>
                  </td>
                </tr>
              {/each}
            {/if}
          </tbody>
        </table>
      </div>

      {#if hasFlatpak}
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:0.5rem">
          <span style="font-size:0.85rem;font-weight:500;color:var(--text-secondary)">Flatpak Remotes</span>
          <button class="btn btn-ghost" onclick={() => showAddRemote=true}>+ Add remote</button>
        </div>
        <div class="card" style="padding:0;margin-bottom:1rem">
          <table class="data-table">
            <thead><tr><th>Name</th><th>URL</th><th>Type</th><th>Status</th><th style="text-align:right">Remove</th></tr></thead>
            <tbody>
              {#each flatpakRemotes as remote, i (remote.name + ':' + i)}
                <tr>
                  <td class="mono" style="font-weight:600">{remote.name}</td>
                  <td style="font-size:0.78rem;color:var(--text-secondary);max-width:220px;overflow:hidden;text-overflow:ellipsis">{remote.url}</td>
                  <td><span class="badge {remote.type==='system'?'badge-blue':'badge-gray'}">{remote.type}</span></td>
                  <td><span class="badge {remote.enabled?'badge-green':'badge-gray'}">{remote.enabled?'active':'disabled'}</span></td>
                  <td style="text-align:right">
                    <button class="btn btn-ghost" style="font-size:0.72rem;color:var(--red)" onclick={() => removeFlatpakRemote(remote.name)}>✕</button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}

      {#if showAddRepo}
        <div class="modal-overlay" role="button" tabindex="0" onkeydown={(e)=>e.key==='Escape'&&(showAddRepo=false)} onclick={()=>showAddRepo=false}>
          <div class="modal" role="dialog" tabindex="-1" onclick={(e)=>e.stopPropagation()}>
            <div class="modal-header">
              <h2>Add {family} repository</h2>
              <button class="btn btn-ghost" onclick={()=>showAddRepo=false}>✕</button>
            </div>
            <div class="modal-body">
              {#if addRepoError}<div class="alert alert-error" style="margin-bottom:0.75rem">{addRepoError}</div>{/if}
              <div class="form-row">
                <label for="repo-name">Name *</label>
                <input id="repo-name" class="search-input" bind:value={newRepo.name} placeholder="e.g. docker-ce" />
              </div>
              <div class="form-row">
                <label for="repo-url">URL *</label>
                <input id="repo-url" class="search-input mono" bind:value={newRepo.url} placeholder={family==='apt'?'deb https://example.com/repo suite component':'https://example.com/repo/$arch'} />
              </div>
              <div class="form-row">
                <label for="repo-key">GPG key URL (optional)</label>
                <input id="repo-key" class="search-input" bind:value={newRepo.key_url} placeholder="https://example.com/gpg.key" />
              </div>
              <div style="font-size:0.72rem;color:var(--text-tertiary)">
                {#if family==='apt'}Writes to /etc/apt/sources.list.d/{newRepo.name||'name'}.list{:else if family==='pacman'}Appends to /etc/pacman.conf{:else}Writes to /etc/yum.repos.d/{newRepo.name||'name'}.repo{/if}
              </div>
            </div>
            <div class="modal-footer">
              <button class="btn" onclick={()=>showAddRepo=false}>Cancel</button>
              <button class="btn btn-primary" onclick={addRepo} disabled={addingRepo}>{addingRepo?'Adding…':'Add repository'}</button>
            </div>
          </div>
        </div>
      {/if}

      {#if showAddRemote}
        <div class="modal-overlay" role="button" tabindex="0" onkeydown={(e)=>e.key==='Escape'&&(showAddRemote=false)} onclick={()=>showAddRemote=false}>
          <div class="modal" role="dialog" tabindex="-1" onclick={(e)=>e.stopPropagation()}>
            <div class="modal-header">
              <h2>Add Flatpak remote</h2>
              <button class="btn btn-ghost" onclick={()=>showAddRemote=false}>✕</button>
            </div>
            <div class="modal-body">
              <div class="form-row">
                <label for="rem-name">Name *</label>
                <input id="rem-name" class="search-input" bind:value={newRemote.name} placeholder="flathub" />
              </div>
              <div class="form-row">
                <label for="rem-url">URL *</label>
                <input id="rem-url" class="search-input mono" bind:value={newRemote.url} placeholder="https://dl.flathub.org/repo/flathub.flatpakrepo" />
              </div>
              <label style="display:flex;align-items:center;gap:0.5rem;font-size:0.82rem;cursor:pointer;margin-top:0.5rem">
                <input type="checkbox" bind:checked={newRemote.system} />
                System-wide (requires root)
              </label>
            </div>
            <div class="modal-footer">
              <button class="btn" onclick={()=>showAddRemote=false}>Cancel</button>
              <button class="btn btn-primary" onclick={addFlatpakRemote}>Add remote</button>
            </div>
          </div>
        </div>
      {/if}
    {/if}
  {/if}

  <CLIEchoPane context="packages" />
</div>

<style>
.pkg-page { max-width:1200px; padding-bottom:220px; }
.tab-bar { display:flex; border-bottom:1px solid var(--border-subtle); margin-bottom:0.75rem; }
.tab-btn { display:flex; align-items:center; gap:0.3rem; padding:0.5rem 1rem; background:none; border:none; border-bottom:2px solid transparent; cursor:pointer; font-size:0.85rem; color:var(--text-secondary); margin-bottom:-1px; }
.tab-btn.active { color:var(--accent); border-bottom-color:var(--accent); font-weight:500; }
.pkg-table { font-size:0.85rem; }
.op-panel { padding:0; overflow:hidden; margin-bottom:0.75rem; }
.op-header { display:flex; align-items:center; padding:0.4rem 0.75rem; background:var(--bg-raised); border-bottom:1px solid var(--border-subtle); font-size:0.78rem; }
.op-body { max-height:200px; overflow-y:auto; padding:0.5rem 0.75rem; background:var(--bg-base); }
.op-line { font-size:0.72rem; line-height:1.5; color:var(--text-secondary); white-space:pre-wrap; word-break:break-all; }
.op-line.line-ok { color:var(--accent); }
.op-line.line-err { color:var(--red); }
.repo-disabled td { opacity:0.45; }
.modal-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.6); display:flex; align-items:center; justify-content:center; z-index:500; }
.modal { background:var(--bg-panel); border:1px solid var(--border-default); border-radius:var(--r-lg); width:440px; max-width:95vw; }
.modal-header { display:flex; justify-content:space-between; align-items:center; padding:1rem; border-bottom:1px solid var(--border-subtle); }
.modal-header h2 { font-size:1rem; margin:0; }
.modal-body { padding:1rem; display:flex; flex-direction:column; gap:0.75rem; }
.modal-footer { display:flex; justify-content:flex-end; gap:0.5rem; padding:1rem; border-top:1px solid var(--border-subtle); }
.form-row { display:flex; flex-direction:column; gap:0.25rem; }
.form-row label { font-size:0.75rem; color:var(--text-secondary); font-weight:500; }
</style>
