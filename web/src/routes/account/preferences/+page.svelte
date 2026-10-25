<script lang="ts">
	import { onMount } from 'svelte';
	import { THEMES, applyTheme, readTheme, type Theme } from '$lib/appearance';

	let theme = $state<Theme>('system');
	onMount(() => (theme = readTheme()));

	function choose(t: Theme) {
		theme = t;
		applyTheme(t);
	}
</script>

<div class="pane-head"><h1 class="pane-title">Preferences</h1></div>
<p class="pane-intro">How Keystone looks on this device.</p>

<section class="group" aria-labelledby="g-look">
	<h2 id="g-look">Appearance</h2>
	<div class="card">
		<div class="srow stack">
			<div class="looks" role="radiogroup" aria-labelledby="g-look">
				{#each THEMES as t (t.id)}
					<label class="look" class:on={theme === t.id}>
						<input type="radio" name="theme" value={t.id} checked={theme === t.id} onchange={() => choose(t.id)} />
						<span class="swatch {t.id}" aria-hidden="true"><span></span></span>
						<span>{t.label}</span>
					</label>
				{/each}
			</div>
			<p class="help">Automatic follows your system’s light or dark setting.</p>
		</div>
	</div>
</section>

<style>
	.looks {
		display: flex;
		gap: 18px;
		flex-wrap: wrap;
	}
	.look {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		cursor: pointer;
		font-size: 0.9375rem;
	}
	.look input {
		position: absolute;
		opacity: 0;
	}
	.swatch {
		display: grid;
		place-items: end;
		width: 96px;
		height: 64px;
		padding: 8px;
		border-radius: 10px;
		border: 1px solid var(--c-border);
		background: #f5f5f7;
	}
	.swatch span {
		width: 60%;
		height: 18px;
		border-radius: 4px;
		background: #ffffff;
		box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.08);
	}
	.swatch.dark {
		background: #1c1c1e;
	}
	.swatch.dark span {
		background: #3a3a3c;
	}
	.swatch.system {
		background: linear-gradient(135deg, #f5f5f7 50%, #1c1c1e 50%);
	}
	.look.on .swatch {
		box-shadow:
			0 0 0 2px var(--c-bg-card),
			0 0 0 4px var(--primary);
	}
	.look:has(input:focus-visible) .swatch {
		outline: 2px solid var(--focus-ring);
		outline-offset: 5px;
	}
</style>
