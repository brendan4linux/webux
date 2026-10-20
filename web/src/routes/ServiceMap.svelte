<script lang="ts">
  import { onMount } from 'svelte';

  // ── Types ──────────────────────────────────────────────────────────────────
  interface OutboundConn {
    dst_ip: string;
    dst_port: number;
    protocol: string;
    service_hint: string;
    state: string;
  }

  interface Service {
    pid: number;
    name: string;
    display_name: string;
    service_type: string;
    exe: string;
    home_dir: string;
    user: string;
    protocol: string;
    port: number;
    address: string;
    config_paths: string[];
    env_vars: Record<string, string> | null;
    outbound: OutboundConn[];
    client_count: number;
  }

  interface ConnectionStat {
    src_process: string;
    dst_ip: string;
    dst_port: number;
    protocol: string;
    service_hint: string;
    hit_count: number;
    first_seen: string;
    last_seen: string;
  }

  interface TopologyNode {
    id: string;
    label: string;
    type: 'local-service' | 'remote-host';
    service_type?: string;
    port?: number;
    ip?: string;
  }

  interface TopologyEdge {
    source: string;
    target: string;
    protocol: string;
    service_hint: string;
    count: number;
  }

  // ── State ──────────────────────────────────────────────────────────────────
  let activeTab = $state<'services' | 'connections' | 'topology'>('services');
  let services = $state<Service[]>([]);
  let connections = $state<ConnectionStat[]>([]);
  let topoNodes = $state<TopologyNode[]>([]);
  let topoEdges = $state<TopologyEdge[]>([]);

  let loading = $state(false);
  let error = $state('');
  let lastRefresh = $state<Date | null>(null);
  let conntrackOK = $state(true);

  let searchQuery = $state('');
  let expandedService = $state<number | null>(null);

  // ── Data loading ───────────────────────────────────────────────────────────
  async function loadAll() {
    loading = true;
    error = '';
    try {
      const [snapRes, connRes, topoRes] = await Promise.all([
        fetch('/api/servicemap/snapshot'),
        fetch('/api/servicemap/connections'),
        fetch('/api/servicemap/topology'),
      ]);
      if (!snapRes.ok) throw new Error(`snapshot: ${snapRes.status}`);

      const snap = await snapRes.json();
      services = snap.services ?? [];
      conntrackOK = snap.conntrack_ok ?? false;

      if (connRes.ok) {
        const c = await connRes.json();
        connections = c.connections ?? [];
      }
      if (topoRes.ok) {
        const t = await topoRes.json();
        topoNodes = t.nodes ?? [];
        topoEdges = t.edges ?? [];
      }
      lastRefresh = new Date();
    } catch (e: any) {
      error = e.message ?? 'Failed to load service map';
    } finally {
      loading = false;
    }
  }

  onMount(loadAll);

  // ── Computed ───────────────────────────────────────────────────────────────
  let filteredServices = $derived(
    searchQuery.trim()
      ? services.filter(s =>
          s.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
          s.display_name.toLowerCase().includes(searchQuery.toLowerCase()) ||
          s.exe.toLowerCase().includes(searchQuery.toLowerCase()) ||
          String(s.port).includes(searchQuery)
        )
      : services
  );

  // ── Service type icon/colour ───────────────────────────────────────────────
  function serviceIcon(type: string): string {
    const icons: Record<string, string> = {
      nginx: '◫', apache: '◫', caddy: '◫', haproxy: '⬡',
      mysql: '⬠', postgresql: '⬠', mongodb: '⬠', redis: '◈',
      memcached: '◈', rabbitmq: '◎', elasticsearch: '◎',
      ssh: '◉', dns: '◎', smtp: '◈', etcd: '◆', docker: '▣',
      kubernetes: '◆', varnish: '◫', 'java-app': '◧',
      'node-app': '◧', 'python-app': '◧', 'ruby-app': '◧',
      'go-app': '◧', generic: '◈',
    };
    return icons[type] ?? '◈';
  }

  function serviceColor(type: string): string {
    const colors: Record<string, string> = {
      nginx: 'var(--accent)', apache: 'var(--accent)', caddy: 'var(--accent)',
      haproxy: 'var(--accent)',
      mysql: '#f59e0b', postgresql: '#3b82f6', mongodb: '#22c55e',
      redis: '#ef4444',
      rabbitmq: '#f97316', elasticsearch: '#a78bfa',
      ssh: 'var(--text-secondary)', dns: 'var(--text-secondary)',
      'java-app': '#f59e0b', 'node-app': '#22c55e',
      'python-app': '#3b82f6', 'ruby-app': '#ef4444',
    };
    return colors[type] ?? 'var(--text-tertiary)';
  }

  function formatTime(iso: string): string {
    if (!iso) return '—';
    try {
      return new Date(iso).toLocaleString();
    } catch { return iso; }
  }

  function toggleExpand(pid: number) {
    expandedService = expandedService === pid ? null : pid;
  }

  // ── Topology layout ─────────────────────────────────────────────────────────
  // Returns SVG layout info for the topology diagram.
  let localNodes = $derived(topoNodes.filter(n => n.type === 'local-service'));
  let remoteNodes = $derived(topoNodes.filter(n => n.type === 'remote-host'));

  const NODE_H = 44;
  const NODE_W = 200;
  const NODE_GAP = 12;
  const COL_GAP = 140;
  const PAD = 24;

  function nodeY(nodes: TopologyNode[], idx: number): number {
    return PAD + idx * (NODE_H + NODE_GAP);
  }
  function colHeight(nodes: TopologyNode[]): number {
    return PAD * 2 + nodes.length * (NODE_H + NODE_GAP) - NODE_GAP;
  }

  let svgHeight = $derived(
    Math.max(colHeight(localNodes), colHeight(remoteNodes), 80)
  );
  let svgWidth = $derived(PAD * 2 + NODE_W * 2 + COL_GAP);

  // Map node ID → center-right / center-left port coordinates
  function leftPort(idx: number): [number, number] {
    return [PAD + NODE_W, PAD + idx * (NODE_H + NODE_GAP) + NODE_H / 2];
  }
  function rightPort(idx: number): [number, number] {
    return [PAD + NODE_W + COL_GAP, PAD + idx * (NODE_H + NODE_GAP) + NODE_H / 2];
  }

  // Build edge paths
  let edgePaths = $derived.by(() => {
    const localIdx = new Map(localNodes.map((n, i) => [n.id, i]));
    const remoteIdx = new Map(remoteNodes.map((n, i) => [n.id, i]));
    return topoEdges.flatMap(e => {
      const li = localIdx.get(e.source);
      const ri = remoteIdx.get(e.target);
      if (li === undefined || ri === undefined) return [];
      const [x1, y1] = leftPort(li);
      const [x2, y2] = rightPort(ri);
      const mx = (x1 + x2) / 2;
      return [{ path: `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`, edge: e, x1, y1, x2, y2, mx }];
    });
  });
</script>

<!-- ── Page ──────────────────────────────────────────────────────────────────── -->
<div class="page">

  <!-- Header -->
  <div class="page-header">
    <div class="header-left">
      <h1 class="page-title">Service Map</h1>
      <span class="page-subtitle">
        {#if lastRefresh}
          {services.length} service{services.length !== 1 ? 's' : ''} · refreshed {lastRefresh.toLocaleTimeString()}
        {:else}
          Scanning…
        {/if}
      </span>
    </div>
    <div class="header-right">
      {#if !conntrackOK}
        <span class="badge badge-warn" title="Connection history requires nf_conntrack kernel module">conntrack unavailable</span>
      {/if}
      <button class="btn-refresh" onclick={loadAll} disabled={loading}>
        {loading ? '…' : '↺ Refresh'}
      </button>
    </div>
  </div>

  {#if error}
    <div class="error-banner">{error}</div>
  {/if}

  <!-- Tabs -->
  <div class="tabs">
    <button class="tab" class:active={activeTab === 'services'} onclick={() => activeTab = 'services'}>
      Services <span class="tab-count">{services.length}</span>
    </button>
    <button class="tab" class:active={activeTab === 'connections'} onclick={() => activeTab = 'connections'}>
      Connections <span class="tab-count">{connections.length}</span>
    </button>
    <button class="tab" class:active={activeTab === 'topology'} onclick={() => activeTab = 'topology'}>
      Topology
    </button>
  </div>

  <!-- ── SERVICES TAB ────────────────────────────────────────────────────── -->
  {#if activeTab === 'services'}
    <div class="tab-body">
      <div class="search-row">
        <input
          class="search-input"
          type="text"
          placeholder="Filter by name, port, or path…"
          bind:value={searchQuery}
        />
      </div>

      {#if loading && services.length === 0}
        <div class="empty-state">Scanning services…</div>
      {:else if filteredServices.length === 0}
        <div class="empty-state">No services found.</div>
      {:else}
        <div class="service-list">
          {#each filteredServices as svc (`${svc.pid}:${svc.port}`)}
            {@const isExpanded = expandedService === svc.pid}
            <div class="service-card" class:expanded={isExpanded}>
              <!-- Card header — always visible -->
              <button class="card-header" onclick={() => toggleExpand(svc.pid)}>
                <div class="card-icon" style="color: {serviceColor(svc.service_type)}">
                  {serviceIcon(svc.service_type)}
                </div>
                <div class="card-main">
                  <span class="card-name">{svc.display_name}</span>
                  <span class="card-meta">{svc.name} · PID {svc.pid}</span>
                </div>
                <div class="card-port">
                  <span class="port-badge">{svc.protocol.toUpperCase()} :{svc.port}</span>
                  {#if svc.address && svc.address !== '0.0.0.0' && svc.address !== '::'}
                    <span class="port-addr">{svc.address}</span>
                  {/if}
                </div>
                <div class="card-stats">
                  {#if svc.client_count > 0}
                    <span class="stat-pill stat-in" title="Active inbound connections">↓ {svc.client_count}</span>
                  {/if}
                  {#if svc.outbound?.length > 0}
                    <span class="stat-pill stat-out" title="Active outbound connections">↑ {svc.outbound.length}</span>
                  {/if}
                </div>
                <div class="card-chevron">{isExpanded ? '▲' : '▼'}</div>
              </button>

              <!-- Expanded detail -->
              {#if isExpanded}
                <div class="card-detail">
                  <div class="detail-grid">

                    <!-- Process -->
                    <div class="detail-section">
                      <div class="detail-label">Process</div>
                      <div class="detail-value mono">{svc.exe || svc.name}</div>
                      {#if svc.user}
                        <div class="detail-value sub">user: {svc.user}</div>
                      {/if}
                    </div>

                    <!-- Home directory -->
                    {#if svc.home_dir}
                      <div class="detail-section">
                        <div class="detail-label">Home / Data Dir</div>
                        <div class="detail-value mono">{svc.home_dir}</div>
                      </div>
                    {/if}

                    <!-- Config paths -->
                    {#if svc.config_paths?.length > 0}
                      <div class="detail-section">
                        <div class="detail-label">Config Paths</div>
                        {#each svc.config_paths as p}
                          <div class="detail-value mono">{p}</div>
                        {/each}
                      </div>
                    {/if}

                    <!-- Env vars -->
                    {#if svc.env_vars && Object.keys(svc.env_vars).length > 0}
                      <div class="detail-section full-width">
                        <div class="detail-label">Key Environment Variables</div>
                        <div class="env-table">
                          {#each Object.entries(svc.env_vars) as [k, v]}
                            <div class="env-row">
                              <span class="env-key">{k}</span>
                              <span class="env-val mono">{v}</span>
                            </div>
                          {/each}
                        </div>
                      </div>
                    {/if}

                    <!-- Outbound connections -->
                    {#if svc.outbound?.length > 0}
                      <div class="detail-section full-width">
                        <div class="detail-label">Active Outbound Connections</div>
                        <table class="conn-table">
                          <thead>
                            <tr><th>Destination</th><th>Port</th><th>Protocol</th><th>Service</th><th>State</th></tr>
                          </thead>
                          <tbody>
                            {#each svc.outbound as ob}
                              <tr>
                                <td class="mono">{ob.dst_ip}</td>
                                <td class="mono">{ob.dst_port}</td>
                                <td>{ob.protocol.toUpperCase()}</td>
                                <td>{ob.service_hint || '—'}</td>
                                <td><span class="state-badge state-{ob.state.toLowerCase()}">{ob.state}</span></td>
                              </tr>
                            {/each}
                          </tbody>
                        </table>
                      </div>
                    {/if}

                  </div>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </div>

  <!-- ── CONNECTIONS TAB ────────────────────────────────────────────────── -->
  {:else if activeTab === 'connections'}
    <div class="tab-body">
      {#if !conntrackOK}
        <div class="info-banner">
          <strong>conntrack not available</strong> — connection history requires the
          <code>nf_conntrack</code> kernel module. Run
          <code>modprobe nf_conntrack</code> to enable it, then data will accumulate
          over 30-second poll intervals.
        </div>
      {/if}

      {#if connections.length === 0}
        <div class="empty-state">
          {conntrackOK ? 'No connection history yet — data accumulates over time.' : 'No data — conntrack unavailable.'}
        </div>
      {:else}
        <div class="table-wrapper">
          <table class="data-table">
            <thead>
              <tr>
                <th>Source Process</th>
                <th>Destination</th>
                <th>Port</th>
                <th>Protocol</th>
                <th>Service</th>
                <th class="right">Connections</th>
                <th>Last Seen</th>
              </tr>
            </thead>
            <tbody>
              {#each connections as c}
                <tr>
                  <td><span class="process-name">{c.src_process}</span></td>
                  <td class="mono">{c.dst_ip}</td>
                  <td class="mono">{c.dst_port}</td>
                  <td>{c.protocol.toUpperCase()}</td>
                  <td>{c.service_hint || '—'}</td>
                  <td class="right">
                    <span class="count-badge">{c.hit_count.toLocaleString()}</span>
                  </td>
                  <td class="muted">{formatTime(c.last_seen)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>

  <!-- ── TOPOLOGY TAB ───────────────────────────────────────────────────── -->
  {:else if activeTab === 'topology'}
    <div class="tab-body topology-body">
      {#if localNodes.length === 0 && remoteNodes.length === 0}
        <div class="empty-state">No topology data yet.</div>
      {:else}
        <div class="topo-legend">
          <span class="legend-dot local"></span><span>Local service</span>
          <span class="legend-dot remote"></span><span>Remote endpoint</span>
        </div>

        <!-- SVG topology diagram -->
        <div class="topo-scroll">
          <svg
            width={svgWidth}
            height={svgHeight}
            viewBox="0 0 {svgWidth} {svgHeight}"
            class="topo-svg"
          >
            <!-- Column headers -->
            <text x={PAD + NODE_W / 2} y={PAD - 8} class="col-header" text-anchor="middle">LOCAL SERVICES</text>
            <text x={PAD + NODE_W + COL_GAP + NODE_W / 2} y={PAD - 8} class="col-header" text-anchor="middle">REMOTE ENDPOINTS</text>

            <!-- Edge paths — draw first so nodes appear on top -->
            {#each edgePaths as ep}
              <path
                d={ep.path}
                class="edge-path"
                fill="none"
              />
              <!-- Count label at midpoint -->
              {#if ep.edge.count > 1}
                <rect
                  x={(ep.x1 + ep.x2) / 2 - 14}
                  y={(ep.y1 + ep.y2) / 2 - 9}
                  width="28" height="16" rx="4"
                  class="edge-count-bg"
                />
                <text
                  x={(ep.x1 + ep.x2) / 2}
                  y={(ep.y1 + ep.y2) / 2 + 4}
                  class="edge-count-text"
                  text-anchor="middle"
                >{ep.edge.count.toLocaleString()}</text>
              {/if}
            {/each}

            <!-- Local service nodes -->
            {#each localNodes as node, i}
              {@const y = nodeY(localNodes, i)}
              <g class="node-group local-node">
                <rect x={PAD} y={y} width={NODE_W} height={NODE_H} rx="6" class="node-rect local-rect" />
                <circle cx={PAD + 14} cy={y + NODE_H / 2} r="5" class="node-dot local-dot"
                  style="fill: {serviceColor(node.service_type ?? 'generic')}" />
                <text x={PAD + 26} y={y + 16} class="node-name">{
                  node.label.split(' :')[0]
                }</text>
                <text x={PAD + 26} y={y + 32} class="node-port">:{
                  node.port ?? node.label.split(':').pop()
                }</text>
                <!-- Right connector dot -->
                <circle cx={PAD + NODE_W} cy={y + NODE_H / 2} r="3" class="connector-dot" />
              </g>
            {/each}

            <!-- Remote endpoint nodes -->
            {#each remoteNodes as node, i}
              {@const y = nodeY(remoteNodes, i)}
              {@const x = PAD + NODE_W + COL_GAP}
              <g class="node-group remote-node">
                <rect x={x} y={y} width={NODE_W} height={NODE_H} rx="6" class="node-rect remote-rect" />
                <text x={x + 12} y={y + 16} class="node-name remote-name">{node.ip ?? node.label.split(':')[0]}</text>
                <text x={x + 12} y={y + 32} class="node-port remote-port">
                  :{node.port}{#if node.label.includes('(')}&nbsp;· {node.label.split('(')[1]?.replace(')', '')}{/if}
                </text>
                <!-- Left connector dot -->
                <circle cx={x} cy={y + NODE_H / 2} r="3" class="connector-dot" />
              </g>
            {/each}
          </svg>
        </div>

        <!-- Fallback table for services with no outbound edges -->
        {#if localNodes.length > 0}
          <div class="topo-footer">
            <p class="topo-note">
              {localNodes.length} local service{localNodes.length !== 1 ? 's' : ''} ·
              {remoteNodes.length} remote endpoint{remoteNodes.length !== 1 ? 's' : ''} ·
              {topoEdges.length} connection edge{topoEdges.length !== 1 ? 's' : ''}
              {#if !conntrackOK}· <em>Enable conntrack for historical data</em>{/if}
            </p>
          </div>
        {/if}
      {/if}
    </div>
  {/if}
</div>

<style>
/* ── Layout ── */
.page { padding: 1.5rem 2rem; max-width: 1400px; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; margin-bottom: 1.5rem; gap: 1rem; }
.header-left { display: flex; flex-direction: column; gap: 0.25rem; }
.header-right { display: flex; align-items: center; gap: 0.75rem; flex-shrink: 0; }
.page-title { font-family: var(--font-mono); font-size: 1.25rem; font-weight: 600; color: var(--text-primary); margin: 0; }
.page-subtitle { font-size: 0.8rem; color: var(--text-tertiary); font-family: var(--font-mono); }

/* ── Tabs ── */
.tabs { display: flex; gap: 0; border-bottom: 1px solid var(--border-subtle); margin-bottom: 1.5rem; }
.tab {
  font-family: var(--font-mono); font-size: 0.82rem; font-weight: 400;
  color: var(--text-tertiary); background: none; border: none; cursor: pointer;
  padding: 0.6rem 1.25rem; border-bottom: 2px solid transparent;
  transition: color 0.12s, border-color 0.12s;
}
.tab:hover { color: var(--text-primary); }
.tab.active { color: var(--accent); border-bottom-color: var(--accent); font-weight: 500; }
.tab-count {
  display: inline-flex; align-items: center; justify-content: center;
  font-size: 0.65rem; background: var(--accent-dim); color: var(--accent);
  border-radius: 10px; min-width: 18px; height: 18px; padding: 0 5px; margin-left: 6px;
}

/* ── Tab body ── */
.tab-body { min-height: 300px; }

/* ── Search ── */
.search-row { margin-bottom: 1rem; }
.search-input {
  width: 100%; max-width: 400px;
  background: var(--bg-raised); border: 1px solid var(--border-subtle);
  color: var(--text-primary); font-family: var(--font-mono); font-size: 0.82rem;
  padding: 0.45rem 0.75rem; border-radius: 4px; outline: none;
  transition: border-color 0.15s;
}
.search-input:focus { border-color: var(--accent); }

/* ── Service cards ── */
.service-list { display: flex; flex-direction: column; gap: 0.5rem; }

.service-card {
  border: 1px solid var(--border-subtle);
  border-radius: 6px;
  overflow: hidden;
  background: var(--bg-raised);
  transition: border-color 0.15s;
}
.service-card.expanded { border-color: var(--accent); }

.card-header {
  display: flex; align-items: center; gap: 0.75rem;
  padding: 0.65rem 1rem; width: 100%;
  background: none; border: none; cursor: pointer; text-align: left;
  color: var(--text-primary);
}
.card-header:hover { background: var(--accent-dim); }

.card-icon { font-size: 1rem; width: 20px; text-align: center; flex-shrink: 0; }
.card-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.card-name { font-family: var(--font-mono); font-size: 0.88rem; font-weight: 500; color: var(--text-primary); }
.card-meta { font-family: var(--font-mono); font-size: 0.72rem; color: var(--text-tertiary); }
.card-port { display: flex; flex-direction: column; align-items: flex-end; gap: 2px; flex-shrink: 0; }
.port-badge {
  font-family: var(--font-mono); font-size: 0.75rem; font-weight: 600;
  color: var(--accent); background: var(--accent-dim); padding: 2px 8px; border-radius: 3px;
}
.port-addr { font-family: var(--font-mono); font-size: 0.68rem; color: var(--text-tertiary); }
.card-stats { display: flex; gap: 0.5rem; flex-shrink: 0; }
.stat-pill {
  font-family: var(--font-mono); font-size: 0.7rem; padding: 2px 7px; border-radius: 10px;
}
.stat-in  { background: color-mix(in srgb, #22c55e 15%, transparent); color: #22c55e; }
.stat-out { background: color-mix(in srgb, #f59e0b 15%, transparent); color: #f59e0b; }
.card-chevron { font-size: 0.6rem; color: var(--text-tertiary); flex-shrink: 0; }

/* ── Expanded detail ── */
.card-detail { border-top: 1px solid var(--border-subtle); padding: 1rem; background: var(--bg-panel); }
.detail-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 1rem; }
.detail-section { display: flex; flex-direction: column; gap: 4px; }
.detail-section.full-width { grid-column: 1 / -1; }
.detail-label { font-size: 0.68rem; font-weight: 500; text-transform: uppercase; letter-spacing: 0.08em; color: var(--text-tertiary); }
.detail-value { font-size: 0.8rem; color: var(--text-secondary); word-break: break-all; }
.detail-value.mono { font-family: var(--font-mono); font-size: 0.75rem; }
.detail-value.sub { font-size: 0.72rem; color: var(--text-tertiary); }

/* ── Env vars ── */
.env-table { display: flex; flex-direction: column; gap: 4px; margin-top: 4px; }
.env-row { display: flex; gap: 0.75rem; font-size: 0.78rem; }
.env-key { font-family: var(--font-mono); color: var(--accent); min-width: 180px; flex-shrink: 0; }
.env-val { font-family: var(--font-mono); color: var(--text-secondary); word-break: break-all; }

/* ── Outbound connections table ── */
.conn-table { width: 100%; border-collapse: collapse; font-size: 0.78rem; margin-top: 4px; }
.conn-table th { font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-tertiary); text-align: left; padding: 4px 8px; border-bottom: 1px solid var(--border-subtle); }
.conn-table td { padding: 4px 8px; color: var(--text-secondary); border-bottom: 1px solid color-mix(in srgb, var(--border-subtle) 40%, transparent); }
.conn-table tr:last-child td { border-bottom: none; }
.state-badge { font-family: var(--font-mono); font-size: 0.68rem; padding: 2px 6px; border-radius: 3px; background: var(--accent-dim); color: var(--accent); }

/* ── Connections tab ── */
.table-wrapper { overflow-x: auto; }
.data-table { width: 100%; border-collapse: collapse; font-size: 0.82rem; }
.data-table th {
  font-family: var(--font-mono); font-size: 0.68rem; font-weight: 500;
  text-transform: uppercase; letter-spacing: 0.06em;
  color: var(--text-tertiary); text-align: left; padding: 0.5rem 0.75rem;
  border-bottom: 1px solid var(--border-subtle); white-space: nowrap;
}
.data-table td { padding: 0.45rem 0.75rem; color: var(--text-secondary); border-bottom: 1px solid color-mix(in srgb, var(--border-subtle) 40%, transparent); }
.data-table tr:hover td { background: var(--accent-dim); }
.data-table .right { text-align: right; }
.count-badge {
  font-family: var(--font-mono); font-size: 0.78rem; font-weight: 600;
  color: var(--accent);
}
.process-name { font-family: var(--font-mono); font-size: 0.8rem; color: var(--text-primary); }
.muted { color: var(--text-tertiary); font-size: 0.75rem; }

/* ── Topology ── */
.topology-body { overflow: hidden; }
.topo-legend { display: flex; align-items: center; gap: 1.25rem; margin-bottom: 1rem; font-size: 0.78rem; color: var(--text-tertiary); }
.legend-dot { width: 10px; height: 10px; border-radius: 2px; flex-shrink: 0; }
.legend-dot.local { background: var(--accent); }
.legend-dot.remote { background: color-mix(in srgb, var(--text-tertiary) 30%, transparent); border: 1px solid var(--border-subtle); }

.topo-scroll { overflow-x: auto; overflow-y: visible; padding-bottom: 1rem; }
.topo-svg { display: block; font-family: var(--font-mono); overflow: visible; }

/* SVG elements */
:global(.col-header) { font-size: 9px; fill: var(--text-tertiary); font-family: var(--font-mono); letter-spacing: 0.1em; text-transform: uppercase; }
:global(.node-rect) { stroke-width: 1.5; }
:global(.local-rect) { fill: var(--bg-raised); stroke: var(--accent); }
:global(.remote-rect) { fill: var(--bg-raised); stroke: var(--border-subtle); }
:global(.node-name) { font-size: 11px; fill: var(--text-primary); font-family: var(--font-mono); font-weight: 500; }
:global(.remote-name) { fill: var(--text-secondary); }
:global(.node-port) { font-size: 10px; fill: var(--text-tertiary); font-family: var(--font-mono); }
:global(.remote-port) { fill: var(--text-tertiary); }
:global(.node-dot) { }
:global(.connector-dot) { fill: var(--border-subtle); }
:global(.edge-path) { stroke: var(--accent); stroke-width: 1.5; stroke-opacity: 0.5; }
:global(.edge-count-bg) { fill: var(--bg-raised); stroke: var(--border-subtle); stroke-width: 1; }
:global(.edge-count-text) { font-size: 9px; fill: var(--text-tertiary); font-family: var(--font-mono); }

.topo-footer { margin-top: 1rem; }
.topo-note { font-size: 0.78rem; color: var(--text-tertiary); }

/* ── Badges / banners ── */
.badge-warn {
  font-family: var(--font-mono); font-size: 0.7rem;
  background: color-mix(in srgb, #f59e0b 15%, transparent);
  color: #f59e0b; padding: 3px 8px; border-radius: 4px;
}
.error-banner {
  background: color-mix(in srgb, #ef4444 12%, transparent);
  border: 1px solid color-mix(in srgb, #ef4444 30%, transparent);
  color: #ef4444; padding: 0.75rem 1rem; border-radius: 6px; margin-bottom: 1rem;
  font-size: 0.82rem;
}
.info-banner {
  background: color-mix(in srgb, #f59e0b 10%, transparent);
  border: 1px solid color-mix(in srgb, #f59e0b 25%, transparent);
  color: var(--text-secondary); padding: 0.75rem 1rem; border-radius: 6px; margin-bottom: 1rem;
  font-size: 0.82rem; line-height: 1.6;
}
.info-banner code {
  font-family: var(--font-mono); font-size: 0.78rem;
  background: var(--bg-raised); padding: 1px 5px; border-radius: 3px;
}
.empty-state { padding: 3rem; text-align: center; color: var(--text-tertiary); font-size: 0.85rem; }

/* ── Buttons ── */
.btn-refresh {
  font-family: var(--font-mono); font-size: 0.78rem;
  background: var(--bg-raised); border: 1px solid var(--border-subtle);
  color: var(--text-secondary); padding: 0.4rem 0.85rem; border-radius: 4px;
  cursor: pointer; transition: border-color 0.12s, color 0.12s;
}
.btn-refresh:hover:not(:disabled) { border-color: var(--accent); color: var(--accent); }
.btn-refresh:disabled { opacity: 0.5; cursor: default; }

/* ── Utilities ── */
.mono { font-family: var(--font-mono); font-size: 0.8rem; }
</style>
