<script lang="ts">
	import { securityGroups } from '$lib/settingsMeta';
	import SettingsEditor from '$lib/components/settings/SettingsEditor.svelte';

	const tabs = [
		{ id: 'network', label: 'Access', groups: ['network'] },
		{ id: 'headers', label: 'Headers', groups: ['headers'] }
	];
	let tab = $state(tabs[0].id);
	const shown = $derived(tabs.find((t) => t.id === tab)!.groups);
</script>

<div class="pane-head"><h1 class="pane-title">Network</h1></div>
<p class="pane-intro">Where sign-in and the admin portal may be used from, which proxies Keystone trusts, and the HTTP security headers.</p>

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
