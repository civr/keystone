<!-- Fixed, static gradient wash behind everything (no looping motion). -->
<div class="backdrop" aria-hidden="true"></div>

<style>
	.backdrop {
		position: fixed;
		inset: 0;
		z-index: -1;
		background:
			radial-gradient(60% 50% at 100% 100%, color-mix(in srgb, var(--bg-3) 70%, transparent) 0%, transparent 70%),
			radial-gradient(120% 80% at 20% 0%, var(--bg-1) 0%, var(--bg-2) 45%, var(--bg-3) 100%);
		background-color: var(--bg-base);
	}
</style>
