<script lang="ts">
	import { fade } from 'svelte/transition';
	import { flip } from 'svelte/animate';
	import { toasts, dismiss } from '$lib/toast.svelte';
	import { pop, spring } from '$lib/motion';
</script>

<div class="stack" aria-live="polite" aria-atomic="false">
	{#each toasts as t (t.id)}
		<div
			class="toast glass"
			class:error={t.kind === 'error'}
			role={t.kind === 'error' ? 'alert' : 'status'}
			in:pop={{ y: 16, scale: 0.9 }}
			out:fade={{ duration: 160 }}
			animate:flip={{ duration: 400, easing: spring }}
		>
			<span>{t.text}</span>
			<button type="button" aria-label="Sluiten" onclick={() => dismiss(t.id)}>
				<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" /></svg>
			</button>
		</div>
	{/each}
</div>

<style>
	.stack {
		position: fixed;
		left: 50%;
		bottom: max(20px, env(safe-area-inset-bottom));
		transform: translateX(-50%);
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		z-index: 50;
		width: min(440px, calc(100vw - 32px));
		pointer-events: none;
	}
	.toast {
		pointer-events: auto;
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 10px 10px 18px;
		border-radius: 999px;
		font-size: 0.9375rem;
		font-weight: 500;
		background: var(--glass-strong);
		box-shadow: var(--popover-shadow);
	}
	.toast.error span::before {
		content: '';
		display: inline-block;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--danger);
		margin-right: 10px;
		vertical-align: 1px;
	}
	button {
		flex: none;
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		border: 0;
		border-radius: 50%;
		background: transparent;
		color: var(--text-2);
		cursor: pointer;
	}
	button:hover {
		background: var(--hover);
	}
	svg {
		width: 12px;
		height: 12px;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
	}
</style>
