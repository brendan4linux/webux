<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte';

  // ── Types ──────────────────────────────────────────────────────────────────
  interface LogEntry { name: string; path: string; size: number; mod_time: string; is_dir: boolean; }
  interface Unit     { name: string; description: string; active: string; }
  type Source        = { kind: 'file'; path: string; label: string }
                     | { kind: 'unit'; name: string; label: string }
                     | null;

  // ── State ──────────────────────────────────────────────────────────────────
  let rootEntries:    LogEntry[]  = $state([]);
  let expanded:       Set<string> = $state(new Set());
  let subEntries:     Map<string, LogEntry[]> = $state(new Map());
  let loadingDirs:    Set<string> = $state(new Set());
  let units:          Unit[]      = $state([]);
  let unitFilter:     string      = $state('');
  let activeSource:   Source      = $state(null);

  let lines:          string[]    = $state([]);
  let following:      boolean     = $state(false);
  let loadingContent: boolean     = $state(false);
  let autoScroll:     boolean     = $state(true);
  let filter:         string      = $state('');
  let error:          string      = $state('');

  let logEl: HTMLElement | null   = $state(null);
  let eventSource: EventSource | null = null;

  const MAX_LINES = 2000;

  // ── Derived ────────────────────────────────────────────────────────────────
  let filteredLines = $derived(
    filter.trim()
      ? lines.filter(l => l.toLowerCase().includes(filter.toLowerCase()))
      : lines
  );

  let filteredUnits = $derived(
    unitFilter.trim()
      ? units.filter(u =>
          u.name.toLowerCase().includes(unitFilter.toLowerCase()) ||
          u.description.toLowerCase().includes(unitFilter.toLowerCase()))
      : units
  );

  // ── Lifecycle ──────────────────────────────────────────────────────────────
  onMount(async () => {
    const [entriesRes, unitsRes] = await Promise.all([
      fetch('/api/logs/files').then(r => r.ok ? r.json() : null).catch(() => null),
      fetch('/api/logs/systemd/units').then(r => r.ok ? r.json() : null).catch(() => null),
    ]);
    if (entriesRes?.entries) rootEntries = entriesRes.entries;
    if (unitsRes?.units)    units = unitsRes.units;
  });

  onDestroy(() => closeSSE());

  // ── Directory tree ─────────────────────────────────────────────────────────
  async function toggleDir(path: string) {
    if (expanded.has(path)) {
      expanded.delete(path);
      expanded = new Set(expanded);
      return;
    }
    if (!subEntries.has(path)) {
      loadingDirs.add(path);
      loadingDirs = new Set(loadingDirs);
      const res = await fetch(`/api/logs/files?path=${encodeURIComponent(path)}`)
        .then(r => r.ok ? r.json() : null).catch(() => null);
      if (res?.entries) {
        subEntries.set(path, res.entries);
        subEntries = new Map(subEntries);
      }
      loadingDirs.delete(path);
      loadingDirs = new Set(loadingDirs);
    }
    expanded.add(path);
    expanded = new Set(expanded);
  }

  // ── Open a file or unit ────────────────────────────────────────────────────
  async function openFile(entry: LogEntry) {
    if (entry.is_dir) { toggleDir(entry.path); return; }
    closeSSE();
    following = false;
    filter = '';
    error = '';
    activeSource = { kind: 'file', path: entry.path, label: entry.path };
    await loadTail(entry.path);
  }

  async function openUnit(unit: Unit) {
    closeSSE();
    following = false;
    filter = '';
    error = '';
    activeSource = { kind: 'unit', name: unit.name, label: unit.name + '.service' };
    await loadUnitTail(unit.name);
  }

  async function loadTail(path: string) {
    loadingContent = true;
    lines = [];
    try {
      const res = await fetch(`/api/logs/read?path=${encodeURIComponent(path)}&lines=500`);
      if (!res.ok) { error = await res.text(); return; }
      const data = await res.json();
      lines = data.lines ?? [];
      await scrollToBottom();
    } catch(e: any) { error = e.message; }
    finally { loadingContent = false; }
  }

  async function loadUnitTail(unit: string) {
    // For systemd units, just start following immediately (no separate read endpoint)
    lines = [];
    startFollow();
  }

  // ── Live follow ────────────────────────────────────────────────────────────
  function toggleFollow() {
    if (following) {
      closeSSE();
      following = false;
    } else {
      startFollow();
    }
  }

  function startFollow() {
    if (!activeSource) return;
    closeSSE();

    let url: string;
    if (activeSource.kind === 'file') {
      url = `/api/logs/follow?path=${encodeURIComponent(activeSource.path)}`;
    } else {
      url = `/api/logs/systemd/follow?unit=${encodeURIComponent(activeSource.name)}`;
    }

    following = true;
    eventSource = new EventSource(url);
    eventSource.onmessage = async (e) => {
      lines = [...lines, e.data].slice(-MAX_LINES);
      if (autoScroll) await scrollToBottom();
    };
    eventSource.onerror = () => {
      following = false;
      closeSSE();
    };
  }

  function closeSSE() {
    if (eventSource) { eventSource.close(); eventSource = null; }
  }

  async function scrollToBottom() {
    await tick();
    if (logEl) logEl.scrollTop = logEl.scrollHeight;
  }

  // ── Helpers ────────────────────────────────────────────────────────────────
  function fmtSize(b: number): string {
    if (b < 1024) return b + ' B';
    if (b < 1024*1024) return (b/1024).toFixed(1) + ' KB';
    return (b/1024/1024).toFixed(1) + ' MB';
  }

  function colorLine(line: string): string {
    const l = line.toLowerCase();
    if (l.includes('error') || l.includes('fatal') || l.includes('crit') || l.includes('emerg')) return 'line-error';
    if (l.includes('warn')) return 'line-warn';
    if (l.includes('notice') || l.includes('info')) return 'line-info';
    if (l.includes('debug')) return 'line-debug';
    return '';
  }

  function unitColor(active: string): string {
    if (active === 'active')   return 'var(--green)';
    if (active === 'failed')   return 'var(--red)';
    if (active === 'inactive') return 'var(--text-tertiary)';
    return 'var(--text-secondary)';
  }
</script>

<div class="logs-layout">
  <!-- ── Left sidebar ─────────────────────────────────────────────────────── -->
  <aside class="logs-sidebar">
    <!-- /var/log tree -->
    <div class="sidebar-section">
      <div class="sidebar-section-header">/var/log</div>
      <div class="tree">
        {#each rootEntries as entry (entry.path)}
          <div class="tree-item" class:active={activeSource?.kind === 'file' && activeSource.path === entry.path}>
            {#if entry.is_dir}
              <button class="tree-btn dir-btn" onclick={() => toggleDir(entry.path)}>
                <span class="tree-icon">{expanded.has(entry.path) ? '▾' : '▸'}</span>
                <span class="tree-name">{entry.name}/</span>
                {#if loadingDirs.has(entry.path)}<span class="tree-spin">…</span>{/if}
              </button>
              {#if expanded.has(entry.path) && subEntries.has(entry.path)}
                <div class="tree-children">
                  {#each subEntries.get(entry.path)! as child (child.path)}
                    <div class="tree-item child-item" class:active={activeSource?.kind === 'file' && activeSource.path === child.path}>
                      {#if child.is_dir}
                        <button class="tree-btn dir-btn" onclick={() => toggleDir(child.path)}>
                          <span class="tree-icon">{expanded.has(child.path) ? '▾' : '▸'}</span>
                          <span class="tree-name">{child.name}/</span>
                        </button>
                        {#if expanded.has(child.path) && subEntries.has(child.path)}
                          <div class="tree-children">
                            {#each subEntries.get(child.path)! as gc (gc.path)}
                              {#if !gc.is_dir}
                                <button class="tree-btn file-btn grandchild" onclick={() => openFile(gc)}
                                  class:active={activeSource?.kind === 'file' && activeSource.path === gc.path}>
                                  <span class="tree-name">{gc.name}</span>
                                  <span class="tree-meta">{fmtSize(gc.size)}</span>
                                </button>
                              {/if}
                            {/each}
                          </div>
                        {/if}
                      {:else}
                        <button class="tree-btn file-btn" onclick={() => openFile(child)}>
                          <span class="tree-name">{child.name}</span>
                          <span class="tree-meta">{fmtSize(child.size)}</span>
                        </button>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
            {:else}
              <button class="tree-btn file-btn" onclick={() => openFile(entry)}>
                <span class="tree-name">{entry.name}</span>
                <span class="tree-meta">{fmtSize(entry.size)}</span>
              </button>
            {/if}
          </div>
        {/each}
      </div>
    </div>

    <!-- Systemd units -->
    {#if units.length > 0}
      <div class="sidebar-section">
        <div class="sidebar-section-header">Systemd</div>
        <div class="unit-filter-wrap">
          <input class="unit-filter" placeholder="filter units…" bind:value={unitFilter} />
        </div>
        <div class="unit-list">
          {#each filteredUnits as unit (unit.name)}
            <button class="unit-btn"
              class:active={activeSource?.kind === 'unit' && activeSource.name === unit.name}
              onclick={() => openUnit(unit)}>
              <span class="unit-dot" style="background:{unitColor(unit.active)}"></span>
              <span class="unit-name">{unit.name}</span>
            </button>
          {/each}
        </div>
      </div>
    {/if}
  </aside>

  <!-- ── Main viewer ──────────────────────────────────────────────────────── -->
  <main class="logs-main">
    {#if !activeSource}
      <div class="logs-empty">
        <div class="empty-icon">◫</div>
        <div class="empty-title">Select a log file or service</div>
        <div class="empty-sub">Browse /var/log on the left, or pick a systemd unit to follow its journal.</div>
      </div>
    {:else}
      <!-- Toolbar -->
      <div class="log-toolbar">
        <div class="log-title">
          {#if activeSource.kind === 'unit'}
            <span class="log-title-badge">journal</span>
          {/if}
          <span class="log-title-path">{activeSource.label}</span>
        </div>
        <div class="toolbar-actions">
          <input class="filter-input" placeholder="filter lines…" bind:value={filter} />
          <label class="autoscroll-label">
            <input type="checkbox" bind:checked={autoScroll} />
            auto-scroll
          </label>
          <button class="btn btn-sm"
            class:btn-follow-active={following}
            onclick={toggleFollow}>
            {following ? '⏹ Stop' : '▶ Follow'}
          </button>
          {#if following}
            <span class="follow-indicator">● live</span>
          {/if}
        </div>
      </div>

      <!-- Log content -->
      {#if loadingContent}
        <div class="log-loading">Loading…</div>
      {:else if error}
        <div class="log-error">{error}</div>
      {:else}
        <div class="log-viewer" bind:this={logEl}
          onscroll={() => {
            if (logEl) {
              const atBottom = logEl.scrollHeight - logEl.scrollTop - logEl.clientHeight < 40;
              autoScroll = atBottom;
            }
          }}>
          {#if filteredLines.length === 0}
            <div class="log-empty-msg">{filter ? 'No lines match filter.' : 'No output yet — click Follow to stream.'}</div>
          {:else}
            {#each filteredLines as line, i (i)}
              <div class="log-line {colorLine(line)}">{line}</div>
            {/each}
          {/if}
        </div>
        <div class="log-footer">
          {filteredLines.length}{filter ? ` of ${lines.length}` : ''} lines
          {#if lines.length >= MAX_LINES}<span class="capped"> · capped at {MAX_LINES}</span>{/if}
        </div>
      {/if}
    {/if}
  </main>
</div>

<style>
/* ── Layout ── */
.logs-layout {
  display: flex;
  height: calc(100vh - 52px);
  overflow: hidden;
}

.logs-sidebar {
  width: 240px;
  flex-shrink: 0;
  border-right: 1px solid var(--border-subtle);
  overflow-y: auto;
  overflow-x: hidden;
  display: flex;
  flex-direction: column;
  gap: 0;
}

.logs-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-width: 0;
}

/* ── Sidebar sections ── */
.sidebar-section {
  padding: 0;
  border-bottom: 1px solid var(--border-subtle);
}

.sidebar-section-header {
  font-size: 0.63rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.09em;
  color: var(--text-tertiary);
  padding: 0.6rem 0.75rem 0.3rem;
  font-family: var(--font-mono);
}

/* ── File tree ── */
.tree { padding-bottom: 0.5rem; }

.tree-item { display: flex; flex-direction: column; }
.tree-item.child-item { padding-left: 0.75rem; }

.tree-btn {
  display: flex;
  align-items: center;
  gap: 0.3rem;
  width: 100%;
  background: none;
  border: none;
  padding: 0.22rem 0.625rem;
  text-align: left;
  cursor: pointer;
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: 0.77rem;
  border-radius: 0;
  transition: background 0.1s, color 0.1s;
}
.tree-btn:hover { background: var(--accent-dim); color: var(--text-primary); }
.tree-item.active > .tree-btn,
.tree-btn.active {
  background: var(--accent-dim);
  color: var(--accent);
}

.tree-icon { font-size: 0.65rem; width: 10px; flex-shrink: 0; color: var(--text-tertiary); }
.tree-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tree-meta { font-size: 0.67rem; color: var(--text-tertiary); flex-shrink: 0; margin-left: 0.25rem; }
.tree-spin { font-size: 0.7rem; color: var(--text-tertiary); }
.tree-children { display: flex; flex-direction: column; }
.dir-btn .tree-name { color: var(--text-primary); }
.grandchild { padding-left: 1rem; }

/* ── Unit list ── */
.unit-filter-wrap { padding: 0.3rem 0.625rem; }
.unit-filter {
  width: 100%;
  background: var(--bg-raised);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-sm);
  padding: 0.2rem 0.4rem;
  font-size: 0.72rem;
  font-family: var(--font-mono);
  color: var(--text-primary);
  outline: none;
  box-sizing: border-box;
}

.unit-list { display: flex; flex-direction: column; padding-bottom: 0.5rem; }

.unit-btn {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  background: none;
  border: none;
  padding: 0.22rem 0.625rem;
  text-align: left;
  cursor: pointer;
  color: var(--text-secondary);
  font-family: var(--font-mono);
  font-size: 0.77rem;
  transition: background 0.1s, color 0.1s;
  width: 100%;
}
.unit-btn:hover { background: var(--accent-dim); color: var(--text-primary); }
.unit-btn.active { background: var(--accent-dim); color: var(--accent); }

.unit-dot {
  width: 6px; height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}
.unit-name { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

/* ── Empty state ── */
.logs-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  gap: 0.625rem;
  color: var(--text-tertiary);
}
.empty-icon { font-size: 2rem; opacity: 0.3; }
.empty-title { font-size: 0.95rem; font-weight: 600; color: var(--text-secondary); }
.empty-sub { font-size: 0.8rem; text-align: center; max-width: 320px; line-height: 1.5; }

/* ── Toolbar ── */
.log-toolbar {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  padding: 0.5rem 0.875rem;
  border-bottom: 1px solid var(--border-subtle);
  background: var(--bg-raised);
  flex-shrink: 0;
  flex-wrap: wrap;
}

.log-title {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  flex: 1;
  min-width: 0;
  overflow: hidden;
}
.log-title-path {
  font-family: var(--font-mono);
  font-size: 0.78rem;
  color: var(--text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.log-title-badge {
  font-size: 0.65rem;
  background: var(--accent-dim);
  color: var(--accent);
  padding: 0.1rem 0.35rem;
  border-radius: var(--r-sm);
  font-family: var(--font-mono);
  flex-shrink: 0;
}

.toolbar-actions {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-shrink: 0;
}

.filter-input {
  background: var(--bg-base);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-sm);
  padding: 0.2rem 0.5rem;
  font-size: 0.75rem;
  font-family: var(--font-mono);
  color: var(--text-primary);
  width: 160px;
  outline: none;
}
.filter-input:focus { border-color: var(--accent); }

.autoscroll-label {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  font-size: 0.72rem;
  color: var(--text-tertiary);
  cursor: pointer;
  user-select: none;
}

.btn { padding: 0.25rem 0.625rem; font-size: 0.75rem; cursor: pointer; border-radius: var(--r-sm); border: 1px solid var(--border-default); background: var(--bg-raised); color: var(--text-secondary); font-family: var(--font-mono); transition: background 0.1s, color 0.1s; }
.btn:hover { background: var(--accent-dim); color: var(--accent); border-color: var(--accent); }
.btn-follow-active { background: var(--accent-dim); color: var(--accent); border-color: var(--accent); }
.btn-sm { padding: 0.2rem 0.5rem; font-size: 0.72rem; }

.follow-indicator {
  font-size: 0.7rem;
  color: var(--green, #4ade80);
  font-family: var(--font-mono);
  animation: pulse 1.5s ease-in-out infinite;
}
@keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }

/* ── Log viewer ── */
.log-loading, .log-error, .log-empty-msg {
  padding: 1.5rem;
  font-size: 0.82rem;
  color: var(--text-tertiary);
  font-family: var(--font-mono);
}
.log-error { color: var(--red); }

.log-viewer {
  flex: 1;
  overflow-y: auto;
  overflow-x: auto;
  padding: 0.5rem 0;
  font-family: var(--font-mono);
  font-size: 0.74rem;
  line-height: 1.5;
  background: var(--bg-base);
}

.log-line {
  padding: 0 0.875rem;
  white-space: pre;
  min-height: 1.5em;
}
.log-line:hover { background: var(--bg-raised); }
.line-error { color: var(--red, #f87171); }
.line-warn  { color: var(--yellow, #fbbf24); }
.line-info  { color: var(--text-secondary); }
.line-debug { color: var(--text-tertiary); }

.log-footer {
  flex-shrink: 0;
  padding: 0.2rem 0.875rem;
  font-size: 0.67rem;
  color: var(--text-tertiary);
  border-top: 1px solid var(--border-subtle);
  font-family: var(--font-mono);
  background: var(--bg-raised);
}
.capped { color: var(--yellow, #fbbf24); }
</style>
