<script lang="ts">
  import { onMount } from 'svelte';

  interface CertInfo {
    path: string;
    source: string;
    subject: string;
    issuer: string;
    self_signed: boolean;
    dns_names: string[] | null;
    ip_addresses: string[] | null;
    not_before: string;
    not_after: string;
    days_left: number;
    expired: boolean;
    key_type: string;
    bit_size: number;
  }

  type Tab = 'certs' | 'generate-csr' | 'self-signed';

  let tab = $state<Tab>('certs');
  let certs = $state<CertInfo[]>([]);
  let loading = $state(true);
  let selected = $state<CertInfo | null>(null);
  let pemContent = $state('');
  let pemLoading = $state(false);
  let copyMsg = $state('');
  let certSearch = $state('');

  // CSR form
  let csrCN = $state('');
  let csrOrg = $state('');
  let csrCountry = $state('');
  let csrState = $state('');
  let csrLocality = $state('');
  let csrSANs = $state('');
  let csrKeyType = $state('rsa');
  let csrKeySize = $state(2048);
  let csrResult = $state<{private_key_pem: string; csr_pem: string} | null>(null);
  let csrLoading = $state(false);
  let csrError = $state('');

  // Self-signed form
  let ssFields = $state({
    cn: '', org: '', country: '', state: '', locality: '',
    sans: '', key_type: 'rsa', key_size: 2048, days: 365,
    install: false, install_name: '',
  });
  let ssResult = $state<{private_key_pem: string; cert_pem: string; install_output?: string; install_error?: string} | null>(null);
  let ssLoading = $state(false);
  let ssError = $state('');

  onMount(() => loadCerts());

  async function loadCerts() {
    loading = true;
    try {
      const res = await fetch('/api/ssl/certs');
      const raw: CertInfo[] = await res.json();
      // Sort ascending by days_left so expiring certs surface first; expired come first (negative days)
      certs = raw.sort((a, b) => a.days_left - b.days_left);
      if (certs.length > 0 && !selected) selectCert(certs[0]);
    } finally {
      loading = false;
    }
  }

  async function selectCert(c: CertInfo) {
    selected = c;
    pemContent = '';
    pemLoading = true;
    try {
      const res = await fetch(`/api/ssl/cert/pem?path=${encodeURIComponent(c.path)}`);
      if (res.ok) {
        const data = await res.json();
        pemContent = data.pem;
      }
    } finally {
      pemLoading = false;
    }
  }

  async function generateCSR() {
    csrLoading = true; csrError = ''; csrResult = null;
    try {
      const res = await fetch('/api/ssl/csr', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          common_name: csrCN,
          organization: csrOrg,
          country: csrCountry,
          state: csrState,
          locality: csrLocality,
          sans: csrSANs.split(',').map(s => s.trim()).filter(Boolean),
          key_type: csrKeyType,
          key_size: csrKeySize,
        }),
      });
      if (!res.ok) { csrError = await res.text(); return; }
      csrResult = await res.json();
    } catch (e) {
      csrError = String(e);
    } finally {
      csrLoading = false;
    }
  }

  async function generateSelfSigned() {
    ssLoading = true; ssError = ''; ssResult = null;
    try {
      const res = await fetch('/api/ssl/self-signed', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          common_name: ssFields.cn,
          organization: ssFields.org,
          country: ssFields.country,
          state: ssFields.state,
          locality: ssFields.locality,
          sans: ssFields.sans.split(',').map(s => s.trim()).filter(Boolean),
          key_type: ssFields.key_type,
          key_size: ssFields.key_size,
          days: ssFields.days,
          install: ssFields.install,
          install_name: ssFields.install_name || ssFields.cn,
        }),
      });
      if (!res.ok) { ssError = await res.text(); return; }
      ssResult = await res.json();
      if (ssResult?.install_output) loadCerts(); // refresh cert list if we just installed one
    } catch (e) {
      ssError = String(e);
    } finally {
      ssLoading = false;
    }
  }

  async function copyToClipboard(text: string, label: string) {
    await navigator.clipboard.writeText(text);
    copyMsg = label + ' copied!';
    setTimeout(() => { copyMsg = ''; }, 2000);
  }

  function downloadFile(content: string, filename: string) {
    const blob = new Blob([content], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url; a.download = filename; a.click();
    URL.revokeObjectURL(url);
  }

  function daysColor(c: CertInfo): string {
    if (c.expired) return '#f87171';
    if (c.days_left <= 14) return '#f87171';
    if (c.days_left <= 30) return '#fb923c';
    if (c.days_left <= 90) return '#facc15';
    return '#4ade80';
  }

  function sourceLabel(s: string): string {
    const map: Record<string, string> = {
      letsencrypt: "Let's Encrypt", nginx: 'nginx', apache: 'Apache',
      system: 'System', haproxy: 'HAProxy', manual: 'Manual',
    };
    return map[s] ?? s;
  }

  function fmtDate(d: string): string {
    return new Date(d).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
  }

  let filteredCerts = $derived(() => {
    const q = certSearch.toLowerCase().trim();
    if (!q) return certs;
    return certs.filter(c =>
      c.subject?.toLowerCase().includes(q) ||
      c.issuer?.toLowerCase().includes(q) ||
      c.path.toLowerCase().includes(q) ||
      c.source.toLowerCase().includes(q) ||
      c.dns_names?.some(d => d.toLowerCase().includes(q)) ||
      c.ip_addresses?.some(ip => ip.includes(q))
    );
  });

  let grouped = $derived(() => {
    const g: Record<string, CertInfo[]> = {};
    for (const c of filteredCerts()) {
      const lbl = sourceLabel(c.source);
      (g[lbl] ||= []).push(c);
    }
    return g;
  });
</script>

<div class="ssl-wrap">
  <!-- Tab bar -->
  <div class="ssl-tabs">
    <button class="ssl-tab" class:active={tab === 'certs'} onclick={() => tab = 'certs'}>
      ◈ Certificates
    </button>
    <button class="ssl-tab" class:active={tab === 'generate-csr'} onclick={() => tab = 'generate-csr'}>
      ⊕ Generate CSR
    </button>
    <button class="ssl-tab" class:active={tab === 'self-signed'} onclick={() => tab = 'self-signed'}>
      ⊕ Self-Signed Cert
    </button>
  </div>

  <!-- CERTS TAB -->
  {#if tab === 'certs'}
    <div class="certs-layout">
      <!-- Left panel: cert list -->
      <div class="cert-list-panel">
        <div class="cert-list-header">
          <span>Found {certs.length} certificate{certs.length !== 1 ? 's' : ''}</span>
          <button class="btn-sm" onclick={loadCerts}>↻</button>
        </div>
        <div class="cert-search-wrap">
          <input
            class="cert-search"
            placeholder="Search certs…"
            bind:value={certSearch}
          />
        </div>

        {#if loading}
          <div class="list-msg">Scanning…</div>
        {:else if !certs.length}
          <div class="list-msg">No certificates found.</div>
        {:else if !filteredCerts().length}
          <div class="list-msg">No results for "{certSearch}".</div>
        {:else}
          {#each Object.entries(grouped()) as [group, items]}
            <div class="cert-group-label">{group}</div>
            {#each items as c}
              <button
                class="cert-row"
                class:active={selected?.path === c.path}
                onclick={() => selectCert(c)}
              >
                <div class="cert-row-top">
                  <span class="cert-cn">{c.subject || c.path.split('/').pop()}</span>
                  <span class="days-badge" style="color: {daysColor(c)}">
                    {c.expired ? 'EXPIRED' : c.days_left + 'd'}
                  </span>
                </div>
                <div class="cert-row-sub">{c.path}</div>
              </button>
            {/each}
          {/each}
        {/if}
      </div>

      <!-- Right panel: cert detail -->
      <div class="cert-detail-panel">
        {#if !selected}
          <div class="detail-empty">Select a certificate to view details</div>
        {:else}
          <div class="detail-header">
            <h2 class="detail-cn">{selected.subject || '(no CN)'}</h2>
            {#if selected.self_signed}
              <span class="tag-selfsigned">Self-Signed</span>
            {/if}
            {#if selected.expired}
              <span class="tag-expired">Expired</span>
            {:else if selected.days_left <= 30}
              <span class="tag-expiring">Expiring Soon</span>
            {/if}
          </div>

          <div class="detail-grid">
            <div class="dg-row">
              <span class="dg-label">Path</span>
              <span class="dg-val mono">{selected.path}</span>
            </div>
            <div class="dg-row">
              <span class="dg-label">Issuer</span>
              <span class="dg-val">{selected.issuer || '—'}</span>
            </div>
            <div class="dg-row">
              <span class="dg-label">Valid From</span>
              <span class="dg-val">{fmtDate(selected.not_before)}</span>
            </div>
            <div class="dg-row">
              <span class="dg-label">Valid Until</span>
              <span class="dg-val" style="color: {daysColor(selected)}">
                {fmtDate(selected.not_after)} ({selected.expired ? 'EXPIRED' : selected.days_left + ' days left'})
              </span>
            </div>
            <div class="dg-row">
              <span class="dg-label">Key</span>
              <span class="dg-val">{selected.key_type}{selected.bit_size ? ' ' + selected.bit_size + '-bit' : ''}</span>
            </div>
            {#if selected.dns_names?.length}
              <div class="dg-row">
                <span class="dg-label">DNS SANs</span>
                <span class="dg-val">
                  {#each selected.dns_names as dns}
                    <span class="san-chip">{dns}</span>
                  {/each}
                </span>
              </div>
            {/if}
            {#if selected.ip_addresses?.length}
              <div class="dg-row">
                <span class="dg-label">IP SANs</span>
                <span class="dg-val">
                  {#each selected.ip_addresses as ip}
                    <span class="san-chip">{ip}</span>
                  {/each}
                </span>
              </div>
            {/if}
          </div>

          <div class="pem-section">
            <div class="pem-header">
              <span class="pem-title">Certificate PEM</span>
              <div class="pem-actions">
                {#if pemContent}
                  <button class="btn-sm" onclick={() => copyToClipboard(pemContent, 'PEM')}>Copy</button>
                  <button class="btn-sm" onclick={() => downloadFile(pemContent, (selected?.subject || 'cert') + '.pem')}>Download</button>
                {/if}
              </div>
            </div>
            {#if pemLoading}
              <div class="pem-loading">Loading PEM…</div>
            {:else if pemContent}
              <textarea class="pem-area" readonly value={pemContent}></textarea>
            {:else}
              <div class="pem-loading">Could not read PEM (permission denied or not a cert file)</div>
            {/if}
          </div>
        {/if}
      </div>
    </div>

  <!-- CSR TAB -->
  {:else if tab === 'generate-csr'}
    <div class="gen-wrap">
      <div class="gen-panel">
        <h2 class="gen-title">Generate CSR + Private Key</h2>
        <p class="gen-desc">Creates a Certificate Signing Request to send to a CA, and a matching private key.</p>

        <div class="form-grid">
          <label class="field">
            <span class="field-label">Common Name (CN) *</span>
            <input class="field-input" bind:value={csrCN} placeholder="example.com" />
          </label>
          <label class="field">
            <span class="field-label">Organization</span>
            <input class="field-input" bind:value={csrOrg} placeholder="Acme Corp" />
          </label>
          <label class="field">
            <span class="field-label">Country (2-letter)</span>
            <input class="field-input" bind:value={csrCountry} placeholder="US" maxlength="2" />
          </label>
          <label class="field">
            <span class="field-label">State / Province</span>
            <input class="field-input" bind:value={csrState} placeholder="California" />
          </label>
          <label class="field">
            <span class="field-label">Locality / City</span>
            <input class="field-input" bind:value={csrLocality} placeholder="San Francisco" />
          </label>
          <label class="field full">
            <span class="field-label">SANs (comma-separated — DNS names or IPs)</span>
            <input class="field-input" bind:value={csrSANs} placeholder="example.com, www.example.com, 192.168.1.1" />
          </label>
          <label class="field">
            <span class="field-label">Key Type</span>
            <select class="field-input" bind:value={csrKeyType}>
              <option value="rsa">RSA</option>
              <option value="ec">ECDSA</option>
            </select>
          </label>
          <label class="field">
            <span class="field-label">{csrKeyType === 'ec' ? 'Curve Bits' : 'Key Size'}</span>
            <select class="field-input" bind:value={csrKeySize}>
              {#if csrKeyType === 'ec'}
                <option value={256}>P-256 (recommended)</option>
                <option value={384}>P-384</option>
                <option value={521}>P-521</option>
              {:else}
                <option value={2048}>2048-bit (recommended)</option>
                <option value={4096}>4096-bit</option>
              {/if}
            </select>
          </label>
        </div>

        {#if csrError}
          <div class="gen-error">{csrError}</div>
        {/if}
        <button class="btn-generate" onclick={generateCSR} disabled={csrLoading || !csrCN}>
          {csrLoading ? 'Generating…' : '⊕ Generate CSR + Key'}
        </button>
      </div>

      {#if csrResult}
        <div class="result-panel">
          {#if copyMsg}<div class="copy-toast">{copyMsg}</div>{/if}
          <div class="result-block">
            <div class="result-block-header">
              <span class="result-block-title">🔑 Private Key</span>
              <div style="display:flex;gap:0.4rem">
                <button class="btn-sm" onclick={() => copyToClipboard(csrResult!.private_key_pem, 'Key')}>Copy</button>
                <button class="btn-sm" onclick={() => downloadFile(csrResult!.private_key_pem, (csrCN || 'cert') + '.key')}>Download</button>
              </div>
            </div>
            <textarea class="pem-area" readonly value={csrResult.private_key_pem}></textarea>
          </div>
          <div class="result-block">
            <div class="result-block-header">
              <span class="result-block-title">📄 CSR</span>
              <div style="display:flex;gap:0.4rem">
                <button class="btn-sm" onclick={() => copyToClipboard(csrResult!.csr_pem, 'CSR')}>Copy</button>
                <button class="btn-sm" onclick={() => downloadFile(csrResult!.csr_pem, (csrCN || 'cert') + '.csr')}>Download</button>
              </div>
            </div>
            <textarea class="pem-area" readonly value={csrResult.csr_pem}></textarea>
          </div>
        </div>
      {/if}
    </div>

  <!-- SELF-SIGNED TAB -->
  {:else if tab === 'self-signed'}
    <div class="gen-wrap">
      <div class="gen-panel">
        <h2 class="gen-title">Generate Self-Signed Certificate</h2>
        <p class="gen-desc">Creates a certificate signed by itself — useful for internal services and development.</p>

        <div class="form-grid">
          <label class="field">
            <span class="field-label">Common Name (CN) *</span>
            <input class="field-input" bind:value={ssFields.cn} placeholder="example.com" />
          </label>
          <label class="field">
            <span class="field-label">Organization</span>
            <input class="field-input" bind:value={ssFields.org} placeholder="Acme Corp" />
          </label>
          <label class="field">
            <span class="field-label">Country (2-letter)</span>
            <input class="field-input" bind:value={ssFields.country} placeholder="US" maxlength="2" />
          </label>
          <label class="field">
            <span class="field-label">State / Province</span>
            <input class="field-input" bind:value={ssFields.state} placeholder="California" />
          </label>
          <label class="field">
            <span class="field-label">Locality / City</span>
            <input class="field-input" bind:value={ssFields.locality} placeholder="San Francisco" />
          </label>
          <label class="field full">
            <span class="field-label">SANs (comma-separated — DNS names or IPs)</span>
            <input class="field-input" bind:value={ssFields.sans} placeholder="example.com, www.example.com, 192.168.1.1" />
          </label>
          <label class="field">
            <span class="field-label">Key Type</span>
            <select class="field-input" bind:value={ssFields.key_type}>
              <option value="rsa">RSA</option>
              <option value="ec">ECDSA</option>
            </select>
          </label>
          <label class="field">
            <span class="field-label">{ssFields.key_type === 'ec' ? 'Curve Bits' : 'Key Size'}</span>
            <select class="field-input" bind:value={ssFields.key_size}>
              {#if ssFields.key_type === 'ec'}
                <option value={256}>P-256 (recommended)</option>
                <option value={384}>P-384</option>
                <option value={521}>P-521</option>
              {:else}
                <option value={2048}>2048-bit (recommended)</option>
                <option value={4096}>4096-bit</option>
              {/if}
            </select>
          </label>
          <label class="field">
            <span class="field-label">Validity (days)</span>
            <input class="field-input" type="number" bind:value={ssFields.days} min="1" max="3650" />
          </label>
        </div>

        <div class="install-section">
          <label class="install-toggle">
            <input type="checkbox" bind:checked={ssFields.install} />
            <span>Install into system trusted CA store</span>
          </label>
          {#if ssFields.install}
            <label class="field" style="margin-top:0.5rem">
              <span class="field-label">Store name (filename, no extension)</span>
              <input
                class="field-input"
                bind:value={ssFields.install_name}
                placeholder={ssFields.cn || 'my-cert'}
              />
            </label>
            <p class="install-hint">
              Installs to the distro's trusted CA directory and runs
              <code>update-ca-certificates</code> / <code>update-ca-trust</code> automatically.
              Requires the server process to have write access (run as root or with sudo).
            </p>
          {/if}
        </div>

        {#if ssError}
          <div class="gen-error">{ssError}</div>
        {/if}
        <button class="btn-generate" onclick={generateSelfSigned} disabled={ssLoading || !ssFields.cn}>
          {ssLoading ? 'Generating…' : '⊕ Generate Certificate'}
        </button>
      </div>

      {#if ssResult}
        <div class="result-panel">
          {#if copyMsg}<div class="copy-toast">{copyMsg}</div>{/if}
          <div class="result-block">
            <div class="result-block-header">
              <span class="result-block-title">🔑 Private Key</span>
              <div style="display:flex;gap:0.4rem">
                <button class="btn-sm" onclick={() => copyToClipboard(ssResult!.private_key_pem, 'Key')}>Copy</button>
                <button class="btn-sm" onclick={() => downloadFile(ssResult!.private_key_pem, (ssFields.cn || 'cert') + '.key')}>Download</button>
              </div>
            </div>
            <textarea class="pem-area" readonly value={ssResult.private_key_pem}></textarea>
          </div>
          <div class="result-block">
            <div class="result-block-header">
              <span class="result-block-title">📜 Certificate</span>
              <div style="display:flex;gap:0.4rem">
                <button class="btn-sm" onclick={() => copyToClipboard(ssResult!.cert_pem, 'Cert')}>Copy</button>
                <button class="btn-sm" onclick={() => downloadFile(ssResult!.cert_pem, (ssFields.cn || 'cert') + '.crt')}>Download</button>
              </div>
            </div>
            <textarea class="pem-area" readonly value={ssResult.cert_pem}></textarea>
          </div>
          {#if ssResult.install_output}
            <div class="install-result install-ok">
              <div class="install-result-title">✓ Installed to system trust store</div>
              <pre class="install-result-pre">{ssResult.install_output}</pre>
            </div>
          {/if}
          {#if ssResult.install_error}
            <div class="install-result install-fail">
              <div class="install-result-title">✕ Install failed</div>
              <pre class="install-result-pre">{ssResult.install_error}</pre>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
.ssl-wrap {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

/* Tabs */
.ssl-tabs {
  display: flex;
  gap: 0.15rem;
  padding: 0.75rem 1.5rem 0;
  border-bottom: 1px solid var(--border, #2a2a2a);
  flex-shrink: 0;
}
.ssl-tab {
  padding: 0.5rem 1.1rem;
  border: none;
  background: none;
  color: var(--text-muted, #888);
  cursor: pointer;
  font-size: 0.85rem;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
}
.ssl-tab:hover { color: var(--text, #e0e0e0); }
.ssl-tab.active {
  color: var(--accent, #6d8aff);
  border-bottom-color: var(--accent, #6d8aff);
  font-weight: 500;
}

/* Certs tab */
.certs-layout {
  display: flex;
  flex: 1;
  overflow: hidden;
}
.cert-list-panel {
  width: 280px;
  flex-shrink: 0;
  border-right: 1px solid var(--border, #2a2a2a);
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}
.cert-list-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.6rem 0.875rem;
  font-size: 0.75rem;
  color: var(--text-muted, #888);
  border-bottom: 1px solid var(--border, #2a2a2a);
  position: sticky;
  top: 0;
  background: var(--surface, #111);
  z-index: 1;
}
.list-msg {
  padding: 1rem;
  color: var(--text-muted, #888);
  font-size: 0.85rem;
}
.cert-search-wrap {
  padding: 0.4rem 0.6rem;
  border-bottom: 1px solid var(--border, #2a2a2a);
}
.cert-search {
  width: 100%;
  box-sizing: border-box;
  background: var(--surface3, #1a1a1a);
  border: 1px solid var(--border, #2a2a2a);
  border-radius: 5px;
  color: var(--text, #e0e0e0);
  font-size: 0.8rem;
  padding: 0.3rem 0.55rem;
}
.cert-search:focus { outline: none; border-color: var(--accent, #6d8aff); }
.cert-group-label {
  padding: 0.5rem 0.875rem 0.25rem;
  font-size: 0.68rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-muted, #888);
}
.cert-row {
  display: block;
  width: 100%;
  text-align: left;
  padding: 0.55rem 0.875rem;
  background: none;
  border: none;
  border-left: 2px solid transparent;
  cursor: pointer;
  color: var(--text, #e0e0e0);
}
.cert-row:hover { background: var(--surface2, #181818); }
.cert-row.active {
  background: var(--surface2, #181818);
  border-left-color: var(--accent, #6d8aff);
}
.cert-row-top {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 0.4rem;
}
.cert-cn {
  font-size: 0.82rem;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.days-badge {
  font-size: 0.7rem;
  font-weight: 700;
  flex-shrink: 0;
}
.cert-row-sub {
  font-size: 0.7rem;
  color: var(--text-muted, #888);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-top: 0.1rem;
}

/* Detail panel */
.cert-detail-panel {
  flex: 1;
  overflow-y: auto;
  padding: 1.25rem 1.5rem;
}
.detail-empty {
  color: var(--text-muted, #888);
  padding-top: 2rem;
}
.detail-header {
  display: flex;
  align-items: center;
  gap: 0.6rem;
  margin-bottom: 1.25rem;
  flex-wrap: wrap;
}
.detail-cn {
  font-size: 1.15rem;
  font-weight: 600;
  margin: 0;
}
.tag-selfsigned {
  font-size: 0.7rem;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
  background: color-mix(in srgb, #888 15%, transparent);
  color: #888;
  font-weight: 600;
}
.tag-expired {
  font-size: 0.7rem;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
  background: color-mix(in srgb, #f87171 15%, transparent);
  color: #f87171;
  font-weight: 600;
}
.tag-expiring {
  font-size: 0.7rem;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
  background: color-mix(in srgb, #fb923c 15%, transparent);
  color: #fb923c;
  font-weight: 600;
}
.detail-grid {
  display: flex;
  flex-direction: column;
  gap: 0;
  background: var(--surface2, #161616);
  border: 1px solid var(--border, #2a2a2a);
  border-radius: 8px;
  margin-bottom: 1.25rem;
  overflow: hidden;
}
.dg-row {
  display: flex;
  padding: 0.6rem 0.875rem;
  gap: 1rem;
  font-size: 0.85rem;
  border-bottom: 1px solid color-mix(in srgb, var(--border, #2a2a2a) 50%, transparent);
}
.dg-row:last-child { border-bottom: none; }
.dg-label {
  width: 100px;
  flex-shrink: 0;
  color: var(--text-muted, #888);
  font-size: 0.78rem;
}
.dg-val { flex: 1; word-break: break-all; }
.dg-val.mono { font-family: monospace; font-size: 0.78rem; }
.san-chip {
  display: inline-block;
  background: var(--surface3, #1e1e1e);
  border: 1px solid var(--border, #2a2a2a);
  border-radius: 4px;
  padding: 0.05rem 0.4rem;
  font-size: 0.78rem;
  font-family: monospace;
  margin: 0.1rem 0.2rem 0.1rem 0;
}
.pem-section { margin-top: 0.5rem; }
.pem-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.5rem;
}
.pem-title { font-size: 0.85rem; font-weight: 500; }
.pem-actions { display: flex; gap: 0.4rem; }
.pem-loading { font-size: 0.82rem; color: var(--text-muted, #888); }
.pem-area {
  width: 100%;
  height: 180px;
  background: var(--surface2, #111);
  border: 1px solid var(--border, #2a2a2a);
  border-radius: 6px;
  color: var(--text, #e0e0e0);
  font-family: monospace;
  font-size: 0.72rem;
  padding: 0.6rem;
  resize: vertical;
  box-sizing: border-box;
}

/* Generate tabs */
.gen-wrap {
  display: flex;
  gap: 1.5rem;
  padding: 1.25rem 1.5rem;
  overflow-y: auto;
  flex: 1;
  flex-wrap: wrap;
}
.gen-panel {
  width: 400px;
  flex-shrink: 0;
}
.gen-title { font-size: 1.05rem; font-weight: 600; margin: 0 0 0.4rem; }
.gen-desc { font-size: 0.82rem; color: var(--text-muted, #888); margin-bottom: 1.25rem; }
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.7rem;
  margin-bottom: 1rem;
}
.field { display: flex; flex-direction: column; gap: 0.25rem; }
.field.full { grid-column: 1 / -1; }
.field-label { font-size: 0.75rem; color: var(--text-muted, #888); }
.field-input {
  background: var(--surface2, #161616);
  border: 1px solid var(--border, #2a2a2a);
  border-radius: 5px;
  color: var(--text, #e0e0e0);
  padding: 0.4rem 0.6rem;
  font-size: 0.85rem;
  width: 100%;
  box-sizing: border-box;
}
.field-input:focus { outline: none; border-color: var(--accent, #6d8aff); }
.gen-error { color: #f87171; font-size: 0.82rem; margin-bottom: 0.75rem; }
.btn-generate {
  padding: 0.5rem 1.25rem;
  background: var(--accent, #6d8aff);
  color: #fff;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-size: 0.88rem;
  font-weight: 500;
}
.btn-generate:disabled { opacity: 0.5; cursor: default; }
.btn-generate:not(:disabled):hover { filter: brightness(1.1); }

.result-panel {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1rem;
  position: relative;
}
.result-block {
  background: var(--surface2, #161616);
  border: 1px solid var(--border, #2a2a2a);
  border-radius: 8px;
  overflow: hidden;
}
.result-block-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.55rem 0.875rem;
  border-bottom: 1px solid var(--border, #2a2a2a);
}
.result-block-title { font-size: 0.85rem; font-weight: 500; }
.result-block .pem-area {
  border: none;
  border-radius: 0;
  background: var(--surface, #111);
}
.copy-toast {
  position: sticky;
  top: 0;
  background: color-mix(in srgb, #4ade80 20%, var(--surface2, #161616));
  border: 1px solid #4ade80;
  border-radius: 6px;
  padding: 0.4rem 0.875rem;
  font-size: 0.82rem;
  color: #4ade80;
  z-index: 10;
}

/* Shared small button */
.btn-sm {
  padding: 0.25rem 0.6rem;
  border: 1px solid var(--border, #333);
  background: var(--surface2, #1e1e1e);
  color: var(--text, #e0e0e0);
  border-radius: 5px;
  cursor: pointer;
  font-size: 0.75rem;
}
.btn-sm:hover { background: var(--surface3, #2a2a2a); }

/* Install section */
.install-section {
  margin: 0.85rem 0 1rem;
  padding: 0.75rem 0.875rem;
  background: var(--surface2, #161616);
  border: 1px solid var(--border, #2a2a2a);
  border-radius: 8px;
}
.install-toggle {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  cursor: pointer;
  font-size: 0.85rem;
}
.install-toggle input[type="checkbox"] { cursor: pointer; }
.install-hint {
  font-size: 0.75rem;
  color: var(--text-muted, #888);
  margin: 0.5rem 0 0;
  line-height: 1.5;
}
.install-hint code {
  font-family: monospace;
  color: var(--text, #e0e0e0);
}
.install-result {
  border-radius: 8px;
  overflow: hidden;
  border: 1px solid;
}
.install-ok  { border-color: #4ade80; }
.install-fail { border-color: #f87171; }
.install-result-title {
  padding: 0.45rem 0.875rem;
  font-size: 0.82rem;
  font-weight: 600;
  border-bottom: 1px solid currentColor;
}
.install-ok .install-result-title  { color: #4ade80; background: color-mix(in srgb, #4ade80 10%, transparent); }
.install-fail .install-result-title { color: #f87171; background: color-mix(in srgb, #f87171 10%, transparent); }
.install-result-pre {
  margin: 0;
  padding: 0.6rem 0.875rem;
  font-family: monospace;
  font-size: 0.75rem;
  white-space: pre-wrap;
  word-break: break-all;
  color: var(--text-muted, #aaa);
  background: var(--surface, #111);
}
</style>
