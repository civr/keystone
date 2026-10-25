import { describe, expect, it } from 'vitest';
import { askPassword, prompt, settlePrompt } from './passwordPrompt.svelte';

describe('password prompt', () => {
	it('closes and resolves on success', async () => {
		const p = askPassword();
		expect(prompt.open).toBe(true);
		settlePrompt(true);
		expect(prompt.open).toBe(false);
		await expect(p).resolves.toBeUndefined();
	});
	it('closes and rejects on cancel, and a later close is harmless', async () => {
		const p = askPassword();
		settlePrompt(false);
		await expect(p).rejects.toThrow('cancelled');
		settlePrompt(false); // the sheet's own close after a cancel
		expect(prompt.open).toBe(false);
	});
	it('closing after a success does not cancel it', async () => {
		const p = askPassword();
		settlePrompt(true);
		settlePrompt(false); // Sheet onclose fires after open = false
		await expect(p).resolves.toBeUndefined();
	});
	it('a new request replaces an open one', async () => {
		const first = askPassword();
		const second = askPassword();
		await expect(first).rejects.toThrow('replaced');
		expect(prompt.open).toBe(true);
		settlePrompt(true);
		await expect(second).resolves.toBeUndefined();
	});
});
