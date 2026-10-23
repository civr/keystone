<script lang="ts">
	import { fade, fly } from 'svelte/transition';
	import { flip } from 'svelte/animate';
	import { toasts, dismiss } from '$lib/toast.svelte';
	import { easeOut, reducedMotion } from '$lib/motion';

	const still = reducedMotion();
</script>

<div class="stack" aria-live="polite" aria-atomic="false">
	{#each toasts as t (t.id)}
		<div
			class="toast"
			class:error={t.kind === 'error'}
			role={t.kind === 'error' ? 'alert' : 'status'}
			in:fly={{ y: still ? 0 : -16, duration: still ? 0 : 320, easing: easeOut }}
			out:fade={{ duration: still ? 0 : 150 }}
			animate:flip={{ duration: still ? 0 : 200, easing: easeOut }}
		>
			<span class="dot" aria-hidden="true"></span>
			<span class="text">{t.text}</span>
			<button type="button" aria-label="Sluiten" onclick={() => dismiss(t.id)}>
				<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" /></svg>
			</button>
		</div>
	{/each}
</div>

<style>
	/* A frosted capsule that drops in at the top centre, like iOS banners. */
	.stack {
		position: fixed;
		top: max(12px, env(safe-area-inset-top));
		left: 50%;
		transform: translateX(-50%);
		z-index: var(--z-toast);
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		width: min(420px, calc(100vw - 24px));
		pointer-events: none;
	}
	.toast {
		pointer-events: auto;
		display: flex;
		align-items: center;
		gap: 10px;
		max-width: 100%;
		min-height: 44px;
		padding: 8px 8px 8px 16px;
		border-radius: var(--radius-pill);
		background: rgba(255, 255, 255, 0.86);
		-webkit-backdrop-filter: var(--material-blur);
		backdrop-filter: var(--material-blur);
		box-shadow:
			0 0 0 0.5px rgba(0, 0, 0, 0.08),
			0 8px 28px rgba(0, 0, 0, 0.14);
		font-size: 0.875rem;
		font-weight: 500;
		line-height: 1.35;
	}
	@media (prefers-reduced-transparency: reduce) {
		.toast {
			background: #fff;
		}
	}
	.dot {
		flex: none;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--color-success);
	}
	.error .dot {
		background: var(--color-danger);
	}
	.text {
		flex: 1;
		min-width: 0;
	}
	button {
		flex: none;
		display: grid;
		place-items: center;
		width: 26px;
		height: 26px;
		border: 0;
		border-radius: 50%;
		background: rgba(118, 118, 128, 0.12);
		color: #8e8e93;
		cursor: pointer;
		transition: background-color var(--dur-1) var(--ease);
	}
	button:hover {
		background: rgba(118, 118, 128, 0.2);
	}
	svg {
		width: 9px;
		height: 9px;
		stroke: currentColor;
		stroke-width: 2.4;
		stroke-linecap: round;
	}
</style>
