<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface Entry {
    name:string; path:string; is_dir:boolean; size:number;
    mode:string; mode_octal:string; owner:string; group:string;
    mod_time:string; mime_type:string; is_symlink:boolean;
    symlink_target?:string;
  }

  let entries: Entry[] = $state([]);
  let currentPath = $state('/');
  let loading = $state(false);
  let error = $state('');
  let selectedEntry = $state<Entry|null>(null);
  let fileContent = $state('');
  let fileLoading = $state(false);
  let fileSaving = $state(false);
  let fileError = $state('');
  let view = $state<'browser'|'editor'>('browser');
  let showNewDir = $state(false);
  let newDirName = $state('');
  let showChmod = $state(false);
  let chmodTarget = $state<Entry|null>(null);
  let chmodValue = $state('644');

  async function browse(path: string) {
    loading = true; error = ''; selectedEntry = null; view = 'browser';
    try {
      const res = await api.get<any>(`/api/files?path=${encodeURIComponent(path)}`);
      entries = res.entries ?? [];
      currentPath = res.path ?? path;
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function openFile(entry: Entry) {
    if (entry.is_dir) { browse(entry.path); return; }
    selectedEntry = entry;
    fileLoading = true; fileError = ''; view = 'editor';
    try {
      const res = await api.get<any>(`/api/files/read?path=${encodeURIComponent(entry.path)}`);
      fileContent = res.content ?? '';
    } catch(e: any) { fileError = e.message; }
    finally { fileLoading = false; }
  }

  let fileSaved = $state(false);

  async function saveFile() {
    if (!selectedEntry) return;
    fileSaving = true; fileError = ''; fileSaved = false;
    try {
      await api.put('/api/files/write', { path: selectedEntry.path, content: fileContent });
      fileSaved = true;
      setTimeout(() => fileSaved = false, 2500);
    } catch(e: any) { fileError = e.message; }
    finally { fileSaving = false; }
  }

  async function deleteEntry(entry: Entry) {
    if (!confirm(`Delete ${entry.name}?`)) return;
    try {
      await api.delete(`/api/files?path=${encodeURIComponent(entry.path)}`);
      await browse(currentPath);
    } catch(e: any) { error = e.message; }
  }

  async function mkdir() {
    if (!newDirName) return;
    const path = currentPath.replace(/\/$/, '') + '/' + newDirName;
    try {
      await api.post('/api/files/mkdir', { path });
      showNewDir = false; newDirName = '';
      await browse(currentPath);
    } catch(e: any) { error = e.message; }
  }

  async function applyChmod() {
    if (!chmodTarget) return;
    try {
      await api.post('/api/files/chmod', { path: chmodTarget.path, mode: chmodValue });
      showChmod = false;
      await browse(currentPath);
    } catch(e: any) { error = e.message; }
  }

  function breadcrumbs(): {label:string, path:string}[] {
    const parts = currentPath.split('/').filter(Boolean);
    const crumbs = [{ label: '/', path: '/' }];
    let acc = '';
    for (const p of parts) {
      acc += '/' + p;
      crumbs.push({ label: p, path: acc });
    }
    return crumbs;
  }

  function fmtSize(b: number) {
    if (b > 1e9) return (b/1e9).toFixed(1)+'GB';
    if (b > 1e6) return (b/1e6).toFixed(1)+'MB';
    if (b > 1e3) return (b/1e3).toFixed(1)+'KB';
    return b+'B';
  }

  function fileIcon(entry: Entry) {
    if (entry.is_dir) return '📁';
    if (entry.is_symlink) return '🔗';
    const ext = entry.name.split('.').pop()?.toLowerCase() ?? '';
    if (['js','ts','py','go','rs','cpp','c','java'].includes(ext)) return '📄';
    if (['json','yaml','yml','toml','xml'].includes(ext)) return '⚙';
    if (['sh','bash','zsh','fish'].includes(ext)) return '▶';
    if (['log','txt'].includes(ext)) return '📋';
    if (['jpg','jpeg','png','gif','svg','webp'].includes(ext)) return '🖼';
    return '📄';
  }

  onMount(() => browse('/'));
</script>

<div class="files-page">
  <div class="page-header">
    <div>
      <h1>Files</h1>
      <div class="breadcrumbs">
        {#each breadcrumbs() as crumb, i}
          {#if i > 0}<span style="color:var(--text-tertiary)"> / </span>{/if}
          <button class="crumb" onclick={() => browse(crumb.path)}>{crumb.label}</button>
        {/each}
      </div>
    </div>
    <div class="actions">
      {#if view === 'editor' && selectedEntry}
        <button class="btn" onclick={() => { view = 'browser'; selectedEntry = null; }}>← Back</button>
        <button class="btn btn-primary" onclick={saveFile} disabled={fileSaving}
        style={fileSaved ? 'background:var(--green);border-color:var(--green)' : ''}>
        {fileSaving ? 'Saving…' : fileSaved ? '✓ Saved' : '💾 Save'}
      </button>
      {:else}
        <button class="btn" onclick={() => showNewDir = true}>+ New folder</button>
        <button class="btn" onclick={() => browse(currentPath)} disabled={loading}>⟳ Refresh</button>
      {/if}
    </div>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:0.75rem">{error}</div>{/if}

  {#if view === 'browser'}
    {#if showNewDir}
      <div class="new-dir-bar">
        <input class="search-input" bind:value={newDirName} placeholder="Folder name"
          onkeydown={(e) => e.key === 'Enter' && mkdir()} />
        <button class="btn btn-primary" onclick={mkdir}>Create</button>
        <button class="btn btn-ghost" onclick={() => { showNewDir=false; newDirName=''; }}>Cancel</button>
      </div>
    {/if}

    <div class="card" style="padding:0;overflow-x:auto">
      <table class="data-table files-table">
        <thead>
          <tr><th>Name</th><th>Size</th><th>Mode</th><th>Owner</th><th>Modified</th><th></th></tr>
        </thead>
        <tbody>
          {#if loading}
            {#each [1,2,3,4,5] as _}
              <tr>{#each [1,2,3,4,5,6] as _}<td><div class="skeleton" style="height:13px;width:80%"></div></td>{/each}</tr>
            {/each}
          {:else if entries.length === 0}
            <tr><td colspan="6" style="text-align:center;padding:2rem;color:var(--text-tertiary)">Empty directory</td></tr>
          {:else}
            {#if currentPath !== '/'}
              <tr class="file-row" onclick={() => browse(currentPath.split('/').slice(0,-1).join('/') || '/')}>
                <td colspan="6" style="color:var(--text-tertiary);font-family:var(--font-mono)">
                  📁 ..
                </td>
              </tr>
            {/if}
            {#each entries as entry (entry.path)}
              <tr class="file-row" onclick={() => openFile(entry)}>
                <td>
                  <span style="margin-right:0.375rem">{fileIcon(entry)}</span>
                  <span class="mono" style="font-weight:{entry.is_dir?'600':'400'}">{entry.name}</span>
                  {#if entry.symlink_target}
                    <span style="font-size:0.68rem;color:var(--text-tertiary)"> → {entry.symlink_target}</span>
                  {/if}
                </td>
                <td class="mono" style="font-size:0.72rem;color:var(--text-tertiary)">
                  {entry.is_dir ? '—' : fmtSize(entry.size)}
                </td>
                <td class="mono" style="font-size:0.72rem">{entry.mode_octal}</td>
                <td style="font-size:0.75rem;color:var(--text-secondary)">{entry.owner}:{entry.group}</td>
                <td style="font-size:0.72rem;color:var(--text-tertiary)">
                  {new Date(entry.mod_time).toLocaleString()}
                </td>
                <td onclick={(e) => e.stopPropagation()}>
                  <div style="display:flex;gap:0.25rem;justify-content:flex-end">
                    <button class="btn btn-ghost" style="font-size:0.68rem"
                      onclick={() => { chmodTarget=entry; chmodValue=entry.mode_octal.replace(/^0/,''); showChmod=true; }}>
                      chmod
                    </button>
                    {#if !entry.is_dir}
                      <a class="btn btn-ghost" style="font-size:0.68rem"
                        href={`/api/files/download?path=${encodeURIComponent(entry.path)}`}>↓</a>
                    {/if}
                    <button class="btn btn-ghost" style="font-size:0.68rem;color:var(--red)"
                      onclick={() => deleteEntry(entry)}>✕</button>
                  </div>
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>

    <!-- Chmod modal -->
    {#if showChmod && chmodTarget}
      <div class="modal-overlay" role="button" tabindex="0"
        onkeydown={(e) => e.key === 'Escape' && (showChmod = false)}
        onclick={() => showChmod = false}>
        <div class="modal" role="dialog" tabindex="-1" onclick={(e) => e.stopPropagation()}>
          <div class="modal-header">
            <h2>chmod {chmodTarget.name}</h2>
            <button class="btn btn-ghost" onclick={() => showChmod = false}>✕</button>
          </div>
          <div class="modal-body">
            <label for="chmod-val" style="font-size:0.75rem;color:var(--text-secondary)">Octal mode</label>
            <input id="chmod-val" class="search-input" bind:value={chmodValue} placeholder="755" />
            <div style="font-size:0.72rem;color:var(--text-tertiary);margin-top:0.375rem">
              Current: {chmodTarget.mode_octal} ({chmodTarget.mode})
            </div>
          </div>
          <div class="modal-footer">
            <button class="btn" onclick={() => showChmod = false}>Cancel</button>
            <button class="btn btn-primary" onclick={applyChmod}>Apply</button>
          </div>
        </div>
      </div>
    {/if}

  {:else}
    <!-- Editor view -->
    {#if fileError}<div class="alert alert-error" style="margin-bottom:0.5rem">{fileError}</div>{/if}
    {#if fileLoading}
      <div class="skeleton" style="height:400px;border-radius:var(--r-md)"></div>
    {:else}
      <div style="font-size:0.72rem;color:var(--text-tertiary);margin-bottom:0.375rem">
        {selectedEntry?.path}
      </div>
      <textarea class="file-editor mono" bind:value={fileContent} spellcheck="false"></textarea>
    {/if}
  {/if}

  <CLIEchoPane context="files" />
</div>

<style>
.files-page { max-width:1200px; padding-bottom:220px; }
.breadcrumbs { display:flex; align-items:center; gap:0; font-size:0.78rem; margin-top:0.25rem; }
.crumb { background:none; border:none; cursor:pointer; color:var(--accent); font-family:var(--font-mono); font-size:0.78rem; padding:0; }
.crumb:hover { color:var(--accent-hover); }
.new-dir-bar { display:flex; gap:0.5rem; margin-bottom:0.75rem; }
.files-table { font-size:0.8rem; }
.file-row { cursor:pointer; }
.file-row:hover { background:var(--bg-hover); }
.file-editor { width:100%; min-height:500px; padding:0.875rem; background:var(--bg-base); border:1px solid var(--border-default); border-radius:var(--r-md); color:var(--text-primary); font-size:0.78rem; line-height:1.6; resize:vertical; }
.file-editor:focus { outline:none; border-color:var(--accent); }
.modal-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.6); display:flex; align-items:center; justify-content:center; z-index:500; }
.modal { background:var(--bg-panel); border:1px solid var(--border-default); border-radius:var(--r-lg); width:360px; max-width:95vw; }
.modal-header { display:flex; justify-content:space-between; align-items:center; padding:1rem; border-bottom:1px solid var(--border-subtle); }
.modal-header h2 { font-size:1rem; margin:0; }
.modal-body { padding:1rem; display:flex; flex-direction:column; gap:0.375rem; }
.modal-footer { display:flex; justify-content:flex-end; gap:0.5rem; padding:1rem; border-top:1px solid var(--border-subtle); }
</style>
