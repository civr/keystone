<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api';
	import { securityGroups } from '$lib/settingsMeta';
	import { codesFile, download } from '$lib/codes';
	import { toast } from '$lib/toast.svelte';
	import SettingsEditor from '$lib/components/settings/SettingsEditor.svelte';
	import Sheet from '$lib/components/Sheet.svelte';
	import Confirm from '$lib/components/settings/Confirm.svelte';

	let confirmer: Confirm;

	// --- Herstelcodes ---
	let codes = $state<string[] | null>(null);
	let codesOpen = $state(false);
	let regenBusy = $state(false);

	async function regenerate() {
		const ok = await confirmer.ask({
			title: 'Nieuwe herstelcodes maken?',
			message: 'Je krijgt 10 nieuwe codes. Alle oude herstelcodes werken daarna niet meer.',
			confirm: 'Nieuwe codes maken'
		});
		if (!ok) return;
		regenBusy = true;
		try {
			const res = await api.send<{ recovery_codes: string[] }>('POST', '/admin/recovery/regenerate');
			codes = res.recovery_codes;
			codesOpen = true;
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Nieuwe codes maken is niet gelukt.', 'error');
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
		saml: { label: 'SAML-ondertekening', help: 'Ondertekent SAML-antwoorden. Apps die de metadata niet zelf ophalen, moeten het nieuwe certificaat krijgen.' },
		oidc: { label: 'OIDC-tokens', help: 'Ondertekent ID- en access-tokens. De oude sleutel blijft nog 24 uur geldig.' },
		ldap: { label: 'LDAPS-certificaat', help: 'Het TLS-certificaat van keystone-ldap. Apps die het certificaat controleren, moeten het nieuwe krijgen.' }
	} as const;
	let keyList = $state<Key[] | null>(null);
	let keyBusy = $state('');

	async function loadKeys() {
		try {
			keyList = (await api.get<{ keys: Key[] }>('/admin/keys')).keys.filter((k) => k.active);
		} catch {
			toast('De sleutels konden niet worden geladen.', 'error');
		}
	}

	async function rotate(k: Key) {
		const ok = await confirmer.ask({
			title: `${KEY_INFO[k.use].label} vernieuwen?`,
			message: KEY_INFO[k.use].help,
			confirm: 'Vernieuwen'
		});
		if (!ok) return;
		keyBusy = k.use;
		try {
			await api.send('POST', `/admin/keys/${k.use}/rotate`);
			toast('Sleutel vernieuwd');
			await loadKeys();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Vernieuwen is niet gelukt.', 'error');
		} finally {
			keyBusy = '';
		}
	}

	const date = (iso: string) => new Intl.DateTimeFormat('nl-NL', { day: 'numeric', month: 'long', year: 'numeric' }).format(new Date(iso));

	onMount(() => {
		loadKeys();
		api.get<{ enabled: boolean; password_set: boolean }>('/admin/emergency').then(
			(r) => (emergency = r),
			() => toast('De status van het nood-account kon niet worden geladen.', 'error')
		);
	});

	async function putEmergency(body: { enabled: boolean; password?: string }) {
		emBusy = true;
		try {
			await api.send('PUT', '/admin/emergency', body);
			emergency = { enabled: body.enabled, password_set: emergency!.password_set || !!body.password };
			return true;
		} catch (e) {
			if (e instanceof ApiError && e.code === 'weak_password') pwError = e.message;
			else toast(e instanceof ApiError ? e.message : 'Opslaan is niet gelukt.', 'error');
			return false;
		} finally {
			emBusy = false;
		}
	}

	async function toggleEmergency(e: Event & { currentTarget: HTMLInputElement }) {
		const on = e.currentTarget.checked;
		e.currentTarget.checked = emergency!.enabled; // follows the server, not the click
		if (on && !emergency!.password_set) return openPassword(true);
		if (await putEmergency({ enabled: on })) toast(on ? 'Nood-account aan' : 'Nood-account uit');
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
		if (pw.length < 16) return void (pwError = 'Gebruik minstens 16 tekens.');
		if (pw !== pw2) return void (pwError = 'De wachtwoorden zijn niet gelijk.');
		pwError = '';
		const enabled = enableAfterPassword || emergency!.enabled;
		if (await putEmergency({ enabled, password: pw })) {
			pwOpen = false;
			toast(enableAfterPassword ? 'Nood-account aan' : 'Noodwachtwoord gewijzigd');
		}
	}
</script>

<div class="pane-head"><h1 class="pane-title">Beveiliging</h1></div>
<p class="pane-intro">
	Elke beveiligingsmaatregel van Keystone. Wijzigingen vragen om je passkey en komen in de activiteitenlog.
</p>

<SettingsEditor groups={securityGroups}>
	{#snippet after()}
		<section class="group" aria-labelledby="g-herstel">
			<h2 id="g-herstel">Herstel</h2>
			<div class="card">
				<div class="srow wrap-phone">
					<div class="text">
						<span class="label">Herstelcodes</span>
						<p class="help">Voor als je je passkey kwijt bent. Elke code werkt één keer.</p>
					</div>
					<div class="control">
						<button type="button" class="btn btn-secondary btn-sm" onclick={regenerate} disabled={regenBusy}>Nieuwe codes maken</button>
					</div>
				</div>
				<div class="srow">
					<div class="text">
						<label class="label" for="em-switch">Nood-account</label>
						<p class="help" id="em-help">
							Inloggen met een apart wachtwoord als AD niet bereikbaar is. Een passkey blijft nodig.
							{#if emergency?.enabled}<span class="chip warn">Aan</span>{/if}
						</p>
						{#if emergency?.password_set}
							<button type="button" class="link pw-link" onclick={() => openPassword(false)}>Noodwachtwoord wijzigen</button>
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

		<section class="group" aria-labelledby="g-sleutels">
			<h2 id="g-sleutels">Sleutels</h2>
			<div class="card">
				{#each keyList ?? [] as k (k.use)}
					<div class="srow wrap-phone">
						<div class="text">
							<span class="label">{KEY_INFO[k.use].label}</span>
							<p class="help">Sinds {date(k.created_at)} · <span class="mono">{k.id}</span></p>
						</div>
						<div class="control key-actions">
							<button type="button" class="btn btn-secondary btn-sm" onclick={() => download(`keystone-${k.use}.pem`, k.cert_pem)}>Certificaat</button>
							<button type="button" class="btn btn-secondary btn-sm" disabled={keyBusy === k.use} onclick={() => rotate(k)}>Vernieuw</button>
						</div>
					</div>
				{:else}
					<div class="loading-note" aria-label="Laden"><span class="spinner"></span></div>
				{/each}
			</div>
			<p class="foot">De kluissleutel staat in het secrets-volume en wordt niet via deze pagina vernieuwd.</p>
		</section>
	{/snippet}
</SettingsEditor>

<Sheet bind:open={codesOpen} title="Nieuwe herstelcodes">
	{#if codes}
		<p class="sheet-msg">Bewaar ze op een veilige plek, bijvoorbeeld in je wachtwoordmanager. De oude codes werken niet meer.</p>
		<ul class="codes">
			{#each codes as c}<li>{c}</li>{/each}
		</ul>
		<div class="sheet-actions">
			<button type="button" class="btn btn-secondary" onclick={() => download('keystone-herstelcodes.txt', codesFile(codes!, location.origin))}>
				Download
			</button>
			<button type="button" class="btn btn-primary" onclick={() => (codesOpen = false)}>Ik heb ze bewaard</button>
		</div>
	{/if}
</Sheet>

<Sheet bind:open={pwOpen} title={enableAfterPassword ? 'Nood-account aanzetten' : 'Noodwachtwoord wijzigen'}>
	<form class="form" onsubmit={savePassword} novalidate>
		<p class="sheet-msg">Kies een lang wachtwoord dat je nergens anders gebruikt (minstens 16 tekens).</p>
		<label class="field">
			<span>Noodwachtwoord</span>
			<input class="input" type="password" autocomplete="new-password" bind:value={pw} />
		</label>
		<label class="field">
			<span>Herhaal</span>
			<input class="input" type="password" autocomplete="new-password" bind:value={pw2} />
		</label>
		{#if pwError}<p class="error" role="alert">{pwError}</p>{/if}
		<div class="sheet-actions">
			<button type="button" class="btn btn-secondary" onclick={() => (pwOpen = false)}>Annuleren</button>
			<button class="btn btn-primary" disabled={emBusy}>Opslaan</button>
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
