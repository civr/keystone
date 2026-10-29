<script lang="ts">
	import { onMount, tick } from 'svelte';
	import qrcode from 'qrcode-generator';
	import { api, ApiError } from '$lib/api';
	import { toast } from '$lib/toast.svelte';

	let enabled = $state<boolean | null>(null);
	let setup = $state<{ secret: string; qr: string } | null>(null);
	let code = $state('');
	let error = $state('');
	let busy = $state(false);
	let input = $state<HTMLInputElement | null>(null);

	onMount(async () => {
		try {
			enabled = (await api.get<{ enabled: boolean }>('/account/totp')).enabled;
		} catch {
			enabled = false;
		}
	});

	/** The otpauth link as a QR image (a data: URL, shown through <img>). */
	function qrFor(uri: string): string {
		const q = qrcode(0, 'M');
		q.addData(uri);
		q.make();
		return q.createDataURL(5, 8);
	}

	async function start() {
		busy = true;
		error = '';
		try {
			const r = await api.send<{ secret: string; uri: string }>('POST', '/account/totp');
			setup = { secret: r.secret, qr: qrFor(r.uri) };
			code = '';
			await tick();
			input?.focus();
		} catch (e) {
			if (!(e instanceof Error && e.message === 'cancelled')) toast(e instanceof ApiError ? e.message : 'Setting up failed.', 'error');
		} finally {
			busy = false;
		}
	}

	async function confirm(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			await api.send('POST', '/account/totp/confirm', { code });
			enabled = true;
			setup = null;
			toast('Authenticator added. Sign-in now asks for its code.');
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'That code did not work.';
			code = '';
		} finally {
			busy = false;
		}
	}

	async function remove() {
		busy = true;
		try {
			await api.send('DELETE', '/account/totp');
			enabled = false;
			toast('Authenticator removed');
		} catch (e) {
			if (!(e instanceof Error && e.message === 'cancelled')) toast(e instanceof ApiError ? e.message : 'Removing failed.', 'error');
		} finally {
			busy = false;
		}
	}

	const grouped = (s: string) => s.match(/.{1,4}/g)?.join(' ') ?? s;
</script>

<section class="group" aria-labelledby="g-totp">
	<h2 id="g-totp">Authenticator app</h2>
	<div class="card">
		{#if setup}
			<div class="srow stack">
				<div class="setup">
					<img src={setup.qr} alt="QR code to add Keystone to your authenticator app" width="180" height="180" />
					<div class="steps">
						<p>Scan this with an authenticator app (Apple Passwords, 1Password, Google Authenticator…), then enter the code it shows.</p>
						<p class="help">Cannot scan? Enter this key: <span class="mono key">{grouped(setup.secret)}</span></p>
						<form class="row" onsubmit={confirm} novalidate>
							<input
								class="input otp"
								aria-label="Code"
								inputmode="numeric"
								autocomplete="one-time-code"
								maxlength="6"
								placeholder="000000"
								bind:this={input}
								bind:value={code}
							/>
							<button class="btn btn-primary" disabled={busy || code.length !== 6}>Confirm</button>
							<button type="button" class="btn btn-secondary" onclick={() => (setup = null)}>Cancel</button>
						</form>
						{#if error}<p class="field-error" role="alert">{error}</p>{/if}
					</div>
				</div>
			</div>
		{:else}
			<div class="srow">
				<div class="text">
					<span class="label">{enabled ? 'On' : 'Off'}</span>
					<p class="help">
						{enabled
							? 'Signing in with your password also asks for the six-digit code.'
							: 'A six-digit code from your phone as a second step after your password.'}
					</p>
				</div>
				<div class="control">
					{#if enabled}
						<button type="button" class="btn btn-secondary btn-sm danger-ink" onclick={remove} disabled={busy}>Remove</button>
					{:else if enabled === false}
						<button type="button" class="btn btn-secondary btn-sm" onclick={start} disabled={busy}>Set up</button>
					{/if}
				</div>
			</div>
		{/if}
	</div>
</section>

<style>
	.setup {
		display: flex;
		gap: 20px;
		align-items: flex-start;
		flex-wrap: wrap;
	}
	.setup img {
		border-radius: 8px;
		background: #fff;
		padding: 6px;
		image-rendering: pixelated;
	}
	.steps {
		flex: 1;
		min-width: 240px;
	}
	.steps p {
		margin: 0 0 10px;
	}
	.key {
		user-select: all;
		color: var(--color-text);
	}
	.row {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
	}
	.otp {
		width: 140px;
		letter-spacing: 0.2em;
		text-align: center;
	}
</style>
