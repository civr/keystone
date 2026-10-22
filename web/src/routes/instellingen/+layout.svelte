<script lang="ts">
	// Everything under /instellingen is its own lazily loaded chunk: none of
	// this (or forms.css/settings.css) ships with the home page.
	import '$lib/forms.css';
	import '$lib/settings.css';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { fade } from 'svelte/transition';
	import { api, type Me } from '$lib/api';
	import { NAV, isActive } from '$lib/nav';
	import { reducedMotion } from '$lib/motion';
	import AppShell from '$lib/components/AppShell.svelte';

	let { children } = $props();

	let me = $state<Me | null>(null);
	onMount(() => {
		api.get<Me>('/me').then((m) => (me = m), () => {});
	});

	const sections = NAV[1].items;
	const current = $derived(sections.find((s) => isActive(page.url.pathname, s.href)) ?? sections[0]);
	const still = reducedMotion();
</script>

<svelte:head>
	<title>{current.label} · Instellingen · Keystone</title>
</svelte:head>

<AppShell {me}>
	{#key current.href}
		<div class="pane" in:fade={{ duration: still ? 0 : 150 }}>{@render children()}</div>
	{/key}
</AppShell>

<style>
	.pane {
		max-width: 880px;
	}
</style>
