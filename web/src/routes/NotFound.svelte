<script lang="ts">
  // Current hash from URL - strip the '#/' prefix for display
  let page = window.location.hash.replace('#/', '') || 'dashboard';
</script>

<div class="not-found">
  <div class="icon">◈</div>
  <h2>{page}</h2>
  <p>This module hasn't been built yet — it's coming in the next sprint.</p>
  <a href="#/" class="btn">← Back to Dashboard</a>
</div>

<style>
.not-found {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 40vh;
  gap: 0.75rem;
  text-align: center;
  color: var(--text-secondary);
}
.icon {
  font-size: 2.5rem;
  color: var(--text-tertiary);
  margin-bottom: 0.5rem;
}
h2 {
  font-family: var(--font-mono);
  font-size: 1.1rem;
  color: var(--text-primary);
  font-weight: 500;
  text-transform: lowercase;
}
p {
  font-size: 0.85rem;
  max-width: 320px;
}
</style>
