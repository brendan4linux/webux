<script lang="ts">
  import { onMount } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface DBInstance {
    driver: string; host: string; port: number;
    data_dir: string; version: string; socket: string;
    running: boolean; can_query: boolean;
  }

  let instances: DBInstance[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let connections: Record<string, boolean> = $state({});
  let activeConn = $state('');
  let password = $state('');
  let connecting = $state(false);
  let databases: string[] = $state([]);
  let tables: string[] = $state([]);
  let selectedDB = $state('');
  let queryText = $state('SELECT 1;');
  let queryResult: any = $state(null);
  let queryError = $state('');
  let querying = $state(false);

  async function load() {
    loading = true; error = '';
    try {
      const res = await api.get<any>('/api/databases');
      instances = res.instances ?? [];
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  async function connect(inst: DBInstance) {
    connecting = true; error = '';
    try {
      const res = await api.post<any>('/api/databases/connect', {
        driver: inst.driver,
        host: inst.host || '127.0.0.1',
        port: inst.port,
        socket: inst.socket,
        password,
      });
      if (res.ok) {
        connections = { ...connections, [inst.driver]: true };
        activeConn = inst.driver;
        password = '';
        await loadDatabases();
      } else {
        error = res.error ?? 'Connection failed';
      }
    } catch(e: any) { error = e.message; }
    finally { connecting = false; }
  }

  async function loadDatabases() {
    if (!activeConn) return;
    const res = await api.get<any>(`/api/databases/${activeConn}/databases`);
    databases = res.databases ?? [];
    if (databases.length) { selectedDB = databases[0]; await loadTables(); }
  }

  async function loadTables() {
    if (!activeConn || !selectedDB) return;
    const res = await api.get<any>(`/api/databases/${activeConn}/tables?db=${selectedDB}`);
    tables = res.tables ?? [];
  }

  async function runQuery() {
    querying = true; queryResult = null; queryError = '';
    try {
      const res = await api.post<any>(`/api/databases/${activeConn}/query`,
        { database: selectedDB, sql: queryText });
      if (res.error) queryError = res.error;
      else queryResult = res;
    } catch(e: any) { queryError = e.message; }
    finally { querying = false; }
  }

  async function disconnect() {
    if (!activeConn) return;
    await api.delete(`/api/databases/${activeConn}`);
    const { [activeConn]: _, ...rest } = connections;
    connections = rest;
    activeConn = '';
    databases = []; tables = []; queryResult = null;
  }

  function driverIcon(d: string) {
    return {mysql:'🐬', postgres:'🐘', redis:'🔴', mongodb:'🍃'}[d] ?? '🗄';
  }

  onMount(load);
</script>

<div class="db-page">
  <div class="page-header">
    <div><h1>Databases</h1>
      <p class="subtitle">{instances.length} detected</p>
    </div>
    <button class="btn" onclick={load} disabled={loading}>⟳ Refresh</button>
  </div>

  {#if error}<div class="alert alert-error" style="margin-bottom:1rem">{error}</div>{/if}

  <div class="db-layout">
    <!-- Left: detected instances -->
    <div class="db-instances">
      {#if loading}
        {#each [1,2] as _}<div class="card skeleton" style="height:80px"></div>{/each}
      {:else if instances.length === 0}
        <div class="card" style="text-align:center;padding:2rem;color:var(--text-tertiary)">
          No databases detected on well-known ports (3306, 5432, 6379, 27017)
        </div>
      {:else}
        {#each instances as inst}
          <div class="card inst-card" class:active={activeConn === inst.driver}>
            <div class="inst-header">
              <span class="inst-icon">{driverIcon(inst.driver)}</span>
              <div>
                <div class="mono" style="font-weight:600">{inst.driver}</div>
                <div style="font-size:0.72rem;color:var(--text-tertiary)">
                  {inst.socket || inst.host + ':' + inst.port}
                </div>
              </div>
              <div style="margin-left:auto">
                {#if connections[inst.driver]}
                  <span class="badge badge-green">Connected</span>
                {:else if !inst.can_query}
                  <span class="badge badge-gray" title="Rebuild with -tags {inst.driver}">No driver</span>
                {:else}
                  <span class="badge {inst.running ? 'badge-yellow' : 'badge-gray'}">
                    {inst.running ? 'Detected' : 'Offline'}
                  </span>
                {/if}
              </div>
            </div>
            <div style="font-size:0.72rem;color:var(--text-tertiary);margin-top:0.25rem">
              {inst.version}
            </div>
            {#if !connections[inst.driver] && inst.can_query && inst.running}
              <div style="margin-top:0.5rem;display:flex;gap:0.375rem">
                <input class="search-input" type="password" bind:value={password}
                  placeholder="Password (blank if none)" style="flex:1;font-size:0.78rem" />
                <button class="btn btn-primary" style="font-size:0.75rem"
                  disabled={connecting} onclick={() => connect(inst)}>
                  {connecting ? '…' : 'Connect'}
                </button>
              </div>
            {:else if connections[inst.driver]}
              <button class="btn btn-ghost" style="margin-top:0.5rem;font-size:0.72rem;color:var(--red)"
                onclick={disconnect}>Disconnect</button>
            {:else if !inst.can_query}
              <div style="margin-top:0.375rem;font-size:0.7rem;color:var(--text-tertiary)">
                Rebuild with <code>-tags {inst.driver}</code> to enable queries
              </div>
            {/if}
          </div>
        {/each}
      {/if}
    </div>

    <!-- Right: query panel -->
    {#if activeConn}
      <div class="query-panel">
        <!-- DB + table selector -->
        <div class="selector-bar">
          <select class="search-input" bind:value={selectedDB} onchange={loadTables}>
            {#each databases as db}
              <option value={db}>{db}</option>
            {/each}
          </select>
          <div class="table-list">
            {#each tables as t}
              <button class="table-pill" onclick={() => queryText = `SELECT * FROM ${t} LIMIT 50;`}>
                {t}
              </button>
            {/each}
          </div>
        </div>

        <!-- Query editor -->
        <textarea class="query-editor mono" bind:value={queryText} rows="5"
          placeholder="SELECT * FROM table LIMIT 50;"></textarea>

        <div style="display:flex;gap:0.5rem;margin-bottom:0.75rem">
          <button class="btn btn-primary" onclick={runQuery} disabled={querying}>
            {querying ? 'Running…' : '▶ Run query'}
          </button>
          <span style="font-size:0.72rem;color:var(--text-tertiary);align-self:center">
            Read-only: SELECT, SHOW, EXPLAIN, DESCRIBE
          </span>
        </div>

        {#if queryError}
          <div class="alert alert-error">{queryError}</div>
        {:else if queryResult}
          <div class="result-wrap">
            <div style="font-size:0.72rem;color:var(--text-tertiary);margin-bottom:0.375rem">
              {queryResult.row_count} rows
            </div>
            <div style="overflow-x:auto">
              <table class="data-table result-table">
                <thead>
                  <tr>{#each (queryResult.columns ?? []) as col}<th>{col}</th>{/each}</tr>
                </thead>
                <tbody>
                  {#each (queryResult.rows ?? []) as row}
                    <tr>{#each row as cell}<td class="mono">{cell ?? 'NULL'}</td>{/each}</tr>
                  {/each}
                </tbody>
              </table>
            </div>
          </div>
        {/if}
      </div>
    {:else if instances.length > 0 && Object.keys(connections).length === 0}
      <div class="card" style="flex:1;display:flex;align-items:center;justify-content:center;color:var(--text-tertiary)">
        Connect to a database to run queries
      </div>
    {/if}
  </div>

  <CLIEchoPane context="databases" />
</div>

<style>
.db-page { max-width:1200px; padding-bottom:220px; }
.db-layout { display:flex; gap:1rem; align-items:flex-start; }
.db-instances { width:280px; flex-shrink:0; display:flex; flex-direction:column; gap:0.5rem; }
.inst-card { padding:0.875rem; cursor:default; }
.inst-card.active { border-color:var(--accent); }
.inst-header { display:flex; align-items:center; gap:0.625rem; }
.inst-icon { font-size:1.4rem; }
.query-panel { flex:1; min-width:0; display:flex; flex-direction:column; gap:0.5rem; }
.selector-bar { display:flex; gap:0.5rem; flex-wrap:wrap; align-items:center; }
.table-list { display:flex; flex-wrap:wrap; gap:0.25rem; }
.table-pill { padding:0.15rem 0.5rem; background:var(--bg-raised); border:1px solid var(--border-default); border-radius:var(--r-sm); font-size:0.72rem; font-family:var(--font-mono); cursor:pointer; color:var(--text-secondary); }
.table-pill:hover { border-color:var(--accent); color:var(--accent); }
.query-editor { width:100%; padding:0.75rem; background:var(--bg-base); border:1px solid var(--border-default); border-radius:var(--r-md); color:var(--text-primary); font-size:0.8rem; resize:vertical; min-height:80px; }
.query-editor:focus { outline:none; border-color:var(--accent); }
.result-wrap { border:1px solid var(--border-subtle); border-radius:var(--r-md); overflow:hidden; }
.result-table { font-size:0.75rem; }
.result-table td { max-width:200px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
</style>
