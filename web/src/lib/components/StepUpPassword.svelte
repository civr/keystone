<script lang="ts">
	import { tick } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { prompt, settlePrompt } from '$lib/passwordPrompt.svelte';
	import '$lib/settings.css';
	import Sheet from './Sheet.svelte';

	let password = $state('');
	let error = $state('');
	let busy = $state(false);
	let input = $state<HTMLInputElement | null>(null);
	let open = $state(false);

	// Mirror the store so closing the sheet (Esc, backdrop) cancels.
	$effect(() => {
		if (prompt.open && !open) {
			open = true;
			password = '';
			error = '';
			tick().then(() => input?.focus());
		}
	});
	$effect(() => {
		if (!open && prompt.open) settlePrompt(false);
	});

	async function confirm(e: SubmitEvent) {
		e.preventDefault();
		if (busy || !password) return;
		busy = true;
		error = '';
		try {
			await api.send('POST', '/auth/stepup/password', { password });
			password = '';
			open = false;
			settlePrompt(true);
		} catch (err) {
			error = err instanceof ApiError && err.status === 401 ? 'That password is not correct.' : err instanceof ApiError ? err.message : 'Something went wrong.';
		} finally {
			busy = false;
		}
	}
</script>

<Sheet bind:open title="Confirm it’s you">
	<form class="form" onsubmit={confirm} novalidate>
		<p class="sheet-msg">Enter your password to continue.</p>
		<input class="input" type="password" autocomplete="current-password" aria-label="Password" placeholder="Password" bind:this={input} bind:value={password} />
		{#if error}<p class="error" role="alert">{error}</p>{/if}
		<div class="sheet-actions">
			<button type="button" class="btn btn-secondary" onclick={() => (open = false)}>Cancel</button>
			<button class="btn btn-primary" disabled={busy || !password}>
				{#if busy}<span class="spinner" aria-hidden="true"></span>{/if}
				Confirm
			</button>
		</div>
	</form>
</Sheet>

<style>
	.form {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.sheet-actions {
		margin-top: 8px;
	}
</style>
