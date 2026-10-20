<script lang="ts">
	import { onMount } from 'svelte';
	import { flip } from 'svelte/animate';
	import { fade } from 'svelte/transition';
	import AppTile from './AppTile.svelte';
	import type { App } from '$lib/apps';
	import { pop, reducedMotion, spring } from '$lib/motion';

	let { apps, highlightFirst = false }: { apps: App[]; highlightFirst?: boolean } = $props();

	// Stagger only the very first appearance; filtering should feel instant.
	const mounted = { done: false };
	onMount(() => {
		mounted.done = true;
	});
	const flipDuration = reducedMotion() ? 0 : 450;
</script>

<ul class="grid" aria-label="Apps">
	{#each apps as app, i (app.id)}
		<li
			in:pop|global={{ delay: mounted.done ? 0 : Math.min(i, 12) * 22, duration: mounted.done ? 420 : 600 }}
			out:fade={{ duration: 120 }}
			animate:flip={{ duration: flipDuration, easing: spring }}
		>
			<AppTile {app} highlighted={highlightFirst && i === 0} />
		</li>
	{/each}
</ul>

<style>
	.grid {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: 14px 6px;
	}
	@media (min-width: 600px) {
		.grid {
			grid-template-columns: repeat(4, minmax(0, 1fr));
			gap: 18px 10px;
		}
	}
	@media (min-width: 900px) {
		.grid {
			grid-template-columns: repeat(5, minmax(0, 1fr));
			gap: 22px 12px;
		}
	}
</style>
