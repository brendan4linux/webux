<script lang="ts">
  import { onMount } from 'svelte';

  interface Result {
    id: string; label: string; description: string;
    pass: boolean; error: string; ms: number;
  }

  let results: Result[] = $state([]);
  let loading = $state(true);
  let expanded = $state<string | null>(null);
  let detailText = $state<Record<string, string>>({});
  let detailLoading = $state(false);

  async function load() {
    loading = true;
    try {
      const res = await fetch('/api/health');
      if (res.ok) {
        const d = await res.json();
        results = d.results ?? [];
      }
    } catch {}
    finally { loading = false; }
  }

  async function toggle(id: string) {
    if (expanded === id) { expanded = null; return; }
    expanded = id;
    if (detailText[id] !== undefined) return;

    detailLoading = true;
    try {
      const res = await fetch(`/api/health/detail?id=${encodeURIComponent(id)}`);
      if (res.ok) {
        const d = await res.json();
        detailText = { ...detailText, [id]: d.detail ?? '' };
      }
    } catch {}
    finally { detailLoading = false; }
  }

  let passed = $derived(results.filter(r => r.pass).length);
  let failed = $derived(results.filter(r => !r.pass).length);

  onMount(load);
</script>

<div class="hc-widget">
  <div class="hc-header">
    <span class="hc-title">System Health</span>
    {#if !loading}
      {#if failed === 0}
        <span class="badge badge-green" style="font-size:0.7rem">✓ All checks passed</span>
      {:else}
        <span class="badge badge-red" style="font-size:0.7rem">✗ {failed} issue{failed !== 1 ? 's' : ''}</span>
      {/if}
    {/if}
    <button class="hc-refresh" onclick={load} disabled={loading} title="Refresh">⟳</button>
  </div>

  <div class="hc-list">
    {#if loading}
      {#each [1,2,3,4,5,6] as _}
        <div class="hc-row skeleton" style="height:40px"></div>
      {/each}
    {:else if results.length === 0}
      <div class="hc-empty">No health checks configured</div>
    {:else}
      {#each results as r (r.id)}
        <div class="hc-row" class:hc-pass={r.pass} class:hc-fail={!r.pass}>
          <!-- clickable row -->
          <button class="hc-row-btn" onclick={() => toggle(r.id)}>
            <span class="hc-icon">{r.pass ? '✓' : '✗'}</span>
            <span class="hc-label">{r.label}</span>
            <span class="hc-desc">{r.description}</span>
            <span class="hc-chevron">{expanded === r.id ? '▾' : '▸'}</span>
          </button>

          <!-- expanded detail -->
          {#if expanded === r.id}
            <div class="hc-detail">
              {#if detailLoading && detailText[r.id] === undefined}
                <span style="color:var(--text-tertiary);font-size:0.75rem">Loading…</span>
              {:else if detailText[r.id]}
                <pre class="hc-pre">{detailText[r.id]}</pre>
              {:else}
                <span style="color:var(--text-tertiary);font-size:0.75rem">No detail available</span>
              {/if}
              {#if r.error}
                <div style="color:var(--red);font-size:0.72rem;margin-top:0.25rem">Error: {r.error}</div>
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    {/if}
  </div>
</div>

<style>
.hc-widget {
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-lg);
  overflow: hidden;
}

.hc-header {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.75rem 1rem;
  background: var(--bg-raised);
  border-bottom: 1px solid var(--border-subtle);
}
.hc-title {
  font-size: 0.82rem;
  font-weight: 600;
  flex: 1;
  color: var(--text-primary);
}
.hc-refresh {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-tertiary);
  font-size: 0.9rem;
  padding: 0.15rem 0.3rem;
  border-radius: var(--r-sm);
  transition: color 0.12s;
}
.hc-refresh:hover { color: var(--accent); }

.hc-list { display: flex; flex-direction: column; }
.hc-row { border-bottom: 1px solid var(--border-subtle); }
.hc-row:last-child { border-bottom: none; }

.hc-row-btn {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.55rem 1rem;
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  transition: background 0.1s;
}
.hc-row-btn:hover { background: var(--bg-hover); }

.hc-icon {
  font-size: 0.8rem;
  font-weight: 700;
  width: 16px;
  flex-shrink: 0;
}
.hc-pass .hc-icon { color: var(--accent); }
.hc-fail .hc-icon { color: var(--red); }

.hc-label {
  font-size: 0.82rem;
  font-weight: 500;
  min-width: 130px;
  flex-shrink: 0;
  color: var(--text-primary);
}
.hc-fail .hc-label { color: var(--red); }

.hc-desc {
  font-size: 0.72rem;
  color: var(--text-tertiary);
  flex: 1;
}

.hc-chevron {
  font-size: 0.62rem;
  color: var(--text-tertiary);
  flex-shrink: 0;
}

.hc-detail {
  padding: 0.625rem 1rem 0.75rem 2.75rem;
  background: var(--bg-base);
  border-top: 1px solid var(--border-subtle);
}
.hc-pre {
  font-family: var(--font-mono);
  font-size: 0.72rem;
  color: var(--text-secondary);
  white-space: pre-wrap;
  word-break: break-all;
  margin: 0;
  line-height: 1.6;
}
.hc-empty {
  padding: 1.5rem;
  text-align: center;
  color: var(--text-tertiary);
  font-size: 0.82rem;
}
</style>
