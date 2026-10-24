/** Greeting for the local hour: 00–06 night, 06–12 morning, 12–18 afternoon, 18–24 evening. */
export function greeting(date: Date): string {
	const h = date.getHours();
	if (h < 6) return 'Good night';
	if (h < 12) return 'Good morning';
	if (h < 18) return 'Good afternoon';
	return 'Good evening';
}

export interface NameParts {
	username: string;
	display_name?: string | null;
	given_name?: string | null;
}

/** AD givenName, falling back to displayName, then the username. */
export function firstName(me: NameParts): string {
	return me.given_name?.trim() || me.display_name?.trim() || me.username;
}
