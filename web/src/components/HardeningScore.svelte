<script lang="ts">
  import { onMount } from 'svelte';

  interface HardeningCheck {
    id: string;
    label: string;
    category: string;
    points: number;
  }
  interface Result {
    id: string;
    label: string;
    category: string;
    points: number;
    pass: boolean;
    detail: string;
    fix?: string;
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

  const categories = ['ssh', 'network', 'filesystem', 'updates', 'auth'];
  const categoryLabel: Record<string, string> = {
    ssh: 'SSH', network: 'Network', filesystem: 'Filesystem',
    updates: 'Updates', auth: 'Authentication'
  };

  let failed = $derived((score?.results ?? []).filter(r => !r.pass));
  let passed = $derived((score?.results ?? []).filter(r => r.pass));

  async function load() {
    loading = true;
    try {
      const res = await fetch('/api/hardening');
      if (res.ok) score = await res.json();
    } catch {}
    finally { loading = false; }
  }

  async function refresh() {
    refreshing = true;
    try {
      const res = await fetch('/api/hardening/refresh', { method: 'POST' });
      if (res.ok) score = await res.json();
    } catch {}
    finally { refreshing = false; }
  }

  function toggle(id: string) {
    expanded = expanded === id ? null : id;
  }

  function runInTerminal(cmd: string) {
    const term = (window as any).__webuxTerminal;
    if (term?.runCommand) {
      term.runCommand(cmd);
      return;
    }
    window.location.hash = '#/terminal';
    let attempts = 0;
    const poll = setInterval(() => {
      const t = (window as any).__webuxTerminal;
      if (t?.runCommand) {
        clearInterval(poll);
        setTimeout(() => t.runCommand(cmd), 400);
        return;
      }
      if (++attempts > 40) clearInterval(poll);
    }, 50);
  }

  onMount(load);
</script>

<div class="hs-widget">
  <div class="hs-header">
    <span class="hs-title">Security Hardening</span>
    {#if score && !loading}
      <span class="hs-rank-badge hs-rank-{score.rank.toLowerCase()}" title="{score.pct}/100 pts">
        {score.rank_icon} {score.rank}
      </span>
      <span class="hs-level-badge" style="background:{score.color}22;color:{score.color};border-color:{score.color}44">
        {score.level}
      </span>
    {/if}
    <button class="hc-refresh" onclick={refresh} disabled={refreshing || loading} title="Re-run checks">
      {refreshing ? '…' : '⟳'}
    </button>
  </div>

  {#if loading}
    <div class="hs-score-bar-wrap">
      <div class="skeleton" style="height:8px;border-radius:4px;margin:0.75rem 1rem"></div>
    </div>
    {#each [1,2,3] as _}
      <div class="hc-row skeleton" style="height:40px;margin:2px 0"></div>
    {/each}
  {:else if score}
    <!-- Score bar -->
    <div class="hs-score-section">
      <div class="hs-score-bar-track">
        <div class="hs-score-bar-fill"
          style="width:{score.pct}%;background:{score.color}"></div>
      </div>
      <div class="hs-score-labels">
        <span class="hs-score-num" style="color:{score.color}">{score.pct}/100</span>
        <span class="hs-score-detail">{passed.length} of {score.results.length} checks passing · {score.raw}/{score.max} pts</span>
      </div>
    </div>

    <!-- Results grouped by category -->
    {#each categories as cat}
      {@const catResults = (score.results ?? []).filter(r => r.category === cat)}
      {#if catResults.length > 0}
        <div class="hs-cat-label">{categoryLabel[cat]}</div>
        {#each catResults as r (r.id)}
          <div class="hc-row" class:hc-pass={r.pass} class:hc-fail={!r.pass}>
            <button class="hc-row-btn" onclick={() => toggle(r.id)}>
              <span class="hc-icon">{r.pass ? '✓' : '✗'}</span>
              <span class="hc-label">{r.label}</span>
              <span class="hs-pts" style="color:{r.pass ? 'var(--accent)' : 'var(--text-tertiary)'}">
                {r.pass ? '+' : ''}{r.pass ? r.points : 0}/{r.points}pts
              </span>
              <span class="hc-chevron">{expanded === r.id ? '▾' : '▸'}</span>
            </button>
            {#if expanded === r.id}
              <div class="hc-detail">
                <p class="hs-detail-text">{r.detail}</p>
                {#if !r.pass && r.fix}
                  <div class="hc-fix-row">
                    <code class="hc-fix-cmd">{r.fix}</code>
                    <button class="hc-run-btn" onclick={() => runInTerminal(r.fix!)}>
                      ▶ Run in Terminal
                    </button>
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        {/each}
      {/if}
    {/each}

    {#if score.run_at}
      <div class="hs-footer">
        Last checked: {new Date(score.run_at).toLocaleString()}
      </div>
    {/if}
  {/if}
</div>

<style>
.hs-widget {
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-lg);
  overflow: hidden;
}

.hs-header {
  display: flex;
  align-items: center;
  gap: 0.625rem;
  padding: 0.75rem 1rem;
  background: var(--bg-raised);
  border-bottom: 1px solid var(--border-subtle);
}
.hs-title {
  font-size: 0.82rem;
  font-weight: 600;
  flex: 1;
  color: var(--text-primary);
}
.hs-rank-badge {
  font-size: 0.72rem;
  font-weight: 700;
  padding: 0.18rem 0.55rem;
  border-radius: 999px;
  border: 1px solid transparent;
  letter-spacing: 0.03em;
}
.hs-rank-bronze  { background: #cd7f3222; color: #cd7f32; border-color: #cd7f3255; }
.hs-rank-silver  { background: #a8a9ad22; color: #a8a9ad; border-color: #a8a9ad55; }
.hs-rank-gold    { background: #ffd70022; color: #b8970a; border-color: #ffd70055; }
.hs-rank-platinum { background: #e5e4e222; color: #8a9aaa; border-color: #e5e4e255; }

.hs-level-badge {
  font-size: 0.7rem;
  font-weight: 600;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
  border: 1px solid transparent;
  letter-spacing: 0.04em;
  text-transform: uppercase;
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
.hc-refresh:disabled { opacity: 0.4; cursor: default; }

.hs-score-section {
  padding: 0.75rem 1rem 0.5rem;
  border-bottom: 1px solid var(--border-subtle);
}
.hs-score-bar-track {
  height: 8px;
  background: var(--bg-base);
  border-radius: 4px;
  overflow: hidden;
}
.hs-score-bar-fill {
  height: 100%;
  border-radius: 4px;
  transition: width 0.4s ease;
}
.hs-score-labels {
  display: flex;
  align-items: baseline;
  gap: 0.75rem;
  margin-top: 0.4rem;
}
.hs-score-num {
  font-size: 0.9rem;
  font-weight: 700;
  font-variant-numeric: tabular-nums;
}
.hs-score-detail {
  font-size: 0.7rem;
  color: var(--text-tertiary);
}

.hs-cat-label {
  font-size: 0.65rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.08em;
  color: var(--text-tertiary);
  padding: 0.4rem 1rem 0.1rem;
  background: var(--bg-base);
  border-bottom: 1px solid var(--border-subtle);
}

/* reuse HealthChecks styles */
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
.hc-icon { font-size: 0.8rem; font-weight: 700; width: 16px; flex-shrink: 0; }
.hc-pass .hc-icon { color: var(--accent); }
.hc-fail .hc-icon { color: var(--red, #f87171); }
.hc-label { font-size: 0.82rem; font-weight: 500; flex: 1; color: var(--text-primary); }
.hc-fail .hc-label { color: var(--red, #f87171); }
.hs-pts { font-size: 0.7rem; font-variant-numeric: tabular-nums; flex-shrink: 0; }
.hc-chevron { font-size: 0.62rem; color: var(--text-tertiary); flex-shrink: 0; }
.hc-detail {
  padding: 0.625rem 1rem 0.75rem 2.75rem;
  background: var(--bg-base);
  border-top: 1px solid var(--border-subtle);
}
.hs-detail-text {
  font-size: 0.76rem;
  color: var(--text-secondary);
  margin: 0 0 0.5rem;
  line-height: 1.5;
}
.hc-fix-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
  margin-top: 0.25rem;
}
.hc-fix-cmd {
  font-family: monospace;
  font-size: 0.72rem;
  background: var(--bg-panel);
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-sm);
  padding: 0.2rem 0.45rem;
  color: var(--text-primary);
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  white-space: nowrap;
  display: block;
}
.hc-run-btn {
  flex-shrink: 0;
  font-size: 0.7rem;
  font-weight: 600;
  padding: 0.2rem 0.6rem;
  border-radius: var(--r-sm);
  border: 1px solid var(--accent);
  background: transparent;
  color: var(--accent);
  cursor: pointer;
  transition: background 0.12s, color 0.12s;
  white-space: nowrap;
}
.hc-run-btn:hover {
  background: var(--accent);
  color: #fff;
}

.hs-footer {
  padding: 0.4rem 1rem;
  font-size: 0.67rem;
  color: var(--text-tertiary);
  border-top: 1px solid var(--border-subtle);
  background: var(--bg-base);
}
</style>
