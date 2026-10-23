<script lang="ts">
	import { onMount } from 'svelte';
	import { flip } from 'svelte/animate';
	import { fade } from 'svelte/transition';
	import AppTile from './AppTile.svelte';
	import type { App } from '$lib/apps';
	import { easeOut, reducedMotion, rise } from '$lib/motion';

	let { apps, highlightFirst = false }: { apps: App[]; highlightFirst?: boolean } = $props();

	// The first load settles a few tiles at a time (capped at 12); filtering is instant.
	const mounted = { done: false };
	onMount(() => {
		mounted.done = true;
	});
	const still = reducedMotion();
</script>

<ul class="grid" aria-label="Apps">
	{#each apps as app, i (app.id)}
		<li
			in:rise|global={{ delay: mounted.done ? 0 : Math.min(i, 12) * 20, duration: mounted.done ? 150 : 220 }}
			out:fade={{ duration: still ? 0 : 100 }}
			animate:flip={{ duration: still ? 0 : 200, easing: easeOut }}
		>
			<AppTile {app} highlighted={highlightFirst && i === 0} />
		</li>
	{/each}
</ul>

<style>
	/* A Home Screen grid: icons in even columns, generous rows. */
	.grid {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(108px, 1fr));
		gap: 22px 12px;
	}
	@media (max-width: 520px) {
		.grid {
			grid-template-columns: repeat(4, minmax(0, 1fr));
			gap: 18px 4px;
		}
	}
</style>
