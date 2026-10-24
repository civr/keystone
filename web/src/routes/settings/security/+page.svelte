<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, type Me } from '$lib/api';
	import { securityGroups } from '$lib/settingsMeta';
	import { codesFile, download } from '$lib/codes';
	import { toast } from '$lib/toast.svelte';
	import SettingsEditor from '$lib/components/settings/SettingsEditor.svelte';
	import Sheet from '$lib/components/Sheet.svelte';
	import Confirm from '$lib/components/settings/Confirm.svelte';

	let confirmer: Confirm;

	// --- Recovery codes ---
	let codes = $state<string[] | null>(null);
	let codesOpen = $state(false);
	let regenBusy = $state(false);

	async function regenerate() {
		const ok = await confirmer.ask({
			title: 'Make new recovery codes?',
			message: 'You get 10 new codes. All old recovery codes stop working.',
			confirm: 'Make new codes'
		});
		if (!ok) return;
		regenBusy = true;
		try {
			const res = await api.send<{ recovery_codes: string[] }>('POST', '/admin/recovery/regenerate');
			codes = res.recovery_codes;
			codesOpen = true;
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Making new codes failed.', 'error');
		} finally {
			regenBusy = false;
		}
	}

	// --- Nood-account ---
	let emergency = $state<{ enabled: boolean; password_set: boolean } | null>(null);
	let emBusy = $state(false);
	let pwOpen = $state(false);
	let pw = $state('');
	let pw2 = $state('');
	let pwError = $state('');
	let enableAfterPassword = $state(false);

	// --- Sleutels ---
	type Key = { use: 'saml' | 'oidc' | 'ldap'; id: string; active: boolean; created_at: string; cert_pem: string };
	const KEY_INFO = {
		saml: { label: 'SAML signing', help: 'Signs SAML responses. Apps that do not fetch the metadata themselves need the new certificate.' },
		oidc: { label: 'OIDC tokens', help: 'Signs ID and access tokens. The old key stays valid for 24 hours.' },
		ldap: { label: 'LDAPS certificate', help: 'The TLS certificate of keystone-ldap. Apps that check the certificate need the new one.' }
	} as const;
	let keyList = $state<Key[] | null>(null);
	let keyBusy = $state('');

	async function loadKeys() {
		try {
			keyList = (await api.get<{ keys: Key[] }>('/admin/keys')).keys.filter((k) => k.active);
		} catch {
			toast('The keys could not be loaded.', 'error');
		}
	}

	async function rotate(k: Key) {
		const ok = await confirmer.ask({
			title: `Renew ${KEY_INFO[k.use].label}?`,
			message: KEY_INFO[k.use].help,
			confirm: 'Renew'
		});
		if (!ok) return;
		keyBusy = k.use;
		try {
			await api.send('POST', `/admin/keys/${k.use}/rotate`);
			toast('Key renewed');
			await loadKeys();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Renewing failed.', 'error');
		} finally {
			keyBusy = '';
		}
	}

	const date = (iso: string) => new Intl.DateTimeFormat('en-GB', { day: 'numeric', month: 'long', year: 'numeric' }).format(new Date(iso));

	// Recovery codes and the emergency account are the owner's.
	let isOwner = $state(false);
	onMount(() => {
		loadKeys();
		api.get<Me>('/me').then((m) => {
			isOwner = m.role === 'owner';
			if (!isOwner) return;
			api.get<{ enabled: boolean; password_set: boolean }>('/admin/emergency').then(
				(r) => (emergency = r),
				() => toast('The emergency account status could not be loaded.', 'error')
			);
		}, () => {});
	});

	async function putEmergency(body: { enabled: boolean; password?: string }) {
		emBusy = true;
		try {
			await api.send('PUT', '/admin/emergency', body);
			emergency = { enabled: body.enabled, password_set: emergency!.password_set || !!body.password };
			return true;
		} catch (e) {
			if (e instanceof ApiError && e.code === 'weak_password') pwError = e.message;
			else toast(e instanceof ApiError ? e.message : 'Saving failed.', 'error');
			return false;
		} finally {
			emBusy = false;
		}
	}

	async function toggleEmergency(e: Event & { currentTarget: HTMLInputElement }) {
		const on = e.currentTarget.checked;
		e.currentTarget.checked = emergency!.enabled; // follows the server, not the click
		if (on && !emergency!.password_set) return openPassword(true);
		if (await putEmergency({ enabled: on })) toast(on ? 'Emergency account on' : 'Emergency account off');
	}

	function openPassword(enable: boolean) {
		enableAfterPassword = enable;
		pw = '';
		pw2 = '';
		pwError = '';
		pwOpen = true;
	}

	async function savePassword(e: SubmitEvent) {
		e.preventDefault();
		if (pw.length < 16) return void (pwError = 'Use at least 16 characters.');
		if (pw !== pw2) return void (pwError = 'The passwords do not match.');
		pwError = '';
		const enabled = enableAfterPassword || emergency!.enabled;
		if (await putEmergency({ enabled, password: pw })) {
			pwOpen = false;
			toast(enableAfterPassword ? 'Emergency account on' : 'Emergency password changed');
		}
	}
</script>

<div class="pane-head"><h1 class="pane-title">Security</h1></div>
<p class="pane-intro">Every security measure in Keystone. Changes ask you to confirm and are logged under Activity.</p>

<SettingsEditor groups={securityGroups}>
	{#snippet after()}
		{#if isOwner}
		<section class="group" aria-labelledby="g-herstel">
			<h2 id="g-herstel">Recovery</h2>
			<div class="card">
				<div class="srow wrap-phone">
					<div class="text">
						<span class="label">Recovery codes</span>
						<p class="help">For when you lose your passkey. Each code works once.</p>
					</div>
					<div class="control">
						<button type="button" class="btn btn-secondary btn-sm" onclick={regenerate} disabled={regenBusy}>Make new codes</button>
					</div>
				</div>
				<div class="srow">
					<div class="text">
						<label class="label" for="em-switch">Emergency account</label>
						<p class="help" id="em-help">
							Sign in with a separate password when Active Directory cannot be reached. A passkey is still needed.
							{#if emergency?.enabled}<span class="chip warn">On</span>{/if}
						</p>
						{#if emergency?.password_set}
							<button type="button" class="link pw-link" onclick={() => openPassword(false)}>Change emergency password</button>
						{/if}
					</div>
					<div class="control">
						<input
							id="em-switch"
							class="switch"
							type="checkbox"
							role="switch"
							aria-describedby="em-help"
							checked={emergency?.enabled ?? false}
							disabled={!emergency || emBusy}
							onchange={toggleEmergency}
						/>
					</div>
				</div>
			</div>
		</section>

		{/if}

		<section class="group" aria-labelledby="g-sleutels">
			<h2 id="g-sleutels">Signing keys</h2>
			<div class="card">
				{#each keyList ?? [] as k (k.use)}
					<div class="srow wrap-phone">
						<div class="text">
							<span class="label">{KEY_INFO[k.use].label}</span>
							<p class="help">Sinds {date(k.created_at)} · <span class="mono">{k.id}</span></p>
						</div>
						<div class="control key-actions">
							<button type="button" class="btn btn-secondary btn-sm" onclick={() => download(`keystone-${k.use}.pem`, k.cert_pem)}>Certificate</button>
							<button type="button" class="btn btn-secondary btn-sm" disabled={keyBusy === k.use} onclick={() => rotate(k)}>Renew</button>
						</div>
					</div>
				{:else}
					<div class="loading-note" aria-label="Loading"><span class="spinner"></span></div>
				{/each}
			</div>
			<p class="foot">The vault key lives in the secrets volume and is not renewed here.</p>
		</section>
	{/snippet}
</SettingsEditor>

<Sheet bind:open={codesOpen} title="New recovery codes">
	{#if codes}
		<p class="sheet-msg">Keep them somewhere safe, such as your password manager. The old codes no longer work.</p>
		<ul class="codes">
			{#each codes as c}<li>{c}</li>{/each}
		</ul>
		<div class="sheet-actions">
			<button type="button" class="btn btn-secondary" onclick={() => download('keystone-recovery-codes.txt', codesFile(codes!, location.origin))}>
				Download
			</button>
			<button type="button" class="btn btn-primary" onclick={() => (codesOpen = false)}>I have saved them</button>
		</div>
	{/if}
</Sheet>

<Sheet bind:open={pwOpen} title={enableAfterPassword ? 'Turn on the emergency account' : 'Change emergency password'}>
	<form class="form" onsubmit={savePassword} novalidate>
		<p class="sheet-msg">Choose a long password you use nowhere else (at least 16 characters).</p>
		<label class="field">
			<span>Emergency password</span>
			<input class="input" type="password" autocomplete="new-password" bind:value={pw} />
		</label>
		<label class="field">
			<span>Repeat</span>
			<input class="input" type="password" autocomplete="new-password" bind:value={pw2} />
		</label>
		{#if pwError}<p class="error" role="alert">{pwError}</p>{/if}
		<div class="sheet-actions">
			<button type="button" class="btn btn-secondary" onclick={() => (pwOpen = false)}>Cancel</button>
			<button class="btn btn-primary" disabled={emBusy}>Save</button>
		</div>
	</form>
</Sheet>

<Confirm bind:this={confirmer} />

<style>
	.pw-link {
		margin: 4px 0 0 -6px;
	}
	.help .chip {
		margin-left: 4px;
	}
	.key-actions {
		display: flex;
		gap: 8px;
	}
</style>
