import { describe, expect, it, vi } from 'vitest';
import { latestQueue } from './serial';

const deferred = () => {
	let resolve!: () => void, reject!: (e: unknown) => void;
	const promise = new Promise<void>((res, rej) => ((resolve = res), (reject = rej)));
	return { promise, resolve, reject };
};

describe('latestQueue', () => {
	it('runs tasks one at a time in call order', async () => {
		const run = latestQueue();
		const log: string[] = [];
		const d1 = deferred();
		const p1 = run(async () => {
			log.push('1 start');
			await d1.promise;
			log.push('1 end');
		}, vi.fn());
		const p2 = run(async () => {
			log.push('2 start');
		}, vi.fn());
		await new Promise((r) => setTimeout(r, 0));
		expect(log).toEqual(['1 start']);
		d1.resolve();
		await Promise.all([p1, p2]);
		expect(log).toEqual(['1 start', '1 end', '2 start']);
	});

	it('reports a failure only when no later task was queued', async () => {
		const run = latestQueue();
		const onErr1 = vi.fn(), onErr2 = vi.fn();
		const d1 = deferred();
		const p1 = run(() => d1.promise, onErr1);
		const p2 = run(() => Promise.resolve(), onErr2);
		d1.reject(new Error('first'));
		await Promise.all([p1, p2]);
		expect(onErr1).not.toHaveBeenCalled();
		expect(onErr2).not.toHaveBeenCalled();

		const onErr3 = vi.fn();
		await run(() => Promise.reject(new Error('own')), onErr3);
		expect(onErr3).toHaveBeenCalledTimes(1);
		expect(onErr3.mock.calls[0][0]).toEqual(new Error('own'));
	});

	it('keeps running after a failure', async () => {
		const run = latestQueue();
		await run(() => Promise.reject(new Error('x')), vi.fn());
		const task = vi.fn().mockResolvedValue(undefined);
		await run(task, vi.fn());
		expect(task).toHaveBeenCalledTimes(1);
	});
});
