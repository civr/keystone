<script lang="ts">
	import { securityGroups } from '$lib/settingsMeta';
	import SettingsEditor from '$lib/components/settings/SettingsEditor.svelte';

	const tabs = [
		{ id: 'tokens', label: 'OIDC', groups: ['tokens'] },
		{ id: 'ldap', label: 'LDAP', groups: ['ldap'] },
		{ id: 'gate', label: 'Gate', groups: ['gate'] }
	];
	let tab = $state(tabs[0].id);
	const shown = $derived(tabs.find((t) => t.id === tab)!.groups);
</script>

<div class="pane-head"><h1 class="pane-title">Protocols</h1></div>
<p class="pane-intro">Settings that apply to every app of a kind: OIDC token lifetimes, keystone-ldap and the gate.</p>

<div class="segmented tabs" role="tablist" aria-label="Sections">
	{#each tabs as t (t.id)}
		<label><input type="radio" name="tab" value={t.id} bind:group={tab} /><span>{t.label}</span></label>
	{/each}
</div>

<SettingsEditor groups={securityGroups} visible={shown} />

<style>
	.tabs {
		margin: 0 0 20px;
	}
</style>
