<script lang="ts">
  import { onMount } from 'svelte';

  interface Result {
    id: string;
    label: string;
    category: string;
    points: number;
    pass: boolean;
    detail: string;
  }
  interface Score {
    results: Result[];
    raw: number;
    max: number;
    pct: number;
    level: string;
    color: string;
    rank: string;
    rank_icon: string;
    run_at: string;
  }

  let score = $state<Score | null>(null);
  let loading = $state(true);
  let refreshing = $state(false);
  let expanded = $state<string | null>(null);

  const categories = ['memory', 'filesystem', 'network', 'kernel', 'systemd'];
  const categoryLabel: Record<string, string> = {
    memory: 'Memory', filesystem: 'Filesystem', network: 'Network',
    kernel: 'Kernel', systemd: 'Systemd'
  };

  let failed = $derived((score?.results ?? []).filter(r => !r.pass));
  let passed = $derived((score?.results ?? []).filter(r => r.pass));

  async function load() {
    loading = true;
    try {
      const res = await fetch('/api/performance');
      if (res.ok) score = await res.json();
    } catch {}
    finally { loading = false; }
  }

  async function refresh() {
    refreshing = true;
    try {
      const res = await fetch('/api/performance/refresh', { method: 'POST' });
      if (res.ok) score = await res.json();
    } catch {}
    finally { refreshing = false; }
  }

  function toggle(id: string) {
    expanded = expanded === id ? null : id;
  }

  onMount(load);
</script>

<div class="ps-widget">
  <div class="ps-header">
    <span class="ps-title">⚡ Performance Tuning</span>
    {#if score && !loading}
      <span class="ps-rank-badge ps-rank-{score.rank.toLowerCase()}" title="{score.pct}/100 pts">
        {score.rank_icon} {score.rank}
      </span>
      <span class="ps-level-badge" style="background:{score.color}22;color:{score.color};border-color:{score.color}44">
        {score.level}
      </span>
    {/if}
    <button class="ps-refresh" onclick={refresh} disabled={refreshing || loading} title="Re-run checks">
      {refreshing ? '…' : '⟳'}
    </button>
  </div>

  {#if loading}
    <div class="ps-score-section">
      <div class="skeleton" style="height:8px;border-radius:4px"></div>
    </div>
    {#each [1,2,3,4,5] as _}
      <div class="ps-row skeleton" style="height:40px;margin:2px 0"></div>
    {/each}
  {:else if score}
    <div class="ps-score-section">
      <div class="ps-score-bar-track">
        <div class="ps-score-bar-fill" style="width:{score.pct}%;background:{score.color}"></div>
      </div>
      <div class="ps-score-labels">
        <span class="ps-score-num" style="color:{score.color}">{score.pct}/100</span>
        <span class="ps-score-detail">{passed.length} of {score.results.length} checks passing · {score.raw}/{score.max} pts</span>
        {#if failed.length > 0}
          <span class="ps-score-detail" style="color:var(--text-tertiary)">· {failed.length} tuning {failed.length === 1 ? 'opportunity' : 'opportunities'}</span>
        {/if}
      </div>
    </div>

    {#each categories as cat}
      {@const catResults = (score.results ?? []).filter(r => r.category === cat)}
      {#if catResults.length > 0}
        <div class="ps-cat-label">{categoryLabel[cat]}</div>
        {#each catResults as r (r.id)}
          <div class="ps-row" class:ps-pass={r.pass} class:ps-fail={!r.pass}>
            <button class="ps-row-btn" onclick={() => toggle(r.id)}>
              <span class="ps-icon">{r.pass ? '✓' : '✗'}</span>
              <span class="ps-label">{r.label}</span>
              <span class="ps-pts" style="color:{r.pass ? 'var(--accent)' : 'var(--text-tertiary)'}">
                {r.pass ? '+' : ''}{r.pass ? r.points : 0}/{r.points}pts
              </span>
              <span class="ps-chevron">{expanded === r.id ? '▾' : '▸'}</span>
            </button>
            {#if expanded === r.id}
              <div class="ps-detail">
                <p class="ps-detail-text">{r.detail}</p>
              </div>
            {/if}
          </div>
        {/each}
      {/if}
    {/each}

    {#if score.run_at}
      <div class="ps-footer">
        Last checked: {new Date(score.run_at).toLocaleString()}
      </div>
    {/if}
  {/if}
</div>

<style>
.ps-widget {
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-lg);
  overflow: hidden;
}

.ps-header {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.75rem 1rem;
  background: var(--bg-raised);
  border-bottom: 1px solid var(--border-subtle);
}
.ps-title {
  font-size: 0.82rem;
  font-weight: 600;
  flex: 1;
  color: var(--text-primary);
}
.ps-rank-badge {
  font-size: 0.72rem;
  font-weight: 700;
  padding: 0.18rem 0.55rem;
  border-radius: 999px;
  border: 1px solid transparent;
  letter-spacing: 0.03em;
}
.ps-rank-bronze   { background: #cd7f3222; color: #cd7f32; border-color: #cd7f3255; }
.ps-rank-silver   { background: #a8a9ad22; color: #a8a9ad; border-color: #a8a9ad55; }
.ps-rank-gold     { background: #ffd70022; color: #b8970a; border-color: #ffd70055; }
.ps-rank-platinum { background: #e5e4e222; color: #8a9aaa; border-color: #e5e4e255; }

.ps-level-badge {
  font-size: 0.7rem;
  font-weight: 600;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
  border: 1px solid transparent;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.ps-refresh {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-tertiary);
  font-size: 0.9rem;
  padding: 0.15rem 0.3rem;
  border-radius: var(--r-sm);
  transition: color 0.12s;
}
.ps-refresh:hover { color: var(--accent); }
.ps-refresh:disabled { opacity: 0.4; cursor: default; }

.ps-score-section {
  padding: 0.75rem 1rem 0.5rem;
  border-bottom: 1px solid var(--border-subtle);
}
.ps-score-bar-track {
  height: 8px;
  background: var(--bg-base);
  border-radius: 4px;
  overflow: hidden;
}
.ps-score-bar-fill {
  height: 100%;
  border-radius: 4px;
  transition: width 0.4s ease;
}
.ps-score-labels {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
  margin-top: 0.4rem;
  flex-wrap: wrap;
}
.ps-score-num {
  font-size: 0.9rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.ps-score-detail {
  font-size: 0.7rem;
  color: var(--text-tertiary);
}

.ps-cat-label {
  font-size: 0.65rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-tertiary);
  padding: 0.4rem 1rem 0.1rem;
  background: var(--bg-base);
  border-bottom: 1px solid var(--border-subtle);
}

.ps-row { border-bottom: 1px solid var(--border-subtle); }
.ps-row:last-child { border-bottom: none; }
.ps-row-btn {
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
.ps-row-btn:hover { background: var(--bg-hover); }
.ps-icon { font-size: 0.8rem; font-weight: 700; width: 16px; flex-shrink: 0; }
.ps-pass .ps-icon { color: var(--accent); }
.ps-fail .ps-icon { color: var(--red, #f87171); }
.ps-label { font-size: 0.82rem; font-weight: 500; flex: 1; color: var(--text-primary); }
.ps-fail .ps-label { color: var(--red, #f87171); }
.ps-pts { font-size: 0.7rem; font-variant-numeric: tabular-nums; flex-shrink: 0; }
.ps-chevron { font-size: 0.62rem; color: var(--text-tertiary); flex-shrink: 0; }
.ps-detail {
  padding: 0.625rem 1rem 0.75rem 2.75rem;
  background: var(--bg-base);
  border-top: 1px solid var(--border-subtle);
}
.ps-detail-text {
  font-size: 0.76rem;
  color: var(--text-secondary);
  margin: 0;
  line-height: 1.5;
}

.ps-footer {
  padding: 0.4rem 1rem;
  font-size: 0.67rem;
  color: var(--text-tertiary);
  border-top: 1px solid var(--border-subtle);
  background: var(--bg-base);
}
</style>
