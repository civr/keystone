<script lang="ts">
	import { tick } from 'svelte';
	import { fade } from 'svelte/transition';
	import { rise } from '$lib/motion';
	import { initials } from '$lib/nav';

	let {
		name,
		username,
		onlogout
	}: { name: string; username: string; onlogout: () => void } = $props();

	let open = $state(false);
	let root: HTMLDivElement;
	let button: HTMLButtonElement;
	let menu = $state<HTMLDivElement>();

	function items(): HTMLElement[] {
		return Array.from(menu?.querySelectorAll<HTMLElement>('[role="menuitem"]') ?? []);
	}

	async function toggle() {
		open = !open;
		if (open) {
			await tick();
			items()[0]?.focus();
		}
	}

	function close(refocus = true) {
		if (!open) return;
		open = false;
		if (refocus) button.focus();
	}

	function onMenuKey(e: KeyboardEvent) {
		const list = items();
		const i = list.indexOf(document.activeElement as HTMLElement);
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			e.preventDefault();
			const d = e.key === 'ArrowDown' ? 1 : -1;
			list[(i + d + list.length) % list.length]?.focus();
		} else if (e.key === 'Home' || e.key === 'End') {
			e.preventDefault();
			list[e.key === 'Home' ? 0 : list.length - 1]?.focus();
		} else if (e.key === 'Escape') {
			e.preventDefault();
			e.stopPropagation();
			close();
		} else if (e.key === 'Tab') {
			close(false);
		}
	}

	function onWindowPointer(e: PointerEvent) {
		if (open && !root.contains(e.target as Node)) close(false);
	}
</script>

<svelte:window onpointerdown={onWindowPointer} />

<div class="wrap" bind:this={root}>
	<button
		bind:this={button}
		type="button"
		class="trigger"
		aria-haspopup="menu"
		aria-expanded={open}
		aria-label="Account van {name}"
		onclick={toggle}
	>
		<span class="avatar" aria-hidden="true">{initials(name)}</span>
		<span class="who" aria-hidden="true">
			<strong>{name}</strong>
			<span>{username}</span>
		</span>
		<svg class="caret" viewBox="0 0 20 20" aria-hidden="true"><path d="M6 8l4 4 4-4" /></svg>
	</button>

	{#if open}
		<div
			bind:this={menu}
			class="menu"
			role="menu"
			tabindex="-1"
			aria-label="Account"
			onkeydown={onMenuKey}
			in:rise={{ y: -4, scale: 0.98, duration: 200 }}
			out:fade={{ duration: 100 }}
		>
			<div class="head">
				<strong>{name}</strong>
				<span>{username}</span>
			</div>
			<a role="menuitem" href="/instellingen" onclick={() => close(false)}>
				<svg viewBox="0 0 20 20" aria-hidden="true">
					<circle cx="10" cy="10" r="2.6" />
					<path
						d="M10 2.5v2M10 15.5v2M2.5 10h2M15.5 10h2M4.7 4.7l1.4 1.4M13.9 13.9l1.4 1.4M4.7 15.3l1.4-1.4M13.9 6.1l1.4-1.4"
					/>
				</svg>
				Instellingen
			</a>
			<button
				role="menuitem"
				type="button"
				onclick={() => {
					close(false);
					onlogout();
				}}
			>
				<svg viewBox="0 0 20 20" aria-hidden="true">
					<path d="M8 3.5H5A1.5 1.5 0 0 0 3.5 5v10A1.5 1.5 0 0 0 5 16.5h3M13 13.5L16.5 10 13 6.5M16.5 10H8" />
				</svg>
				Uitloggen
			</button>
		</div>
	{/if}
</div>

<style>
	.wrap {
		position: relative;
		flex: none;
	}
	/* Nimbus .topbar-menu: avatar, bold name, the account underneath. */
	.trigger {
		display: flex;
		align-items: center;
		gap: 10px;
		height: 48px;
		padding: 0 8px 0 6px;
		border: 0;
		border-radius: var(--radius-md);
		background: transparent;
		color: var(--topbar-ink);
		text-align: left;
		cursor: pointer;
		transition: background-color var(--dur-2) var(--ease);
	}
	.trigger:hover,
	.trigger[aria-expanded='true'] {
		background: var(--topbar-hover);
	}
	.who {
		display: flex;
		flex-direction: column;
		min-width: 0;
		max-width: 180px;
		line-height: 1.3;
	}
	.who strong {
		font-size: 0.8125rem;
		font-weight: 700;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.who span {
		font-size: 0.75rem;
		color: var(--topbar-ink-2);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.caret {
		width: 16px;
		height: 16px;
		fill: none;
		stroke: var(--topbar-ink-2);
		stroke-width: 1.8;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
	@media (max-width: 767px) {
		.who,
		.caret {
			display: none;
		}
		.trigger {
			padding: 0 4px;
		}
	}

	.menu {
		position: absolute;
		top: calc(100% + 6px);
		right: 0;
		z-index: var(--z-dropdown);
		min-width: 232px;
		padding: 6px;
		border: 1px solid var(--c-border2);
		border-radius: var(--radius-lg);
		background: var(--c-bg-card);
		box-shadow: var(--shadow-md);
		transform-origin: top right;
	}
	.head {
		display: flex;
		flex-direction: column;
		padding: 8px 10px 10px;
		margin-bottom: 4px;
		border-bottom: 1px solid var(--c-border2);
		line-height: 1.35;
	}
	.head strong {
		font-size: 0.875rem;
		font-weight: 700;
	}
	.head span {
		font-size: 0.75rem;
		color: var(--color-text-faint);
	}
	[role='menuitem'] {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 8px 10px;
		border: 0;
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--color-text);
		font-size: 0.8125rem;
		font-weight: 500;
		text-align: left;
		text-decoration: none;
		cursor: pointer;
		transition: background-color var(--dur-1) var(--ease);
	}
	[role='menuitem']:hover,
	[role='menuitem']:focus-visible {
		background: var(--c-bg-section);
		color: var(--color-text);
	}
	[role='menuitem']:focus-visible {
		outline: 2px solid var(--primary);
		outline-offset: -2px;
	}
	[role='menuitem']:active {
		background: var(--c-bg-sunken);
	}
	[role='menuitem'] svg {
		width: 16px;
		height: 16px;
		fill: none;
		stroke: var(--color-text-muted);
		stroke-width: 1.6;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
</style>
