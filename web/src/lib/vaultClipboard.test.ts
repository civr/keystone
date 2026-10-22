import { describe, expect, it, vi } from 'vitest';
import { copyThenClear } from './vaultClipboard';

function fakeClipboard() {
	let text = '';
	return {
		writeText: vi.fn(async (t: string) => void (text = t)),
		readText: vi.fn(async () => text),
		get text() {
			return text;
		}
	};
}

describe('copyThenClear', () => {
	it('copies and clears after the delay when unchanged', async () => {
		vi.useFakeTimers();
		const cb = fakeClipboard();
		await copyThenClear(Promise.resolve('s3cret'), 30, cb);
		expect(cb.text).toBe('s3cret');
		await vi.advanceTimersByTimeAsync(29_000);
		expect(cb.text).toBe('s3cret');
		await vi.advanceTimersByTimeAsync(1_000);
		expect(cb.text).toBe('');
		vi.useRealTimers();
	});

	it('leaves the clipboard alone when the user copied something else', async () => {
		vi.useFakeTimers();
		const cb = fakeClipboard();
		await copyThenClear(Promise.resolve('s3cret'), 5, cb);
		await cb.writeText('other');
		await vi.advanceTimersByTimeAsync(5_000);
		expect(cb.text).toBe('other');
		vi.useRealTimers();
	});
});
