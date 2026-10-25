<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import type { App } from '$lib/apps';

	interface Access {
		groups: { id: string; name: string }[];
		roles: { id: string; name: string }[];
		permissions: string[];
		app_admin_of: string[];
	}

	let access = $state<Access | null>(null);
	let apps = $state<App[]>([]);
	let loadError = $state('');

	onMount(async () => {
		try {
			[access, apps] = await Promise.all([api.get<Access>('/account/access'), api.get<{ apps: App[] }>('/apps?mine=1').then((r) => r.apps)]);
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Your access could not be loaded.';
		}
	});
</script>

<div class="pane-head"><h1 class="pane-title">My access</h1></div>
<p class="pane-intro">The groups you are in, the roles you hold and the apps you can open. An admin changes these.</p>

{#if loadError}
	<p class="error" role="alert">{loadError}</p>
{:else if !access}
	<p class="loading-note" role="status">Loading…</p>
{:else}
	<section class="group" aria-labelledby="g-groups">
		<h2 id="g-groups">Groups</h2>
		{#if access.groups.length}
			<ul class="card list">{#each access.groups as g (g.id)}<li class="srow"><span class="label">{g.name}</span></li>{/each}</ul>
		{:else}
			<p class="empty-note">You are not in a group.</p>
		{/if}
	</section>
	<section class="group" aria-labelledby="g-roles">
		<h2 id="g-roles">Roles</h2>
		{#if access.roles.length}
			<ul class="card list">{#each access.roles as r (r.id)}<li class="srow"><span class="label">{r.name}</span></li>{/each}</ul>
		{:else}
			<p class="empty-note">No admin roles: you use the apps you are given.</p>
		{/if}
	</section>
	<section class="group" aria-labelledby="g-apps">
		<h2 id="g-apps">Apps</h2>
		{#if apps.length}
			<ul class="card list">
				{#each apps as a (a.id)}
					<li class="srow">
						<span class="label">{a.name}</span>
						{#if access.app_admin_of.includes(a.id)}<span class="chip accent">You manage it</span>{/if}
					</li>
				{/each}
			</ul>
		{:else}
			<p class="empty-note">No apps yet.</p>
		{/if}
	</section>
{/if}

<style>
	.list {
		margin: 0;
	}
</style>
