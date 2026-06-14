<!-- Health Checks settings section — add to Settings.svelte -->
<!-- Add this import at top of script: -->
<!-- let healthChecks: any[] = $state([]); -->
<!-- let healthEditorOpen = $state(false); -->
<!-- let healthSaving = $state(false); -->

<!--
  Add to load() function:
  const hcRes = await fetch('/api/health/checks').then(r => r.json()).catch(() => null);
  if (hcRes?.checks) healthChecks = hcRes.checks;

  Add save function:
  async function saveHealthChecks() {
    healthSaving = true;
    try {
      await api.put('/api/health/checks', { checks: healthChecks });
      saved = true; setTimeout(() => saved = false, 2500);
    } catch(e: any) { error = e.message; }
    finally { healthSaving = false; }
  }

  async function resetHealthChecks() {
    if (!confirm('Reset health checks to defaults?')) return;
    const res = await fetch('/api/health/checks', { method: 'DELETE' }).then(r => r.json());
    healthChecks = res.checks ?? [];
  }
-->
