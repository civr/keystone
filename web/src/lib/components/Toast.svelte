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
			in:fly={{ x: still ? 0 : 14, duration: still ? 0 : 220, easing: easeOut }}
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
	.stack {
		position: fixed;
		right: max(16px, env(safe-area-inset-right));
		bottom: max(16px, env(safe-area-inset-bottom));
		z-index: var(--z-toast);
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		gap: 8px;
		width: min(380px, calc(100vw - 32px));
		pointer-events: none;
	}
	.toast {
		pointer-events: auto;
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 10px 8px 10px 14px;
		border: 1px solid var(--c-border2);
		border-radius: var(--radius-md);
		background: var(--c-bg-card);
		box-shadow: var(--shadow-md);
		font-size: 0.8125rem;
		font-weight: 500;
		line-height: 1.45;
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
		width: 28px;
		height: 28px;
		border: 0;
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--color-text-faint);
		cursor: pointer;
		transition: background-color var(--dur-2) var(--ease);
	}
	button:hover {
		background: var(--c-bg-section);
		color: var(--color-text);
	}
	svg {
		width: 12px;
		height: 12px;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
	}
	@media (max-width: 599px) {
		.stack {
			left: 50%;
			right: auto;
			transform: translateX(-50%);
			align-items: stretch;
		}
	}
</style>
