<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { api } from '$lib/api';
  import CLIEchoPane from '$components/CLIEchoPane.svelte';

  interface Addr { ip: string; prefix: number; family: string; scope: string; }
  interface Iface {
    name: string; type: string; state: string; flags: string[];
    mac: string; mtu: number; addresses: Addr[];
    rx_bytes: number; tx_bytes: number; rx_packets: number; tx_packets: number;
    rx_errors: number; tx_errors: number;
    speed: string; duplex: string; driver: string;
    master: string; vlan_of: string; vlan_id: number;
  }
  interface Route { destination: string; gateway: string; iface: string; metric: number; flags: string; family: string; }

  interface BWSample { rx: number; tx: number; ts: number; }

  const HISTORY = 60; // seconds of history to show

  let ifaces: Iface[] = $state([]);
  let routes: Route[] = $state([]);
  let loading = $state(true);
  let error = $state('');
  let tab = $state<'interfaces'|'routes'>('interfaces');
  let expandedIface = $state<string|null>(null);
  let actionPending = $state<string|null>(null);

  // Bandwidth streams: ifaceName → samples[]
  let bwSamples = $state<Record<string, BWSample[]>>({});
  let bwCurrent = $state<Record<string, {rx: number, tx: number}>>({});
  let sseConnections: Record<string, EventSource> = {};

  // Add address modal
  let showAddAddr = $state(false);
  let addAddrIface = $state('');
  let addAddrCIDR = $state('');
  let addAddrError = $state('');

  async function load() {
    loading = true; error = '';
    try {
      const [ifRes, rtRes] = await Promise.all([
        api.get<any>('/api/network/interfaces'),
        api.get<any>('/api/network/routes'),
      ]);
      ifaces = ifRes.interfaces ?? [];
      routes = rtRes.routes ?? [];
    } catch(e: any) { error = e.message; }
    finally { loading = false; }
  }

  function startBWStream(name: string) {
    if (sseConnections[name]) return; // already streaming
    bwSamples[name] = [];
    bwCurrent[name] = { rx: 0, tx: 0 };

    const es = new EventSource(`/api/network/interfaces/${encodeURIComponent(name)}/stats/stream`);
    es.onmessage = (e) => {
      try {
        const d = JSON.parse(e.data);
        const sample: BWSample = { rx: d.rx_bytes_sec, tx: d.tx_bytes_sec, ts: d.ts };

        bwSamples = {
          ...bwSamples,
          [name]: [...(bwSamples[name] ?? []).slice(-(HISTORY - 1)), sample]
        };
        bwCurrent = {
          ...bwCurrent,
          [name]: { rx: d.rx_bytes_sec, tx: d.tx_bytes_sec }
        };
      } catch {}
    };
    sseConnections[name] = es;
  }

  function stopBWStream(name: string) {
    if (sseConnections[name]) {
      sseConnections[name].close();
      delete sseConnections[name];
    }
  }

  function toggleExpand(name: string) {
    if (expandedIface === name) {
      expandedIface = null;
      stopBWStream(name);
    } else {
      expandedIface = name;
      startBWStream(name);
    }
  }

  // Build SVG sparkline path from samples
  function sparklinePath(samples: BWSample[], key: 'rx'|'tx', w: number, h: number): string {
    if (!samples || samples.length < 2) return '';
    const vals = samples.map(s => s[key]);
    const max = Math.max(...vals, 1024); // minimum scale of 1KB/s
    const points = vals.map((v, i) => {
      const x = (i / (HISTORY - 1)) * w;
      const y = h - (v / max) * h;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    });
    return `M ${points.join(' L ')}`;
  }

  function sparklineArea(samples: BWSample[], key: 'rx'|'tx', w: number, h: number): string {
    if (!samples || samples.length < 2) return '';
    const vals = samples.map(s => s[key]);
    const max = Math.max(...vals, 1024);
    const points = vals.map((v, i) => {
      const x = (i / (HISTORY - 1)) * w;
      const y = h - (v / max) * h;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    });
    const lastX = ((vals.length - 1) / (HISTORY - 1)) * w;
    return `M 0,${h} L ${points.join(' L ')} L ${lastX.toFixed(1)},${h} Z`;
  }

  async function setState(name: string, state: 'up'|'down') {
    actionPending = name + ':' + state;
    try {
      await api.post(`/api/network/interfaces/${name}/state`, { state });
      await load();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  async function setMTU(name: string) {
    const val = prompt(`New MTU for ${name}:`, '1500');
    if (!val) return;
    const mtu = parseInt(val);
    if (isNaN(mtu) || mtu < 68 || mtu > 9000) { error = 'MTU must be between 68 and 9000'; return; }
    actionPending = name + ':mtu';
    try {
      await api.post(`/api/network/interfaces/${name}/mtu`, { mtu });
      await load();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  async function addAddress() {
    if (!addAddrCIDR) { addAddrError = 'CIDR required (e.g. 192.168.1.100/24)'; return; }
    addAddrError = '';
    try {
      await api.post(`/api/network/interfaces/${addAddrIface}/addresses`, { cidr: addAddrCIDR });
      showAddAddr = false; addAddrCIDR = '';
      await load();
    } catch(e: any) { addAddrError = e.message; }
  }

  async function delAddress(name: string, cidr: string) {
    if (!confirm(`Remove ${cidr} from ${name}?`)) return;
    actionPending = name + ':' + cidr;
    try {
      await api.delete(`/api/network/interfaces/${name}/addresses`);
      await load();
    } catch(e: any) { error = e.message; }
    finally { actionPending = null; }
  }

  function fmtBytes(b: number): string {
    if (b > 1e9) return (b/1e9).toFixed(2) + ' GB';
    if (b > 1e6) return (b/1e6).toFixed(2) + ' MB';
    if (b > 1e3) return (b/1e3).toFixed(1) + ' KB';
    return b + ' B';
  }

  function fmtRate(bps: number): string {
    if (bps > 1e9) return (bps/1e9).toFixed(2) + ' GB/s';
    if (bps > 1e6) return (bps/1e6).toFixed(2) + ' MB/s';
    if (bps > 1e3) return (bps/1e3).toFixed(1) + ' KB/s';
    return bps.toFixed(0) + ' B/s';
  }

  function stateClass(state: string) {
    if (state === 'up') return 'badge-green';
    if (state === 'down') return 'badge-red';
    return 'badge-yellow';
  }

  function typeIcon(type: string) {
    const icons: Record<string,string> = {
      ethernet:'⬡', loopback:'↺', wireless:'⌘', bridge:'⬢',
      bond:'⬡', veth:'⤷', tun:'⇌', vlan:'◈', dummy:'○', unknown:'?'
    };
    return icons[type] ?? '?';
  }

  onMount(load);
  onDestroy(() => {
    Object.keys(sseConnections).forEach(stopBWStream);
  });
</script>

<div class="net-page">
  <div class="page-header">
    <div>
      <h1>Network</h1>
      <p class="subtitle">{ifaces.length} interfaces · {routes.length} routes</p>
    </div>
    <div class="actions">
      <button class="btn" onclick={load} disabled={loading}>⟳ Refresh</button>
    </div>
  </div>

  {#if error}
    <div class="alert alert-error" style="margin-bottom:1rem">{error}</div>
  {/if}

  <div class="tab-bar">
    <button class="tab-btn" class:active={tab==='interfaces'} onclick={() => tab='interfaces'}>Interfaces</button>
    <button class="tab-btn" class:active={tab==='routes'} onclick={() => tab='routes'}>Routes</button>
  </div>

  {#if tab === 'interfaces'}
    <div class="iface-list">
      {#if loading}
        {#each [1,2,3] as _}
          <div class="card iface-card skeleton" style="height:72px"></div>
        {/each}
      {:else if ifaces.length === 0}
        <div class="empty">No interfaces found</div>
      {:else}
        {#each ifaces as iface (iface.name)}
          <div class="card iface-card" class:expanded={expandedIface === iface.name}>
            <!-- Summary row -->
            <div class="iface-summary" onclick={() => toggleExpand(iface.name)}>
              <span class="iface-type-icon" title={iface.type}>{typeIcon(iface.type)}</span>
              <span class="iface-name mono">{iface.name}</span>
              <span class="badge {stateClass(iface.state)}">{iface.state}</span>
              <span class="badge badge-gray">{iface.type}</span>
              <div class="iface-addrs">
                {#each (iface.addresses ?? []).filter(a => a.family === 'inet') as addr}
                  <span class="mono addr-chip">{addr.ip}/{addr.prefix}</span>
                {/each}
                {#if !iface.addresses?.filter(a => a.family==='inet').length}
                  <span style="color:var(--text-tertiary);font-size:0.72rem">no address</span>
                {/if}
              </div>
              <div class="iface-stats">
                <span title="RX">↓ {fmtBytes(iface.rx_bytes)}</span>
                <span title="TX">↑ {fmtBytes(iface.tx_bytes)}</span>
              </div>
              <span class="expand-chevron">{expandedIface === iface.name ? '▾' : '▸'}</span>
            </div>

            <!-- Expanded detail -->
            {#if expandedIface === iface.name}
              <div class="iface-detail">
                <div class="detail-grid">
                  <div class="detail-col">
                    <h3>Properties</h3>
                    <table class="detail-table"><tbody>
                      <tr><td>MAC</td><td class="mono">{iface.mac || '—'}</td></tr>
                      <tr><td>MTU</td><td class="mono">{iface.mtu}</td></tr>
                      {#if iface.speed && iface.speed !== '-1' && iface.speed !== '4294967295'}
                        <tr><td>Speed</td><td class="mono">{iface.speed} Mbps</td></tr>
                      {/if}
                      {#if iface.duplex}<tr><td>Duplex</td><td class="mono">{iface.duplex}</td></tr>{/if}
                      {#if iface.driver}<tr><td>Driver</td><td class="mono">{iface.driver}</td></tr>{/if}
                      {#if iface.master}<tr><td>Master</td><td class="mono">{iface.master}</td></tr>{/if}
                      {#if iface.vlan_of}<tr><td>VLAN of</td><td class="mono">{iface.vlan_of} (ID {iface.vlan_id})</td></tr>{/if}
                    </tbody></table>
                  </div>

                  <div class="detail-col">
                    <h3>Addresses</h3>
                    {#each (iface.addresses ?? []) as addr}
                      <div class="addr-row">
                        <span class="badge {addr.family === 'inet6' ? 'badge-purple' : 'badge-blue'}">{addr.family}</span>
                        <span class="mono">{addr.ip}/{addr.prefix}</span>
                        <span class="badge badge-gray">{addr.scope}</span>
                        <button class="btn btn-ghost" style="font-size:0.68rem;padding:0.15rem 0.35rem"
                          onclick={() => delAddress(iface.name, `${addr.ip}/${addr.prefix}`)}>✕</button>
                      </div>
                    {/each}
                    <button class="btn btn-ghost" style="margin-top:0.5rem;font-size:0.75rem"
                      onclick={() => { showAddAddr=true; addAddrIface=iface.name; }}>+ Add address</button>
                  </div>

                  <div class="detail-col">
                    <h3>Statistics</h3>
                    <table class="detail-table"><tbody>
                      <tr><td>RX bytes</td><td class="mono">{fmtBytes(iface.rx_bytes)}</td></tr>
                      <tr><td>TX bytes</td><td class="mono">{fmtBytes(iface.tx_bytes)}</td></tr>
                      <tr><td>RX packets</td><td class="mono">{iface.rx_packets.toLocaleString()}</td></tr>
                      <tr><td>TX packets</td><td class="mono">{iface.tx_packets.toLocaleString()}</td></tr>
                      <tr><td>RX errors</td><td class="mono" style="color:{iface.rx_errors>0?'var(--red)':'inherit'}">{iface.rx_errors}</td></tr>
                      <tr><td>TX errors</td><td class="mono" style="color:{iface.tx_errors>0?'var(--red)':'inherit'}">{iface.tx_errors}</td></tr>
                    </tbody></table>
                  </div>
                </div>

                <!-- Bandwidth graph -->
                <div class="bw-graph">
                  <div class="bw-header">
                    <span class="bw-title">Bandwidth — live (60s window)</span>
                    <div class="bw-current">
                      <span class="bw-rx">↓ {fmtRate(bwCurrent[iface.name]?.rx ?? 0)}</span>
                      <span class="bw-tx">↑ {fmtRate(bwCurrent[iface.name]?.tx ?? 0)}</span>
                    </div>
                  </div>
                  <svg class="bw-svg" viewBox="0 0 600 80" preserveAspectRatio="none">
                    <!-- RX area (green) -->
                    <path
                      d={sparklineArea(bwSamples[iface.name] ?? [], 'rx', 600, 80)}
                      fill="rgba(61,186,114,0.15)"
                    />
                    <path
                      d={sparklinePath(bwSamples[iface.name] ?? [], 'rx', 600, 80)}
                      fill="none" stroke="var(--green)" stroke-width="1.5"
                    />
                    <!-- TX area (amber) -->
                    <path
                      d={sparklineArea(bwSamples[iface.name] ?? [], 'tx', 600, 80)}
                      fill="rgba(240,160,48,0.12)"
                    />
                    <path
                      d={sparklinePath(bwSamples[iface.name] ?? [], 'tx', 600, 80)}
                      fill="none" stroke="var(--accent)" stroke-width="1.5"
                    />
                    {#if !bwSamples[iface.name]?.length}
                      <text x="300" y="44" text-anchor="middle" fill="var(--text-tertiary)" font-size="11">
                        Collecting data…
                      </text>
                    {/if}
                  </svg>
                  <div class="bw-legend">
                    <span class="legend-rx">↓ RX (receive)</span>
                    <span class="legend-tx">↑ TX (transmit)</span>
                  </div>
                </div>

                <div class="iface-actions">
                  {#if iface.state === 'up' || iface.state === 'no-carrier'}
                    <button class="btn" disabled={actionPending !== null}
                      onclick={() => setState(iface.name, 'down')}>■ Bring down</button>
                  {:else}
                    <button class="btn btn-primary" disabled={actionPending !== null}
                      onclick={() => setState(iface.name, 'up')}>▶ Bring up</button>
                  {/if}
                  <button class="btn btn-ghost" disabled={actionPending !== null}
                    onclick={() => setMTU(iface.name)}>Set MTU</button>
                </div>
              </div>
            {/if}
          </div>
        {/each}
      {/if}
    </div>

  {:else}
    <!-- Routes tab -->
    <div class="card" style="padding:0;overflow-x:auto">
      <table class="data-table">
        <thead>
          <tr>
            <th>Destination</th>
            <th>Gateway</th>
            <th>Interface</th>
            <th>Metric</th>
            <th>Family</th>
          </tr>
        </thead>
        <tbody>
          {#if loading}
            {#each [1,2,3,4] as _}
              <tr>{#each [1,2,3,4,5] as _}<td><div class="skeleton" style="height:13px;width:80%"></div></td>{/each}</tr>
            {/each}
          {:else if routes.length === 0}
            <tr><td colspan="5" style="text-align:center;padding:2rem;color:var(--text-tertiary)">No routes found</td></tr>
          {:else}
            {#each routes as rt}
              <tr>
                <td class="mono" style="font-weight:500">{rt.destination}</td>
                <td class="mono" style="color:var(--text-secondary)">{rt.gateway === '0.0.0.0' || rt.gateway === '::' ? '—' : rt.gateway}</td>
                <td class="mono">{rt.iface}</td>
                <td class="mono" style="color:var(--text-tertiary)">{rt.metric}</td>
                <td><span class="badge {rt.family === 'inet6' ? 'badge-purple' : 'badge-blue'}">{rt.family}</span></td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>
  {/if}

  <!-- Add address modal -->
  {#if showAddAddr}
    <div class="modal-overlay" role="button" tabindex="0"
      onkeydown={(e) => e.key === 'Escape' && (showAddAddr = false)}
      onclick={() => showAddAddr = false}>
      <div class="modal" role="dialog" onclick={(e) => e.stopPropagation()}>
        <div class="modal-header">
          <h2>Add address to {addAddrIface}</h2>
          <button class="btn btn-ghost" onclick={() => showAddAddr = false}>✕</button>
        </div>
        <div class="modal-body">
          {#if addAddrError}<div class="alert alert-error" style="margin-bottom:0.75rem">{addAddrError}</div>{/if}
          <div class="form-row">
            <label for="addr-cidr">CIDR address</label>
            <input id="addr-cidr" class="search-input" bind:value={addAddrCIDR}
              placeholder="192.168.1.100/24" />
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn" onclick={() => showAddAddr = false}>Cancel</button>
          <button class="btn btn-primary" onclick={addAddress}>Add address</button>
        </div>
      </div>
    </div>
  {/if}

  <CLIEchoPane context="network" />
</div>

<style>
.bw-graph {
  border: 1px solid var(--border-subtle);
  border-radius: var(--r-md);
  overflow: hidden;
  margin-bottom: 0.75rem;
}
.bw-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.4rem 0.75rem;
  background: var(--bg-raised);
  border-bottom: 1px solid var(--border-subtle);
}
.bw-title { font-size: 0.72rem; color: var(--text-tertiary); font-weight: 500; text-transform: uppercase; letter-spacing: 0.05em; }
.bw-current { display: flex; gap: 1rem; font-family: var(--font-mono); font-size: 0.78rem; }
.bw-rx { color: var(--green); }
.bw-tx { color: var(--accent); }
.bw-svg { display: block; width: 100%; height: 80px; background: var(--bg-base); }
.bw-legend {
  display: flex;
  gap: 1.25rem;
  padding: 0.3rem 0.75rem;
  background: var(--bg-raised);
  border-top: 1px solid var(--border-subtle);
}
.legend-rx { font-size: 0.68rem; color: var(--green); font-family: var(--font-mono); }
.legend-tx { font-size: 0.68rem; color: var(--accent); font-family: var(--font-mono); }

.net-page { max-width:1100px; padding-bottom:220px; }
.tab-bar { display:flex; border-bottom:1px solid var(--border-subtle); margin-bottom:0.75rem; }
.tab-btn { padding:0.5rem 1rem; background:none; border:none; border-bottom:2px solid transparent; cursor:pointer; font-size:0.85rem; color:var(--text-secondary); margin-bottom:-1px; }
.tab-btn.active { color:var(--accent); border-bottom-color:var(--accent); font-weight:500; }

.iface-list { display:flex; flex-direction:column; gap:0.5rem; }

.iface-card { padding:0; cursor:pointer; transition:border-color 0.15s; }
.iface-card:hover { border-color:var(--border-default); }
.iface-card.expanded { border-color:var(--accent); }

.iface-summary {
  display:flex; align-items:center; gap:0.625rem;
  padding:0.75rem 1rem; flex-wrap:wrap;
}
.iface-type-icon { font-size:1rem; color:var(--text-tertiary); flex-shrink:0; }
.iface-name { font-weight:600; font-size:0.9rem; flex-shrink:0; }
.iface-addrs { display:flex; gap:0.25rem; flex-wrap:wrap; flex:1; }
.addr-chip { font-size:0.75rem; color:var(--text-secondary); }
.iface-stats { display:flex; gap:0.75rem; font-size:0.72rem; font-family:var(--font-mono); color:var(--text-tertiary); flex-shrink:0; }
.expand-chevron { margin-left:auto; color:var(--text-tertiary); font-size:0.75rem; flex-shrink:0; }

.iface-detail { border-top:1px solid var(--border-subtle); padding:1rem; }
.detail-grid { display:grid; grid-template-columns:repeat(auto-fill,minmax(220px,1fr)); gap:1rem; margin-bottom:1rem; }
.detail-col h3 { margin-bottom:0.5rem; }
.detail-table { width:100%; font-size:0.78rem; border-collapse:collapse; }
.detail-table td { padding:0.2rem 0.375rem; }
.detail-table td:first-child { color:var(--text-tertiary); width:40%; }

.addr-row { display:flex; align-items:center; gap:0.375rem; margin-bottom:0.25rem; font-size:0.78rem; }

.iface-actions { display:flex; gap:0.5rem; padding-top:0.75rem; border-top:1px solid var(--border-subtle); }

.empty { text-align:center; padding:2rem; color:var(--text-tertiary); }

.modal-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.6); display:flex; align-items:center; justify-content:center; z-index:500; }
.modal { background:var(--bg-panel); border:1px solid var(--border-default); border-radius:var(--r-lg); width:380px; max-width:95vw; }
.modal-header { display:flex; justify-content:space-between; align-items:center; padding:1rem; border-bottom:1px solid var(--border-subtle); }
.modal-header h2 { font-size:1rem; margin:0; }
.modal-body { padding:1rem; }
.modal-footer { display:flex; justify-content:flex-end; gap:0.5rem; padding:1rem; border-top:1px solid var(--border-subtle); }
.form-row { display:flex; flex-direction:column; gap:0.25rem; }
.form-row label { font-size:0.75rem; color:var(--text-secondary); font-weight:500; }
</style>
