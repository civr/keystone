<script lang="ts">
	import { tick } from 'svelte';
	import { fade } from 'svelte/transition';
	import { pop } from '$lib/motion';

	let {
		name,
		username,
		onlogout
	}: { name: string; username: string; onlogout: () => void } = $props();

	let open = $state(false);
	let root: HTMLDivElement;
	let button: HTMLButtonElement;
	let menu = $state<HTMLDivElement>();

	const initials = $derived(
		name
			.split(/\s+/)
			.filter(Boolean)
			.slice(0, 2)
			.map((w) => Array.from(w)[0])
			.join('')
			.toUpperCase() || '?'
	);

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
		class="avatar"
		aria-haspopup="menu"
		aria-expanded={open}
		aria-label="Account van {name}"
		onclick={toggle}
	>
		<span aria-hidden="true">{initials}</span>
	</button>

	{#if open}
		<div
			bind:this={menu}
			class="menu glass"
			role="menu"
			tabindex="-1"
			aria-label="Account"
			onkeydown={onMenuKey}
			in:pop={{ scale: 0.92, y: -4 }}
			out:fade={{ duration: 120 }}
		>
			<div class="who">
				<strong>{name}</strong>
				<span>{username}</span>
			</div>
			<div class="sep" role="separator"></div>
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
	}
	.avatar {
		display: grid;
		place-items: center;
		width: 40px;
		height: 40px;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: linear-gradient(135deg, #7aa2ff, #b07aff);
		color: #fff;
		font-size: 0.875rem;
		font-weight: 600;
		letter-spacing: 0.01em;
		box-shadow:
			0 0 0 2px rgba(255, 255, 255, 0.7),
			0 4px 12px rgba(80, 90, 200, 0.25);
		cursor: pointer;
		transition: transform var(--spring-dur) var(--spring-bounce);
	}
	.avatar:active {
		transform: scale(0.92);
		transition: transform 90ms ease-out;
	}
	.menu {
		position: absolute;
		top: calc(100% + 10px);
		right: 0;
		z-index: 20;
		min-width: 232px;
		padding: 6px;
		border-radius: var(--radius-m);
		background: var(--menu-bg);
		box-shadow: var(--popover-shadow);
		transform-origin: top right;
	}
	.who {
		display: flex;
		flex-direction: column;
		padding: 8px 10px 10px;
		line-height: 1.3;
	}
	.who strong {
		font-size: 0.9375rem;
		font-weight: 600;
		letter-spacing: -0.01em;
	}
	.who span {
		font-size: 0.8125rem;
		color: var(--text-2);
	}
	.sep {
		height: 1px;
		margin: 0 6px 6px;
		background: var(--hairline);
	}
	[role='menuitem'] {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		padding: 9px 10px;
		border: 0;
		border-radius: var(--radius-s);
		background: transparent;
		color: var(--text);
		font-size: 0.9375rem;
		text-align: left;
		text-decoration: none;
		cursor: pointer;
		outline: none;
	}
	[role='menuitem']:hover,
	[role='menuitem']:focus-visible {
		background: var(--hover);
	}
	[role='menuitem']:active {
		background: var(--press);
	}
	svg {
		width: 18px;
		height: 18px;
		fill: none;
		stroke: var(--text-2);
		stroke-width: 1.6;
		stroke-linecap: round;
		stroke-linejoin: round;
	}
</style>
