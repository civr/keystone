<script lang="ts">
	import { shown } from '$lib/credentials.svelte';
	import { copyText } from '$lib/copy';
	import { toast } from '$lib/toast.svelte';
	import '$lib/settings.css';
	import Sheet from './Sheet.svelte';

	async function copy(text: string, what: string, clearAfter?: number) {
		try {
			await copyText(text);
			if (clearAfter) {
				setTimeout(() => copyText('').catch(() => {}), clearAfter * 1000);
				toast(`${what} copied; cleared in ${clearAfter} s`);
			} else toast(`${what} copied`);
		} catch {
			toast(`${what} could not be copied. Select it and copy it yourself.`, 'error');
		}
	}

	function open(href: string) {
		const w = window.open(href, '_blank');
		if (w) w.opener = null;
	}
</script>

<Sheet bind:open={shown.open} title={shown.item?.title ?? ''}>
	{#if shown.item}
		{@const item = shown.item}
		<div class="card creds">
			{#if item.username}
				<div class="srow">
					<div class="text">
						<span class="help">Username</span>
						<span class="label value">{item.username}</span>
					</div>
					<button type="button" class="btn btn-secondary btn-sm" onclick={() => copy(item.username, 'Username')}>Copy</button>
				</div>
			{/if}
			<div class="srow">
				<div class="text">
					<span class="help">Password</span>
					<span class="label value" aria-label="Hidden">••••••••••••</span>
				</div>
				<button type="button" class="btn btn-secondary btn-sm" onclick={() => copy(item.password, 'Password', item.clearAfter)}>Copy</button>
			</div>
		</div>
		{#if item.href}
			{@const href = item.href}
			<div class="sheet-actions">
				<button type="button" class="btn btn-primary" onclick={() => open(href)}>Open</button>
			</div>
		{/if}
	{/if}
</Sheet>

<style>
	.creds {
		margin: 0;
	}
	.value {
		overflow-wrap: anywhere;
		user-select: all;
	}
</style>
