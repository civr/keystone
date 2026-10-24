<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { toast } from '$lib/toast.svelte';
	import SettingsEditor from '$lib/components/settings/SettingsEditor.svelte';

	let me = $state<Me | null>(null);
	let publicURL = $state<string | null>(null);
	let adopting = $state(false);
	onMount(() => {
		api.get<Me>('/me').then((m) => (me = m), () => {});
		api.get<{ public_url: string }>('/admin/settings').then((s) => (publicURL = s.public_url), () => {});
	});

	// Opened on another address than the public one (say a domain behind a
	// reverse proxy, while setup ran on the IP): offer to make this one public.
	const here = typeof location === 'undefined' ? '' : location.origin;
	const otherAddress = $derived(publicURL !== null && publicURL.replace(/\/+$/, '') !== here);

	async function adopt() {
		if (adopting) return;
		adopting = true;
		try {
			const cfg = await api.get<Record<string, unknown>>('/admin/settings');
			await api.send('PUT', '/admin/settings?confirm=1', { ...cfg, public_url: here });
			location.reload();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'The address could not be changed.', 'error');
			adopting = false;
		}
	}

	const groups = [
		{
			id: 'adres',
			title: 'Address',
			paths: ['domain', 'public_url'],
			foot: 'Passkeys belong to the host name they were made on; on a new one you add them again.'
		}
	];
</script>

<div class="pane-head"><h1 class="pane-title">General</h1></div>
<p class="pane-intro">Where Keystone lives and who owns it.</p>

<section class="group" aria-labelledby="g-account">
	<h2 id="g-account">Owner</h2>
	<div class="card">
		<div class="srow">
			<div class="avatar" aria-hidden="true">{(me?.display_name || me?.username || '·').trim().charAt(0).toUpperCase()}</div>
			<div class="text">
				<span class="label">{me?.display_name || me?.username || '…'}</span>
				<p class="help">
					{#if me}{me.username}{#if me.email} · {me.email}{/if}{:else}&nbsp;{/if}
				</p>
			</div>
			<span class="chip accent">Owner</span>
		</div>
	</div>
	<p class="foot">The owner made Keystone and is always an admin. Add other people under Users.</p>
</section>

{#if otherAddress}
	<div class="notice adopt" role="status">
		<p>
			You are on <span class="mono">{here}</span>, but the public address is <span class="mono">{publicURL}</span>. Apps
			and sign-in links use the public address.
		</p>
		<button type="button" class="btn btn-primary btn-sm" onclick={adopt} disabled={adopting}>
			{#if adopting}<span class="spinner" aria-hidden="true"></span>{/if}
			Use this address
		</button>
	</div>
{/if}

<SettingsEditor {groups} />

<style>
	.adopt {
		display: flex;
		align-items: center;
		gap: 12px;
		margin: 0 0 26px;
	}
	.adopt p {
		flex: 1;
		margin: 0;
	}
	@media (max-width: 599px) {
		.adopt {
			flex-direction: column;
			align-items: flex-start;
		}
	}
</style>
