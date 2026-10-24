<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { initials } from '$lib/nav';
	import { toast } from '$lib/toast.svelte';

	let me = $state<Me | null>(null);
	let current = $state('');
	let next = $state('');
	let repeat = $state('');
	let errors = $state<{ current?: string; new?: string; repeat?: string }>({});
	let saving = $state(false);
	let adPassword = $state(false);

	onMount(() => {
		api.get<Me>('/me').then((m) => (me = m), () => {});
	});

	const ROLE = { owner: 'Owner', admin: 'Admin', member: 'Member' } as const;

	async function change(e: SubmitEvent) {
		e.preventDefault();
		if (saving) return;
		errors = {};
		if (next.length < 12) return void (errors.new = 'Choose a password of at least 12 characters.');
		if (next !== repeat) return void (errors.repeat = 'The passwords do not match.');
		saving = true;
		try {
			await api.send('PUT', '/account/password', { current, new: next });
			current = next = repeat = '';
			toast('Password changed. Other devices are signed out.');
		} catch (err) {
			if (err instanceof ApiError && err.code === 'ad_password') adPassword = true;
			else if (err instanceof ApiError && err.code === 'invalid' && typeof err.data.field === 'string')
				errors = { [err.data.field]: err.message };
			else toast(err instanceof ApiError ? err.message : 'The password could not be changed.', 'error');
		} finally {
			saving = false;
		}
	}
</script>

<div class="pane-head"><h1 class="pane-title">Account</h1></div>

<section class="group" aria-label="You">
	<div class="card">
		<div class="srow">
			<span class="avatar" aria-hidden="true">{initials(me?.display_name || me?.username || '·')}</span>
			<div class="text">
				<span class="label">{me?.display_name || me?.username || '…'}</span>
				<p class="help">{#if me}{me.username}{#if me.email} · {me.email}{/if}{:else}&nbsp;{/if}</p>
			</div>
			{#if me}<span class="chip" class:accent={me.role !== 'member'}>{ROLE[me.role]}</span>{/if}
		</div>
	</div>
</section>

<section class="group" aria-labelledby="pw">
	<h2 id="pw">Password</h2>
	{#if adPassword}
		<div class="card form-card"><p class="help">You sign in with your Active Directory password. Change it in Active Directory.</p></div>
	{:else}
		<form class="card form-card pw" onsubmit={change} novalidate>
			<label class="field">
				<span>Current password</span>
				<input class="input" type="password" autocomplete="current-password" bind:value={current} aria-invalid={errors.current ? 'true' : undefined} />
				{#if errors.current}<small class="field-error" role="alert">{errors.current}</small>{/if}
			</label>
			<label class="field">
				<span>New password</span>
				<input class="input" type="password" autocomplete="new-password" bind:value={next} aria-invalid={errors.new ? 'true' : undefined} />
				{#if errors.new}<small class="field-error" role="alert">{errors.new}</small>{:else}<small>At least 12 characters.</small>{/if}
			</label>
			<label class="field">
				<span>Repeat new password</span>
				<input class="input" type="password" autocomplete="new-password" bind:value={repeat} aria-invalid={errors.repeat ? 'true' : undefined} />
				{#if errors.repeat}<small class="field-error" role="alert">{errors.repeat}</small>{/if}
			</label>
			<div class="actions">
				<button class="btn btn-primary" disabled={saving || !current || !next || !repeat}>
					{#if saving}<span class="spinner" aria-hidden="true"></span>{/if}
					Change password
				</button>
			</div>
		</form>
	{/if}
	<p class="foot">Passkeys and signed-in devices are under Devices.</p>
</section>

<style>
	.pw {
		display: grid;
		gap: 14px;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
	}
</style>
