<script lang="ts">
	// The admin portal is its own lazily loaded chunk: none of this (or
	// forms.css/settings.css) ships with the user portal.
	import '$lib/forms.css';
	import '$lib/settings.css';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { fade } from 'svelte/transition';
	import { api, type Me } from '$lib/api';
	import { ADMIN_NAV, isActive } from '$lib/nav';
	import { reducedMotion } from '$lib/motion';
	import { pageTitle } from '$lib/brand.svelte';
	import AppShell from '$lib/components/AppShell.svelte';

	let { children } = $props();

	let me = $state<Me | null>(null);
	onMount(() => {
		api.get<Me>('/me').then(
			(m) => {
				me = m;
				// The menu is a convenience; the server refuses every admin call anyway.
				if (!m.admin) goto('/', { replaceState: true });
			},
			() => {}
		);
	});

	const items = ADMIN_NAV.flatMap((g) => g.items);
	const current = $derived(items.find((s) => isActive(page.url.pathname, s.href)) ?? items[0]);
	const still = reducedMotion();
</script>

<svelte:head>
	<title>{pageTitle(current.label, 'Admin')}</title>
</svelte:head>

<AppShell {me} portal="admin">
	{#key current.href}
		<div class="pane" in:fade={{ duration: still ? 0 : 150 }}>{@render children()}</div>
	{/key}
</AppShell>

<style>
	.pane {
		max-width: 1080px;
		margin: 0 auto;
	}
</style>
