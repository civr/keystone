<script lang="ts">
	import '$lib/theme.css';
	import Backdrop from '$lib/components/Backdrop.svelte';
	import Toast from '$lib/components/Toast.svelte';

	let { children } = $props();
</script>

<Backdrop />
{@render children()}
<Toast />
