<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface VHost { server_name:string; aliases:string[]; root:string; port:number; ssl:boolean; config_file:string; enabled:boolean; }
  interface Server { type:string; version:string; config_path:string; running:boolean; vhosts:VHost[]; }

  let servers: Server[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let activeServer = $state('');
  let configContent = $state('');
  let configPath = $state('');
  let configLoading = $state(false);
  let configSaving = $state(false);
  let configError = $state('');
  let testResult = $state('');
  let tab = $state<'sites'|'config'|'security'>('sites');

  interface SvcCheck { id: string; label: string; pass: boolean; detail: string; fix: string; severity: string; }
  let secChecks = $state<SvcCheck[]>([]);
  let secLoading = $state(false);
  let secError = $state('');

  async function loadSecurity(type: string) {
    secLoading = true; secError = '';
    try {
      const res = await fetch(`/api/webservers/security?type=${encodeURIComponent(type)}`);
      if (!res.ok) throw new Error(await res.text());
      const d = await res.json();
      secChecks = d.checks ?? [];
    } catch(e: any) { secError = e.message; }
    finally { secLoading = false; }
  }

  $effect(() => {
    if (tab === 'security' && activeServer) loadSecurity(activeServer);
  });

  async function load() {
    loading = true; error = '';
    try {
      const res = await api.get<any>('/api/webservers');
      servers = res.servers ?? [];
      if (servers.length && !activeServer) activeServer = servers[0].type;
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function loadConfig() {
    if (!activeServer) return;
    configLoading = true; configError = '';
    try {
      const res = await api.get<any>(`/api/webservers/${activeServer}/config`);
      configContent = res.content ?? '';
      configPath = res.path ?? '';
    } catch(e: any) { configError = e.message; }
    finally { configLoading = false; }
  }

  async function saveConfig() {
    configSaving = true; configError = '';
    try {
      await api.put(`/api/webservers/${activeServer}/config`, { content: configContent });
      testResult = '✓ Config saved and tested successfully';
    } catch(e: any) { configError = e.message; }
    finally { configSaving = false; }
  }

  async function testConfig() {
    testResult = '';
    try {
      const res = await api.post<any>(`/api/webservers/${activeServer}/test`, {});
      testResult = res.ok ? '✓ Config syntax OK' : '✗ ' + res.error;
    } catch(e: any) { testResult = '✗ ' + e.message; }
  }

  async function reload() {
    try {
      await api.post(`/api/webservers/${activeServer}/reload`, {});
      testResult = '✓ Reloaded successfully';
    } catch(e: any) { error = e.message; }
  }

  async function toggleSite(type: string, site: string, enabled: boolean) {
    const action = enabled ? 'disable' : 'enable';
    try {
      await api.post(`/api/webservers/${type}/sites/${site}/${action}`, {});
      await load();
    } catch(e: any) { error = e.message; }
  }

  $effect(() => { if (tab === 'config' && activeServer) loadConfig(); });

  let activeServerObj = $derived(servers.find(s => s.type === activeServer));

  onMount(load);
</script>

<div class="ws-page">
  <div class="page-header">
    <div><h1>Webservers</h1>
      <p class="subtitle">{servers.length} detected</p>
    </div>
    <button class="btn" onclick={load} disabled={loading}>⟳ Refresh</button>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  {#if loading}
    <div class="card skeleton" style="height:80px"></div>
  {:else if servers.length === 0}
    <div class="card" style="text-align:center;padding:3rem;color:var(--text-tertiary)">
      No webservers detected (Nginx, Apache, or Caddy)
    </div>
  {:else}
    <!-- Server selector -->
    <div class="server-tabs">
      {#each servers as s}
        <button class="server-tab" class:active={activeServer === s.type}
          onclick={() => activeServer = s.type}>
          <span class="dot {s.running ? 'dot-green' : 'dot-gray'}"></span>
          {s.type}
          {#if s.running}<span class="badge badge-green" style="font-size:0.65rem">running</span>{/if}
        </button>
      {/each}
    </div>

    {#if activeServerObj}
      <div class="card server-info">
        <div class="info-row">
          <span style="color:var(--text-secondary)">Version</span>
          <span class="mono">{activeServerObj.version}</span>
        </div>
        <div class="info-row">
          <span style="color:var(--text-secondary)">Config</span>
          <span class="mono">{activeServerObj.config_path}</span>
        </div>
        <div class="info-row">
          <span style="color:var(--text-secondary)">Virtual hosts</span>
          <span>{activeServerObj.vhosts?.length ?? 0}</span>
        </div>
        <div style="display:flex;gap:0.5rem;margin-top:0.5rem">
          <button class="btn" onclick={testConfig}>Test config</button>
          <button class="btn btn-primary" onclick={reload}>Reload</button>
        </div>
        {#if testResult}
          <div class="alert {testResult.startsWith('✓') ? 'alert-info' : 'alert-error'}" style="margin-top:0.5rem">
            {testResult}
          </div>
        {/if}
      </div>

      <!-- Sub-tabs -->
      <div class="tab-bar">
        <button class="tab-btn" class:active={tab==='sites'} onclick={() => tab='sites'}>
          Sites ({activeServerObj.vhosts?.length ?? 0})
        </button>
        <button class="tab-btn" class:active={tab==='config'} onclick={() => tab='config'}>
          Config editor
        </button>
        <button class="tab-btn" class:active={tab==='security'} onclick={() => tab='security'}>
          🔒 Security
        </button>
      </div>

      {#if tab === 'sites'}
        <div class="card" style="padding:0">
          <table class="data-table">
            <thead><tr><th>Domain</th><th>Root</th><th>Port</th><th>SSL</th><th>Enabled</th></tr></thead>
            <tbody>
              {#if !activeServerObj.vhosts?.length}
                <tr><td colspan="5" style="text-align:center;padding:2rem;color:var(--text-tertiary)">No virtual hosts found</td></tr>
              {:else}
                {#each activeServerObj.vhosts as vh}
                  <tr>
                    <td>
                      <div class="mono" style="font-weight:500">{vh.server_name || '—'}</div>
                      {#each (vh.aliases ?? []) as alias}
                        <span class="badge badge-gray" style="margin:1px;font-size:0.65rem">{alias}</span>
                      {/each}
                    </td>
                    <td class="mono" style="font-size:0.72rem;color:var(--text-secondary)">{vh.root || '—'}</td>
                    <td class="mono">{vh.port || '—'}</td>
                    <td>{#if vh.ssl}<span class="badge badge-green">SSL</span>{:else}—{/if}</td>
                    <td>
                      <button class="btn btn-ghost" style="font-size:0.72rem"
                        onclick={() => toggleSite(activeServer, vh.server_name, vh.enabled)}>
                        {vh.enabled ? '● Disable' : '○ Enable'}
                      </button>
                    </td>
                  </tr>
                {/each}
              {/if}
            </tbody>
          </table>
        </div>

      {:else if tab === 'security'}
        {#if secError}<div class="alert alert-error">{secError}</div>
        {:else if secLoading}
          <div class="card" style="padding:1.5rem;text-align:center;color:var(--text-tertiary)">Scanning config…</div>
        {:else if secChecks.length === 0}
          <div class="card" style="padding:1.5rem;text-align:center;color:var(--text-tertiary)">No checks available</div>
        {:else}
          <div class="sec-list card" style="padding:0">
            {#each secChecks as c (c.id)}
              <div class="sec-row" class:sec-pass={c.pass} class:sec-fail={!c.pass}>
                <span class="sec-icon">{c.pass ? '✓' : '✗'}</span>
                <div class="sec-body">
                  <div class="sec-label">{c.label}
                    <span class="badge badge-{c.severity === 'high' ? 'red' : c.severity === 'medium' ? 'yellow' : 'gray'}" style="margin-left:0.375rem;font-size:0.65rem">{c.severity}</span>
                  </div>
                  <div class="sec-detail">{c.pass ? c.detail : c.fix}</div>
                  {#if !c.pass && c.detail}<div class="sec-found">Found: {c.detail}</div>{/if}
                </div>
              </div>
            {/each}
          </div>
        {/if}

      {:else if tab === 'config'}
        {#if configError}<div class="alert alert-error" style="margin-bottom:0.5rem">{configError}</div>{/if}
        {#if configPath}<div style="font-size:0.72rem;color:var(--text-tertiary);margin-bottom:0.375rem">Editing: {configPath}</div>{/if}
        {#if configLoading}
          <div class="skeleton" style="height:300px;border-radius:var(--r-md)"></div>
        {:else}
          <textarea class="config-editor mono" bind:value={configContent} rows="20"></textarea>
          <div style="display:flex;gap:0.5rem;margin-top:0.5rem">
            <button class="btn" onclick={testConfig}>Test syntax</button>
            <button class="btn btn-primary" onclick={saveConfig} disabled={configSaving}>
              {configSaving ? 'Saving…' : 'Save & test'}
            </button>
          </div>
        {/if}
      {/if}
    {/if}
  {/if}

  <CLIEchoPane context="webservers" />
</div>

<style>
.ws-page { max-width:1100px; padding-bottom:220px; }
.server-tabs { display:flex; gap:0.5rem; margin-bottom:0.75rem; }
.server-tab { display:flex; align-items:center; gap:0.375rem; padding:0.5rem 1rem; background:var(--bg-raised); border:1px solid var(--border-default); border-radius:var(--r-md); cursor:pointer; font-size:0.85rem; color:var(--text-secondary); }
.server-tab.active { border-color:var(--accent); color:var(--accent); background:var(--accent-dim); }
.server-info { padding:1rem; margin-bottom:0.75rem; }
.info-row { display:flex; gap:1rem; font-size:0.82rem; margin-bottom:0.25rem; }
.info-row span:first-child { min-width:100px; }
.tab-bar { display:flex; border-bottom:1px solid var(--border-subtle); margin-bottom:0.75rem; }
.tab-btn { padding:0.5rem 1rem; background:none; border:none; border-bottom:2px solid transparent; cursor:pointer; font-size:0.85rem; color:var(--text-secondary); margin-bottom:-1px; }
.tab-btn.active { color:var(--accent); border-bottom-color:var(--accent); font-weight:500; }
.config-editor { width:100%; padding:0.75rem; background:var(--bg-base); border:1px solid var(--border-default); border-radius:var(--r-md); color:var(--text-primary); font-size:0.75rem; line-height:1.5; resize:vertical; min-height:300px; }
.config-editor:focus { outline:none; border-color:var(--accent); }
.sec-row { display:flex; align-items:flex-start; gap:0.75rem; padding:0.75rem 1rem; border-bottom:1px solid var(--border-subtle); }
.sec-row:last-child { border-bottom:none; }
.sec-icon { font-size:0.85rem; font-weight:700; width:16px; flex-shrink:0; margin-top:0.1rem; }
.sec-pass .sec-icon { color:var(--accent); }
.sec-fail .sec-icon { color:#f87171; }
.sec-body { flex:1; min-width:0; }
.sec-label { font-size:0.82rem; font-weight:500; color:var(--text-primary); display:flex; align-items:center; flex-wrap:wrap; gap:0.25rem; }
.sec-fail .sec-label { color:#f87171; }
.sec-detail { font-size:0.74rem; color:var(--text-secondary); margin-top:0.2rem; }
.sec-found { font-size:0.7rem; color:var(--text-tertiary); margin-top:0.15rem; font-style:italic; }
</style>
