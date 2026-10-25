/**
 * Step-up without a passkey: a sheet asks for the password (StepUpPassword)
 * and api.ts retries the request once it is confirmed. prompt.open is the
 * only open/closed state; settling is idempotent, so closing the sheet after
 * a success (or twice) cannot cancel or reopen anything.
 */
export const prompt = $state<{ open: boolean; generation: number }>({ open: false, generation: 0 });

let pending: { resolve: () => void; reject: (e: Error) => void } | null = null;

/** Opens the sheet; resolves once the password is confirmed, rejects on cancel. */
export function askPassword(): Promise<void> {
	pending?.reject(new Error('replaced'));
	prompt.generation++;
	prompt.open = true;
	return new Promise((resolve, reject) => (pending = { resolve, reject }));
}

/** Settles the open request (if any) and closes the sheet. */
export function settlePrompt(ok: boolean): void {
	const p = pending;
	pending = null;
	prompt.open = false;
	if (ok) p?.resolve();
	else p?.reject(new Error('cancelled'));
}
