/**
 * A queue that runs tasks one at a time in call order. A task's onError runs
 * only when no later task was queued by the time it failed: the later task
 * carries newer state and supersedes it (e.g. a newer app order).
 */
export function latestQueue() {
	let chain: Promise<void> = Promise.resolve();
	let seq = 0;
	return (task: () => Promise<unknown>, onError: (e: unknown) => void): Promise<void> => {
		const mine = ++seq;
		chain = chain.then(() =>
			task().then(
				() => undefined,
				(e: unknown) => {
					if (mine === seq) onError(e);
				}
			)
		);
		return chain;
	};
}
