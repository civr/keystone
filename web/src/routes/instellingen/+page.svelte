<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Me } from '$lib/api';
	import SettingsEditor from '$lib/components/settings/SettingsEditor.svelte';

	let me = $state<Me | null>(null);
	onMount(() => {
		api.get<Me>('/me').then((m) => (me = m), () => {});
	});

	const groups = [
		{
			id: 'adres',
			title: 'Adres',
			paths: ['domain', 'public_url'],
			foot: 'Wijzig je het publieke adres naar een andere hostnaam, dan werken je huidige passkeys daar niet meer.'
		}
	];
</script>

<div class="pane-head"><h1 class="pane-title">Algemeen</h1></div>
<p class="pane-intro">Waar Keystone woont en wie het beheert.</p>

<section class="group" aria-labelledby="g-account">
	<h2 id="g-account">Account</h2>
	<div class="card">
		<div class="srow">
			<div class="avatar" aria-hidden="true">{(me?.display_name || me?.username || '·').trim().charAt(0).toUpperCase()}</div>
			<div class="text">
				<span class="label">{me?.display_name || me?.username || '…'}</span>
				<p class="help">
					{#if me}{me.username}{#if me.email} · {me.email}{/if}{:else}&nbsp;{/if}
				</p>
			</div>
			<span class="chip accent">Eigenaar</span>
		</div>
	</div>
	<p class="foot">Alleen dit AD-account kan inloggen bij Keystone.</p>
</section>

<SettingsEditor {groups} />
