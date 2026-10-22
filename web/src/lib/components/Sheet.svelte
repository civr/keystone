<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		open = $bindable(false),
		title,
		children
	}: { open?: boolean; title: string; children: Snippet } = $props();

	let dialog: HTMLDialogElement;
	const uid = $props.id();
	const titleId = `dlg-${uid}`;

	// Native <dialog>: focus trap, Esc, inert background and focus restore for free.
	$effect(() => {
		if (open && !dialog.open) dialog.showModal();
		else if (!open && dialog.open) dialog.close();
	});

	function onBackdrop(e: MouseEvent) {
		if (e.target === dialog) open = false;
	}
</script>

<!-- Nimbus modal: 16px corner on the dialog depth; backdrop fades in 200 ms,
     the panel rises 12px from 98% scale over 250 ms on the decelerate curve. -->
<dialog bind:this={dialog} aria-labelledby={titleId} onclose={() => (open = false)} onclick={onBackdrop}>
	<div class="panel">
		<header>
			<h2 id={titleId}>{title}</h2>
			<button type="button" class="close" aria-label="Sluiten" onclick={() => (open = false)}>
				<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" /></svg>
			</button>
		</header>
		<div class="body">{@render children()}</div>
	</div>
</dialog>

<style>
	dialog {
		padding: 0;
		border: 0;
		background: transparent;
		color: inherit;
		max-width: none;
		max-height: none;
		width: min(520px, 100vw - 32px);
		overflow: visible;
	}
	dialog::backdrop {
		background: var(--scrim);
	}
	.panel {
		display: flex;
		flex-direction: column;
		max-height: min(86dvh, 760px);
		border: 1px solid var(--c-border2);
		border-radius: var(--radius-lg);
		background: var(--c-bg-card);
		box-shadow: var(--shadow-lg);
	}
	dialog[open]::backdrop {
		animation: overlay-in var(--dur-3) var(--ease-out) both;
	}
	dialog[open] .panel {
		animation: modal-in var(--dur-4) var(--ease-out) both;
	}
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 18px 14px 6px 24px;
	}
	h2 {
		margin: 0;
		font-size: 1.125rem;
		font-weight: 700;
		line-height: 1.3;
		letter-spacing: -0.01em;
	}
	.body {
		padding: 8px 24px 24px;
		overflow: auto;
	}
	.close {
		flex: none;
		display: grid;
		place-items: center;
		width: 32px;
		height: 32px;
		border: 0;
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--color-text-faint);
		cursor: pointer;
		transition: background-color var(--dur-2) var(--ease);
	}
	.close:hover {
		background: var(--c-bg-section);
		color: var(--color-text);
	}
	.close svg {
		width: 12px;
		height: 12px;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
	}
	@media (max-width: 599px) {
		header {
			padding: 16px 10px 4px 18px;
		}
		.body {
			padding: 8px 18px 20px;
		}
	}
	@keyframes overlay-in {
		from {
			opacity: 0;
		}
	}
	@keyframes modal-in {
		from {
			opacity: 0;
			transform: translateY(12px) scale(0.98);
		}
	}
</style>
