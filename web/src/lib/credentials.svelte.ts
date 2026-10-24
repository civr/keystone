/** The sheet that shows a username and password to copy (CredentialSheet). */
export interface Shown {
	title: string;
	username: string;
	password: string;
	/** Where "Open" goes; null for none. */
	href: string | null;
}

export const shown = $state<{ open: boolean; item: Shown | null }>({ open: false, item: null });

export function showCredentials(item: Shown): void {
	shown.item = item;
	shown.open = true;
}
