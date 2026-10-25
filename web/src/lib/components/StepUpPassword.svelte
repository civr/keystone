<script lang="ts">
	import '$lib/settings.css';
	import { tick } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { prompt, settlePrompt } from '$lib/passwordPrompt.svelte';
	import Sheet from './Sheet.svelte';

	let password = $state('');
	let error = $state('');
	let busy = $state(false);
	let input = $state<HTMLInputElement | null>(null);

	// A new request starts clean, with the cursor in the field.
	$effect(() => {
		prompt.generation;
		password = '';
		error = '';
		tick().then(() => input?.focus());
	});

	async function confirm(e: SubmitEvent) {
		e.preventDefault();
		if (busy || !password) return;
		busy = true;
		error = '';
		try {
			await api.send('POST', '/auth/stepup/password', { password });
			password = '';
			settlePrompt(true);
		} catch (err) {
			password = '';
			error =
				err instanceof ApiError && err.status === 401
					? 'That password is not correct.'
					: err instanceof ApiError
						? err.message
						: 'Something went wrong. Try again.';
			input?.focus();
		} finally {
			busy = false;
		}
	}
</script>

<Sheet bind:open={prompt.open} title="Confirm it’s you" onclose={() => settlePrompt(false)}>
	<form class="form" onsubmit={confirm} novalidate>
		<p class="sheet-msg">Enter your password to continue.</p>
		<input
			class="input"
			type="password"
			autocomplete="current-password"
			aria-label="Password"
			placeholder="Password"
			aria-invalid={error ? 'true' : undefined}
			bind:this={input}
			bind:value={password}
		/>
		{#if error}<p class="error" role="alert">{error}</p>{/if}
		<div class="sheet-actions">
			<button type="button" class="btn btn-secondary" onclick={() => settlePrompt(false)}>Cancel</button>
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
