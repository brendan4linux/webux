<script lang="ts">
  import { onMount } from 'svelte';

  let { onLogin }: { onLogin?: () => void } = $props();

  let username = $state('');
  let password = $state('');
  let error = $state('');
  let loading = $state(false);

  async function login() {
    if (!username || !password) { error = 'Username and password are required'; return; }
    loading = true; error = '';
    try {
      const res = await fetch('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      });
      if (res.ok) {
        onLogin?.();
      } else {
        const data = await res.json().catch(() => ({}));
        error = data.error ?? 'Login failed';
      }
    } catch(e: any) { error = 'Connection error'; }
    finally { loading = false; }
  }
</script>

<div class="splash">
  <div class="splash-card">
    <div class="logo-wrap">
      <svg width="100%" viewBox="0 0 680 420" role="img" xmlns="http://www.w3.org/2000/svg">
        <title>Webux — Linux Panel</title>
        <desc>Webux penguin logo with graduation cap and stethoscope</desc>
        <defs>
          <radialGradient id="lgBody" cx="45%" cy="42%" r="58%">
            <stop offset="0%" stop-color="#2d3550"/>
            <stop offset="100%" stop-color="#141824"/>
          </radialGradient>
          <radialGradient id="lgHead" cx="50%" cy="38%" r="55%">
            <stop offset="0%" stop-color="#323a55"/>
            <stop offset="100%" stop-color="#141824"/>
          </radialGradient>
          <radialGradient id="lgBelly" cx="50%" cy="44%" r="55%">
            <stop offset="0%" stop-color="#f8f5ef"/>
            <stop offset="75%" stop-color="#ede8df"/>
            <stop offset="100%" stop-color="#ddd6ca"/>
          </radialGradient>
          <radialGradient id="lgFace" cx="50%" cy="40%" r="55%">
            <stop offset="0%" stop-color="#f8f5ef"/>
            <stop offset="80%" stop-color="#ede8df"/>
            <stop offset="100%" stop-color="#d9d2c6"/>
          </radialGradient>
          <radialGradient id="lgWingL" cx="60%" cy="35%" r="60%">
            <stop offset="0%" stop-color="#2d3550"/>
            <stop offset="100%" stop-color="#0f1219"/>
          </radialGradient>
          <radialGradient id="lgWingR" cx="40%" cy="35%" r="60%">
            <stop offset="0%" stop-color="#2d3550"/>
            <stop offset="100%" stop-color="#0f1219"/>
          </radialGradient>
          <radialGradient id="lgBeak" cx="40%" cy="30%" r="65%">
            <stop offset="0%" stop-color="#f9c254"/>
            <stop offset="100%" stop-color="#e08c10"/>
          </radialGradient>
          <linearGradient id="lgCap" x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" stop-color="#3b74f5"/>
            <stop offset="100%" stop-color="#1a44b8"/>
          </linearGradient>
          <radialGradient id="lgStethHead" cx="40%" cy="35%" r="60%">
            <stop offset="0%" stop-color="#5ef5b8"/>
            <stop offset="100%" stop-color="#22c47a"/>
          </radialGradient>
          <radialGradient id="lgShadow" cx="50%" cy="30%" r="50%">
            <stop offset="0%" stop-color="#000" stop-opacity="0.28"/>
            <stop offset="100%" stop-color="#000" stop-opacity="0"/>
          </radialGradient>
        </defs>
        <ellipse cx="340" cy="358" rx="92" ry="12" fill="url(#lgShadow)"/>
        <ellipse cx="255" cy="248" rx="26" ry="62" transform="rotate(-14 255 248)" fill="url(#lgWingL)"/>
        <ellipse cx="340" cy="252" rx="90" ry="112" fill="url(#lgBody)"/>
        <ellipse cx="340" cy="268" rx="60" ry="82" fill="url(#lgBelly)" opacity="0.97"/>
        <ellipse cx="425" cy="248" rx="26" ry="62" transform="rotate(14 425 248)" fill="url(#lgWingR)"/>
        <ellipse cx="316" cy="354" rx="26" ry="11" fill="url(#lgBeak)" opacity="0.95"/>
        <ellipse cx="364" cy="357" rx="26" ry="11" fill="url(#lgBeak)" opacity="0.95"/>
        <ellipse cx="340" cy="152" rx="70" ry="67" fill="url(#lgHead)"/>
        <ellipse cx="340" cy="160" rx="46" ry="48" fill="url(#lgFace)" opacity="0.96"/>
        <ellipse cx="340" cy="210" rx="52" ry="18" fill="url(#lgBody)" opacity="0.6"/>
        <circle cx="317" cy="140" r="14" fill="#eeeae2"/>
        <circle cx="363" cy="140" r="14" fill="#eeeae2"/>
        <circle cx="320" cy="138" r="7.5" fill="#141824"/>
        <circle cx="366" cy="138" r="7.5" fill="#141824"/>
        <circle cx="323" cy="134" r="2.8" fill="#fff" opacity="0.9"/>
        <circle cx="369" cy="134" r="2.8" fill="#fff" opacity="0.9"/>
        <ellipse cx="340" cy="164" rx="16" ry="10" fill="url(#lgBeak)"/>
        <ellipse cx="340" cy="168" rx="12" ry="5" fill="#c07010" opacity="0.25"/>
        <rect x="290" y="85" width="100" height="17" rx="4" fill="url(#lgCap)" opacity="0.95"/>
        <polygon points="272,85 340,59 408,85" fill="url(#lgCap)"/>
        <polygon points="272,85 340,65 408,85" fill="#5a8aff" opacity="0.18"/>
        <line x1="390" y1="71" x2="412" y2="110" stroke="#f9c254" stroke-width="2.5" stroke-linecap="round"/>
        <circle cx="413" cy="115" r="6.5" fill="#f4a62a"/>
        <circle cx="413" cy="115" r="3" fill="#f9c254" opacity="0.7"/>
        <circle cx="255" cy="194" r="6" fill="#2ed898"/>
        <circle cx="275" cy="185" r="6" fill="#2ed898"/>
        <path d="M255,194 Q248,222 271,238" fill="none" stroke="#2ed898" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round"/>
        <path d="M275,185 Q283,217 271,238" fill="none" stroke="#2ed898" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round"/>
        <path d="M271,238 Q260,272 264,302 Q267,334 296,348 Q330,364 370,352 Q422,336 436,296 Q450,260 436,226" fill="none" stroke="#2ed898" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round"/>
        <circle cx="436" cy="219" r="17" fill="url(#lgStethHead)" opacity="0.95"/>
        <circle cx="436" cy="219" r="10" fill="#141824"/>
        <circle cx="436" cy="219" r="5.5" fill="#2ed898"/>
        <circle cx="433" cy="216" r="2" fill="#5ef5b8" opacity="0.7"/>
        <text x="340" y="398" text-anchor="middle" font-family="ui-monospace,monospace" font-size="38" font-weight="700" fill="#e8e4dc" letter-spacing="-1">webux</text>
        <text x="340" y="416" text-anchor="middle" font-family="ui-monospace,monospace" font-size="13" font-weight="400" fill="#2ed898" letter-spacing="3">LINUX PANEL</text>
      </svg>
    </div>

    <div class="login-form">
      {#if error}<div class="alert alert-error" style="margin-bottom:0.75rem">{error}</div>{/if}
      <div class="form-row">
        <label for="lf-user">Username</label>
        <input id="lf-user" class="lf-input" type="text" bind:value={username}
          placeholder="root" autocomplete="username"
          onkeydown={(e) => e.key === 'Enter' && login()} />
      </div>
      <div class="form-row">
        <label for="lf-pass">Password</label>
        <input id="lf-pass" class="lf-input" type="password" bind:value={password}
          placeholder="••••••••" autocomplete="current-password"
          onkeydown={(e) => e.key === 'Enter' && login()} />
      </div>
      <button class="lf-btn" onclick={login} disabled={loading}>
        {loading ? 'Signing in…' : 'Sign in'}
      </button>
      <div class="lf-hint">Webux — AGPL-3.0 · access is logged</div>
    </div>
  </div>
</div>

<style>
.splash {
  min-height: 100vh;
  width: 100%;
  background: var(--bg-base);
  display: flex;
  align-items: center;
  justify-content: center;
}
.splash-card {
  width: 380px;
  max-width: 95vw;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0;
}
.logo-wrap {
  width: 100%;
  max-width: 320px;
}
.login-form {
  width: 100%;
  background: var(--bg-panel);
  border: 1px solid var(--border-default);
  border-radius: var(--r-lg);
  padding: 1.5rem;
  display: flex;
  flex-direction: column;
  gap: 1rem;
  margin-top: -1rem;
}
.form-row {
  display: flex;
  flex-direction: column;
  gap: 0.3rem;
}
.form-row label {
  font-size: 0.72rem;
  font-weight: 500;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.lf-input {
  background: var(--bg-raised);
  border: 1px solid var(--border-default);
  border-radius: var(--r-md);
  color: var(--text-primary);
  font-family: var(--font-mono);
  font-size: 0.875rem;
  padding: 0.55rem 0.75rem;
  outline: none;
  transition: border-color 0.15s;
}
.lf-input:focus { border-color: var(--accent); }
.lf-btn {
  margin-top: 0.25rem;
  padding: 0.6rem;
  background: var(--accent);
  color: var(--bg-base);
  border: none;
  border-radius: var(--r-md);
  font-family: var(--font-mono);
  font-size: 0.85rem;
  font-weight: 600;
  cursor: pointer;
  letter-spacing: 0.04em;
  transition: background 0.15s;
}
.lf-btn:hover { background: var(--accent-hover); }
.lf-hint {
  font-size: 0.65rem;
  color: var(--text-tertiary);
  text-align: center;
  font-family: var(--font-mono);
}
</style>
